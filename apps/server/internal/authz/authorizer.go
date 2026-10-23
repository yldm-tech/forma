package authz

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Authorizer answers authorization questions. PostgreSQL is consulted first, as the tenant boundary: the resource must exist (a survey or response is resolved to its workspace), a user must be active, and an API key must belong to the resource's organization. Only then does SpiceDB decide, behind the projection freshness guard. Failures are returned as *Error and never turned into a denial.
type Authorizer struct {
	db                        *gorm.DB
	client                    *Client
	fresh                     *freshnessGuard
	userManagementMinimumRole string
}

// NewAuthorizer builds the authorizer; client is nil when AuthZed is disabled, and every decision then fails with authzed_disabled.
func NewAuthorizer(cfg config.Config, db *gorm.DB, client *Client) *Authorizer {
	return &Authorizer{db: db, client: client, fresh: newFreshnessGuard(db), userManagementMinimumRole: cfg.UserManagementMinimumRole}
}

// Can reports whether actor may perform action on resource. Within a request carrying a decision memo (WithDecisionMemo), the same question is evaluated once; denials are memoized like grants, failures never.
func (a *Authorizer) Can(ctx context.Context, actor Actor, action Action, resource Resource) (bool, error) {
	checkIssued(ctx)
	if memo := memoFrom(ctx); memo != nil {
		key := fmt.Sprintf("%s:%s|%s|%s:%s", actor.Type, actor.ID, action, resource.Type, resource.ID)
		return memo.do(key, func() (bool, error) { return a.evaluate(ctx, actor, action, resource) })
	}
	return a.evaluate(ctx, actor, action, resource)
}

// Require is Can for service methods: a denial is forma2's AuthorizationError, "Not authorized" (403).
func (a *Authorizer) Require(ctx context.Context, actor Actor, action Action, resource Resource) error {
	allowed, err := a.Can(ctx, actor, action, resource)
	if err != nil {
		return err
	}
	if !allowed {
		return apperr.Forbidden("Not authorized")
	}
	return nil
}

func (a *Authorizer) evaluate(ctx context.Context, actor Actor, action Action, resource Resource) (bool, error) {
	started := time.Now()
	allowed, err := a.check(ctx, actor, action, resource)
	if err != nil {
		err = normalize(err, "authorization")
	}
	recordDecision(ctx, string(action), actor.Type, string(resource.Type), started, allowed, err)
	return allowed && err == nil, err
}

func (a *Authorizer) check(ctx context.Context, actor Actor, action Action, resource Resource) (bool, error) {
	scope, err := resolveScope(ctx, a.db, actor, resource)
	if err != nil || scope == nil {
		return false, err
	}
	if !scope.actorValid {
		return false, nil
	}
	if err := a.fresh.assert(ctx); err != nil {
		return false, err
	}
	permission, err := a.permission(actor, action, resource.Type)
	if err != nil || permission == "" {
		return false, err
	}
	if a.client == nil {
		return false, newError(CodeDisabled, "client_initialization", false, 0)
	}
	return a.client.CheckPermission(ctx,
		ObjectRef{Type: spicedbType(scope.resourceType), ID: scope.resourceID.String()},
		permission,
		ObjectRef{Type: spicedbType(actor.Type), ID: actor.ID.String()})
}

// permission is the SpiceDB permission an action is checked as. "" means the decision is a denial without asking SpiceDB.
func (a *Authorizer) permission(actor Actor, action Action, resourceType ResourceType) (string, error) {
	permission, err := action.parse(resourceType)
	if err != nil {
		return "", err
	}
	// USER_MANAGEMENT_MINIMUM_ROLE governs a user's organization.manage_access; API keys are exempt.
	if actor.Type == ActorUser && action == OrganizationManageAccess {
		switch a.userManagementMinimumRole {
		case "disabled":
			return "", nil
		case "owner":
			return "write", nil
		default:
			return "manage_access", nil
		}
	}
	if p, ok := workspacePermissionForDerivedAction[action]; ok {
		return p, nil
	}
	return permission, nil
}

// LookupOrganizationIDs lists the organizations actor may read.
func (a *Authorizer) LookupOrganizationIDs(ctx context.Context, actor Actor) ([]uuid.UUID, error) {
	return a.observedLookup(ctx, actor, typeOrganization, "read")
}

