package authz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	authzed "github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Read and lookup bounds.
const (
	// maxRelationshipReads is the page size of one ReadRelationships call, well under SpiceDB's --max-read-relationships-limit.
	maxRelationshipReads = 250
	// lookupPageSize is the page size of one LookupResources call.
	lookupPageSize = 250
	// maxLookupResults fails a lookup closed rather than returning a partial or unbounded authorization result.
	maxLookupResults = 20_000
)

// ObjectRef names one SpiceDB object.
type ObjectRef struct {
	Type string
	ID   string
}

// SubjectRef is a relationship's subject; Relation is set for subject sets such as team#member.
type SubjectRef struct {
	Type     string
	ID       string
	Relation string
}

// Relationship is one tuple resource#relation@subject.
type Relationship struct {
	Resource ObjectRef
	Relation string
	Subject  SubjectRef
}

// UpdateOp is how a relationship update applies: both are idempotent, so a write can be retried.
type UpdateOp int

const (
	OpTouch UpdateOp = iota
	OpDelete
)

type RelationshipUpdate struct {
	Op           UpdateOp
	Relationship Relationship
}

func touch(r Relationship) RelationshipUpdate {
	return RelationshipUpdate{Op: OpTouch, Relationship: r}
}
func remove(r Relationship) RelationshipUpdate {
	return RelationshipUpdate{Op: OpDelete, Relationship: r}
}

// SubjectFilter narrows a filter to one subject; Relation is optional.
type SubjectFilter struct {
	Type     string
	ID       string
	Relation string
}

// RelationshipFilter selects relationships of ResourceType, narrowed by any of the optional fields. A delete needs ResourceID or Subject.ID; a read may sweep a whole type.
type RelationshipFilter struct {
	ResourceType string
	ResourceID   string
	Relation     string
	Subject      *SubjectFilter
}

// RelationshipPage is one page of a read. Cursor is empty when the read is exhausted; Revision is the snapshot the page was read at, constant across the pages of one cursored read.
type RelationshipPage struct {
	Relationships []Relationship
	Cursor        string
	Revision      string
}

// SchemaDiff counts the differences between a schema and the one SpiceDB holds, by stable kind.
type SchemaDiff struct {
	Count int            `json:"differenceCount"`
	Kinds map[string]int `json:"differenceKinds"`
}

// Client is the facade over SpiceDB's gRPC API. Every call validates its input first (a malformed request fails with authzed_invalid_request and no call), runs under the process profile's deadline, retries and circuit breaker, and answers strictly: anything SpiceDB returns that Forma never writes (caveats, expirations, conditional permissions) is refused rather than guessed at.
type Client struct {
	api         *authzed.Client
	run         *runner
	consistency *v1.Consistency
}

func fullyConsistent() *v1.Consistency {
	return &v1.Consistency{Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true}}
}

// NewClient connects lazily: gRPC dials on the first call, so a SpiceDB that is down at startup fails checks rather than the start.
func NewClient(cfg config.AuthzConfig, profile Profile, log *slog.Logger) (*Client, error) {
	if !cfg.Enabled {
		return nil, newError(CodeDisabled, "client_initialization", false, 0)
	}
	var opts []grpc.DialOption
	if cfg.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()), grpcutil.WithInsecureBearerToken(cfg.Token))
	} else {
		certs, err := grpcutil.WithSystemCerts(grpcutil.VerifyCA)
		if err != nil {
			return nil, fmt.Errorf("load system certificates for SpiceDB: %w", err)
		}
		opts = append(opts, certs, grpcutil.WithBearerToken(cfg.Token))
	}
	api, err := authzed.NewClient(cfg.Endpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("create SpiceDB client: %w", err)
	}
	consistency := fullyConsistent()
	if cfg.Consistency == "minimize_latency" {
		consistency = &v1.Consistency{Requirement: &v1.Consistency_MinimizeLatency{MinimizeLatency: true}}
	}
	return &Client{api: api, run: newRunner(profile, log), consistency: consistency}, nil
}

// OpenSpiceDB builds the process's client from configuration, or nil when AuthZed is disabled. The returned cleanup closes the connection.
func OpenSpiceDB(cfg config.Config, log *slog.Logger) (*Client, func(), error) {
	if !cfg.Authz.Enabled {
		return nil, func() {}, nil
	}
	c, err := NewClient(cfg.Authz, RequestProfile, log)
	if err != nil {
		return nil, nil, err
	}
	return c, func() { _ = c.Close() }, nil
}

func (c *Client) Close() error { return c.api.Close() }

func invalid(operation string) *Error { return newError(CodeInvalidRequest, operation, false, 0) }