// LookupWorkspaceIDs lists the workspaces actor may read, or write when write is set.
func (a *Authorizer) LookupWorkspaceIDs(ctx context.Context, actor Actor, write bool) ([]uuid.UUID, error) {
	permission := "read"
	if write {
		permission = "write"
	}
	return a.observedLookup(ctx, actor, typeWorkspace, permission)
}

// observedLookup is a list operation counted and recorded as one decision, forma2's lookupAuthorizationResourceIds: an empty authorized set is the aggregate equivalent of a denial.
func (a *Authorizer) observedLookup(ctx context.Context, actor Actor, resourceType, permission string) ([]uuid.UUID, error) {
	checkIssued(ctx)
	started := time.Now()
	ids, err := a.lookup(ctx, actor, resourceType, permission)
	if err != nil {
		err = normalize(err, "authorization_list")
	}
	recordDecision(ctx, resourceType+"."+permission, actor.Type, resourceType, started, len(ids) > 0, err)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// lookup applies the same actor rules as a single check, which forma2's lists skipped: an inactive or missing user, or a missing API key, gets nothing, and an API key only ever sees resources of its own organization.
func (a *Authorizer) lookup(ctx context.Context, actor Actor, resourceType, permission string) ([]uuid.UUID, error) {
	var keyOrganization uuid.UUID
	switch actor.Type {
	case ActorUser:
		active, err := userActive(ctx, a.db, actor.ID)
		if err != nil || !active {
			return nil, err
		}
	case ActorAPIKey:
		org, err := organizationOf(ctx, a.db, "api_keys", actor.ID)
		if err != nil || org == nil {
			return nil, err
		}
		keyOrganization = *org
	}
	if err := a.fresh.assert(ctx); err != nil {
		return nil, err
	}
	if a.client == nil {
		return nil, newError(CodeDisabled, "client_initialization", false, 0)
	}
	raw, err := a.client.LookupResources(ctx, resourceType, permission, ObjectRef{Type: spicedbType(actor.Type), ID: actor.ID.String()})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			// Forma only writes uuids, so anything else is a store this process cannot reason about.
			return nil, newError(CodeUnsupported, "lookup_resources", false, 1)
		}
		ids = append(ids, id)
	}
	if actor.Type == ActorAPIKey {
		return a.withinOrganization(ctx, resourceType, ids, keyOrganization)
	}
	return ids, nil
}

func (a *Authorizer) withinOrganization(ctx context.Context, resourceType string, ids []uuid.UUID, organizationID uuid.UUID) ([]uuid.UUID, error) {
	if resourceType == typeOrganization {
		if slices.Contains(ids, organizationID) {
			return []uuid.UUID{organizationID}, nil
		}
		return []uuid.UUID{}, nil
	}
	var rows []string
	err := a.db.WithContext(ctx).Raw(`SELECT id::text FROM workspaces WHERE id = ANY(?::uuid[]) AND organization_id = ? ORDER BY id`, uuidArray(ids), organizationID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("filter looked-up workspaces: %w", err)
	}
	kept := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		kept = append(kept, uuid.MustParse(r))
	}
	return kept, nil
}

// scope is where a decision is evaluated: the permission resource (a survey or response becomes its workspace) and its organization.
type scope struct {
	resourceType   ResourceType
	resourceID     uuid.UUID
	organizationID uuid.UUID
	actorValid     bool
}

// resolveScope reads the PostgreSQL tenant boundary. A missing resource is nil (a denial); database failures are returned.
func resolveScope(ctx context.Context, db *gorm.DB, actor Actor, resource Resource) (*scope, error) {
	s, err := resourceScope(ctx, db, resource)
	if err != nil || s == nil {
		return nil, err
	}
	switch actor.Type {
	case ActorUser:
		s.actorValid, err = userActive(ctx, db, actor.ID)
	case ActorAPIKey:
		var org *uuid.UUID
		org, err = organizationOf(ctx, db, "api_keys", actor.ID)
		// A key from another organization is denied whatever the graph says.
		s.actorValid = org != nil && *org == s.organizationID
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func resourceScope(ctx context.Context, db *gorm.DB, resource Resource) (*scope, error) {
	s := &scope{resourceType: resource.Type, resourceID: resource.ID}
	var org *uuid.UUID
	var err error
	switch resource.Type {
	case ResourceOrganization:
		var exists bool
		err = db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM organizations WHERE id = ?)`, resource.ID).Scan(&exists).Error
		if exists {
			org = &resource.ID
		}
	case ResourceWorkspace:
		org, err = organizationOf(ctx, db, "workspaces", resource.ID)
	case ResourceTeam:
		org, err = organizationOf(ctx, db, "teams", resource.ID)
	case ResourceAPIKey:
		org, err = organizationOf(ctx, db, "api_keys", resource.ID)
	case ResourceSurvey, ResourceResponse:
		query := `SELECT s.workspace_id, w.organization_id FROM surveys s JOIN workspaces w ON w.id = s.workspace_id WHERE s.id = ?`
		if resource.Type == ResourceResponse {
			query = `SELECT s.workspace_id, w.organization_id FROM responses r JOIN surveys s ON s.id = r.survey_id JOIN workspaces w ON w.id = s.workspace_id WHERE r.id = ?`
		}
		var row struct {
			WorkspaceID    uuid.UUID
			OrganizationID uuid.UUID
		}
		res := db.WithContext(ctx).Raw(query, resource.ID).Scan(&row)
		err = res.Error
		if err == nil && res.RowsAffected > 0 {
			s.resourceType, s.resourceID, org = ResourceWorkspace, row.WorkspaceID, &row.OrganizationID
		}
	default:
		return nil, fmt.Errorf("unknown authorization resource type %q", resource.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve authorization scope: %w", err)
	}
	if org == nil {
		return nil, nil
	}
	s.organizationID = *org
	return s, nil
}

// organizationOf returns the organization_id of one row of table, or nil when the row does not exist. table is always a constant chosen by the caller.
func organizationOf(ctx context.Context, db *gorm.DB, table string, id uuid.UUID) (*uuid.UUID, error) {
	var orgs []string
	err := db.WithContext(ctx).Raw(`SELECT organization_id::text FROM `+table+` WHERE id = ?`, id).Scan(&orgs).Error
	if err != nil {
		return nil, fmt.Errorf("resolve %s organization: %w", table, err)
	}
	if len(orgs) == 0 {
		return nil, nil
	}
	org := uuid.MustParse(orgs[0])
	return &org, nil
}

// userActive is true only for an existing, active user.
func userActive(ctx context.Context, db *gorm.DB, id uuid.UUID) (bool, error) {
	var active []bool
	if err := db.WithContext(ctx).Raw(`SELECT is_active FROM users WHERE id = ?`, id).Scan(&active).Error; err != nil {
		return false, fmt.Errorf("resolve authorization user: %w", err)
	}
	return len(active) == 1 && active[0], nil
}

// decisionMemo dedupes the decisions of one request: concurrent callers share one evaluation, failures are evicted so the next caller asks again.
type decisionMemo struct {
	mu      sync.Mutex
	entries map[string]*memoEntry
}

type memoEntry struct {
	done    chan struct{}
	allowed bool
	err     error
}

type memoKey struct{}

// WithDecisionMemo scopes a decision memo to ctx, normally one request.
func WithDecisionMemo(ctx context.Context) context.Context {
	m := &decisionMemo{entries: map[string]*memoEntry{}}
	return context.WithValue(ctx, memoKey{}, m)
}

func memoFrom(ctx context.Context) *decisionMemo {
	m, _ := ctx.Value(memoKey{}).(*decisionMemo)
	return m
}

func (m *decisionMemo) do(key string, evaluate func() (bool, error)) (bool, error) {
	m.mu.Lock()
	if e, ok := m.entries[key]; ok {
		m.mu.Unlock()
		<-e.done
		return e.allowed, e.err
	}
	e := &memoEntry{done: make(chan struct{})}
	m.entries[key] = e
	m.mu.Unlock()

	e.allowed, e.err = evaluate()
	if e.err != nil {
		m.mu.Lock()
		delete(m.entries, key)
		m.mu.Unlock()
	}
	close(e.done)
	return e.allowed, e.err
}

// DecisionMemo gives every request its own decision memo.
func DecisionMemo() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(WithDecisionMemo(c.Request.Context()))
		c.Next()
	}
}