// CheckPermission asks whether subject holds permission on resource.
func (c *Client) CheckPermission(ctx context.Context, resource ObjectRef, permission string, subject ObjectRef) (bool, error) {
	const op = "check_permission"
	if permission == "" || resource.ID == "" || resource.Type == "" || subject.ID == "" || subject.Type == "" {
		return false, invalid(op)
	}
	var allowed bool
	err := c.run.do(ctx, op, func(ctx context.Context) error {
		resp, err := c.api.CheckPermission(ctx, &v1.CheckPermissionRequest{
			Consistency: c.consistency,
			Resource:    &v1.ObjectReference{ObjectType: resource.Type, ObjectId: resource.ID},
			Permission:  permission,
			Subject:     &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: subject.Type, ObjectId: subject.ID}},
		})
		if err != nil {
			return err
		}
		switch resp.GetPermissionship() {
		case v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION:
			allowed = true
		case v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION:
			allowed = false
		default:
			// Forma never writes caveats, so a conditional answer means the store holds something this facade cannot represent.
			return newError(CodeUnsupported, op, false, 1)
		}
		return nil
	})
	return allowed, err
}

// LookupResources returns every resourceType id subject holds permission on, de-duplicated and sorted. It reads the complete set or fails: past maxLookupResults it fails with authzed_limit_exceeded rather than returning a partial authorization result.
func (c *Client) LookupResources(ctx context.Context, resourceType, permission string, subject ObjectRef) ([]string, error) {
	const op = "lookup_resources"
	if permission == "" || resourceType == "" || subject.ID == "" || subject.Type == "" {
		return nil, invalid(op)
	}
	seen := map[string]struct{}{}
	total := 0
	cursor := ""
	for {
		var page []*v1.LookupResourcesResponse
		err := c.run.do(ctx, op, func(ctx context.Context) error {
			req := &v1.LookupResourcesRequest{
				Consistency:        c.consistency,
				ResourceObjectType: resourceType,
				Permission:         permission,
				Subject:            &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: subject.Type, ObjectId: subject.ID}},
				OptionalLimit:      lookupPageSize,
			}
			if cursor != "" {
				req.OptionalCursor = &v1.Cursor{Token: cursor}
			}
			stream, err := c.api.LookupResources(ctx, req)
			if err != nil {
				return err
			}
			page, err = drain(stream)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			if r.GetPermissionship() != v1.LookupPermissionship_LOOKUP_PERMISSIONSHIP_HAS_PERMISSION || r.GetResourceObjectId() == "" {
				return nil, newError(CodeUnsupported, op, false, 1)
			}
		}
		total += len(page)
		if total > maxLookupResults {
			return nil, newError(CodeLimitExceeded, op, false, 0)
		}
		for _, r := range page {
			seen[r.GetResourceObjectId()] = struct{}{}
		}
		if len(page) < lookupPageSize {
			break
		}
		next := page[len(page)-1].GetAfterResultCursor().GetToken()
		if next == "" || next == cursor {
			return nil, newError(CodeInternal, op, false, 0)
		}
		cursor = next
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

func drain[T any](stream grpc.ServerStreamingClient[T]) ([]*T, error) {
	var out []*T
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
}

func validRelationship(r Relationship) bool {
	return r.Resource.Type != "" && r.Resource.ID != "" && r.Relation != "" && r.Subject.Type != "" && r.Subject.ID != ""
}

// WriteRelationships applies 1 to 1,000 TOUCH or DELETE updates in one SpiceDB transaction.
func (c *Client) WriteRelationships(ctx context.Context, updates []RelationshipUpdate) error {
	const op = "write_relationships"
	if len(updates) == 0 || len(updates) > maxUpdatesPerCall {
		return invalid(op)
	}
	req := &v1.WriteRelationshipsRequest{Updates: make([]*v1.RelationshipUpdate, 0, len(updates))}
	for _, u := range updates {
		if !validRelationship(u.Relationship) {
			return invalid(op)
		}
		operation := v1.RelationshipUpdate_OPERATION_TOUCH
		if u.Op == OpDelete {
			operation = v1.RelationshipUpdate_OPERATION_DELETE
		}
		r := u.Relationship
		req.Updates = append(req.Updates, &v1.RelationshipUpdate{
			Operation: operation,
			Relationship: &v1.Relationship{
				Resource: &v1.ObjectReference{ObjectType: r.Resource.Type, ObjectId: r.Resource.ID},
				Relation: r.Relation,
				Subject:  &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: r.Subject.Type, ObjectId: r.Subject.ID}, OptionalRelation: r.Subject.Relation},
			},
		})
	}
	return c.run.do(ctx, op, func(ctx context.Context) error {
		_, err := c.api.WriteRelationships(ctx, req)
		return err
	})
}

func toProtoFilter(f RelationshipFilter) *v1.RelationshipFilter {
	pf := &v1.RelationshipFilter{ResourceType: f.ResourceType, OptionalResourceId: f.ResourceID, OptionalRelation: f.Relation}
	if f.Subject != nil {
		pf.OptionalSubjectFilter = &v1.SubjectFilter{SubjectType: f.Subject.Type, OptionalSubjectId: f.Subject.ID}
		if f.Subject.Relation != "" {
			pf.OptionalSubjectFilter.OptionalRelation = &v1.SubjectFilter_RelationFilter{Relation: f.Subject.Relation}
		}
	}
	return pf
}

// DeleteRelationships removes every relationship matching filter, which must name a resource id or a subject id. The delete is unlimited and all-or-nothing; anything short of SpiceDB reporting it complete is an error, because a half-revoked graph reported as done is the failure this call must never produce.
func (c *Client) DeleteRelationships(ctx context.Context, filter RelationshipFilter) error {
	const op = "delete_relationships"
	hasSubject := filter.Subject != nil && filter.Subject.ID != ""
	if filter.ResourceType == "" || (filter.ResourceID == "" && !hasSubject) || (filter.Subject != nil && filter.Subject.Type == "") {
		return invalid(op)
	}
	req := &v1.DeleteRelationshipsRequest{RelationshipFilter: toProtoFilter(filter)}
	return c.run.do(ctx, op, func(ctx context.Context) error {
		resp, err := c.api.DeleteRelationships(ctx, req)
		if err != nil {
			return err
		}
		if resp.GetDeletionProgress() != v1.DeleteRelationshipsResponse_DELETION_PROGRESS_COMPLETE {
			return newError(CodeInternal, op, true, 1)
		}
		return nil
	})
}

// ReadRelationships reads one page of raw relationships, fully consistent. It is for operational tooling that reconciles SpiceDB against PostgreSQL, never for a permission decision: deciding access from raw tuples would reimplement the schema in application code.
func (c *Client) ReadRelationships(ctx context.Context, filter RelationshipFilter, limit int, cursor string) (RelationshipPage, error) {
	const op = "read_relationships"
	subjectValid := filter.Subject == nil || (filter.Subject.Type != "" && filter.Subject.ID != "")
	// A limit is mandatory: SpiceDB reads 0 as unlimited, an unbounded allocation that cannot finish within the deadline.
	if filter.ResourceType == "" || !subjectValid || limit < 1 || limit > maxRelationshipReads {
		return RelationshipPage{}, invalid(op)
	}
	req := &v1.ReadRelationshipsRequest{
		// The same requirement on every page: SpiceDB hashes it into the cursor, and the cursor's own revision keeps later pages on the first page's snapshot.
		Consistency:        fullyConsistent(),
		RelationshipFilter: toProtoFilter(filter),
		OptionalLimit:      uint32(limit), //nolint:gosec // bounded to 1..250 above
	}
	if cursor != "" {
		req.OptionalCursor = &v1.Cursor{Token: cursor}
	}
	var page RelationshipPage
	err := c.run.do(ctx, op, func(ctx context.Context) error {
		stream, err := c.api.ReadRelationships(ctx, req)
		if err != nil {
			return err
		}
		responses, err := drain(stream)
		if err != nil {
			return err
		}
		page = RelationshipPage{Relationships: make([]Relationship, 0, len(responses))}
		if len(responses) == 0 {
			return nil
		}
		last := responses[len(responses)-1]
		page.Revision = last.GetReadAt().GetToken()
		if page.Revision == "" {
			return newError(CodeInternal, op, false, 0)
		}
		if len(responses) == limit {
			page.Cursor = last.GetAfterResultCursor().GetToken()
		}
		for _, r := range responses {
			rel, err := fromProtoRelationship(r.GetRelationship())
			if err != nil {
				return err
			}
			page.Relationships = append(page.Relationships, rel)
		}
		return nil
	})
	return page, err
}

// fromProtoRelationship is strict: a tuple with a missing field, or with a caveat or expiry Forma never writes, would compare unequal to anything Forma wrote and be classified as orphaned, which under pruning means deleted.
func fromProtoRelationship(r *v1.Relationship) (Relationship, error) {
	const op = "read_relationships"
	resource := r.GetResource()
	subject := r.GetSubject().GetObject()
	rel := Relationship{
		Resource: ObjectRef{Type: resource.GetObjectType(), ID: resource.GetObjectId()},
		Relation: r.GetRelation(),
		Subject:  SubjectRef{Type: subject.GetObjectType(), ID: subject.GetObjectId(), Relation: r.GetSubject().GetOptionalRelation()},
	}
	if !validRelationship(rel) {
		return Relationship{}, newError(CodeInternal, op, false, 0)
	}
	if r.GetOptionalCaveat() != nil || r.GetOptionalExpiresAt() != nil {
		return Relationship{}, newError(CodeUnsupported, op, false, 0)
	}
	return rel, nil
}

// ReadSchema returns the schema SpiceDB holds, or "" when none has been written yet.
func (c *Client) ReadSchema(ctx context.Context) (string, error) {
	var text string
	err := c.run.do(ctx, "read_schema", func(ctx context.Context) error {
		resp, err := c.api.ReadSchema(ctx, &v1.ReadSchemaRequest{})
		if status.Code(err) == codes.NotFound {
			text = ""
			return nil
		}
		if err != nil {
			return err
		}
		text = resp.GetSchemaText()
		return nil
	})
	return text, err
}

// WriteSchema replaces SpiceDB's schema; writing the same schema again is a no-op.
func (c *Client) WriteSchema(ctx context.Context, schema string) error {
	return c.run.do(ctx, "write_schema", func(ctx context.Context) error {
		_, err := c.api.WriteSchema(ctx, &v1.WriteSchemaRequest{Schema: schema})
		return err
	})
}

// DiffSchema compares schema with the one SpiceDB holds, semantically rather than as text.
func (c *Client) DiffSchema(ctx context.Context, schema string) (SchemaDiff, error) {
	var diff SchemaDiff
	err := c.run.do(ctx, "diff_schema", func(ctx context.Context) error {
		resp, err := c.api.DiffSchema(ctx, &v1.DiffSchemaRequest{Consistency: fullyConsistent(), ComparisonSchema: schema})
		if err != nil {
			return err
		}
		diff = SchemaDiff{Count: len(resp.GetDiffs()), Kinds: map[string]int{}}
		for _, d := range resp.GetDiffs() {
			diff.Kinds[diffKind(d)]++
		}
		return nil
	})
	return diff, err
}

// diffKind names a schema difference by a stable kind, never by the definitions it touches.
func diffKind(d *v1.ReflectionSchemaDiff) string {
	switch d.GetDiff().(type) {
	case *v1.ReflectionSchemaDiff_CaveatAdded:
		return "caveat_added"
	case *v1.ReflectionSchemaDiff_CaveatDocCommentChanged:
		return "caveat_doc_comment_changed"
	case *v1.ReflectionSchemaDiff_CaveatExprChanged:
		return "caveat_expr_changed"
	case *v1.ReflectionSchemaDiff_CaveatParameterAdded:
		return "caveat_parameter_added"
	case *v1.ReflectionSchemaDiff_CaveatParameterRemoved:
		return "caveat_parameter_removed"
	case *v1.ReflectionSchemaDiff_CaveatParameterTypeChanged:
		return "caveat_parameter_type_changed"
	case *v1.ReflectionSchemaDiff_CaveatRemoved:
		return "caveat_removed"
	case *v1.ReflectionSchemaDiff_DefinitionAdded:
		return "definition_added"
	case *v1.ReflectionSchemaDiff_DefinitionDocCommentChanged:
		return "definition_doc_comment_changed"
	case *v1.ReflectionSchemaDiff_DefinitionRemoved:
		return "definition_removed"
	case *v1.ReflectionSchemaDiff_PermissionAdded:
		return "permission_added"
	case *v1.ReflectionSchemaDiff_PermissionDocCommentChanged:
		return "permission_doc_comment_changed"
	case *v1.ReflectionSchemaDiff_PermissionExprChanged:
		return "permission_expr_changed"
	case *v1.ReflectionSchemaDiff_PermissionRemoved:
		return "permission_removed"
	case *v1.ReflectionSchemaDiff_RelationAdded:
		return "relation_added"
	case *v1.ReflectionSchemaDiff_RelationDocCommentChanged:
		return "relation_doc_comment_changed"
	case *v1.ReflectionSchemaDiff_RelationRemoved:
		return "relation_removed"
	case *v1.ReflectionSchemaDiff_RelationSubjectTypeAdded:
		return "relation_subject_type_added"
	case *v1.ReflectionSchemaDiff_RelationSubjectTypeRemoved:
		return "relation_subject_type_removed"
	default:
		return "unknown"
	}
}
