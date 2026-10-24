// Package contact is a workspace's contacts with forma2's rules (apps/web/modules/contacts): contact attribute keys, attribute values stored in typed columns, the contact list and detail, deletion, CSV import, segments with their filter trees, and the evaluation of segment membership.
package contact

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// MaxAttributeKeys is forma2's MAX_ATTRIBUTE_CLASSES_PER_ENVIRONMENT: the keys a workspace may reach through attribute updates.
const MaxAttributeKeys = 150

// ItemsPerPage is forma2's ITEMS_PER_PAGE, the size of a page of contacts.
const ItemsPerPage = 30

// Attribute key types.
const (
	KeyTypeDefault = "default"
	KeyTypeCustom  = "custom"
)

// The default keys every workspace is seeded with; forma2 gives email and userId special rules.
const (
	keyEmail     = "email"
	keyUserID    = "userId"
	keyFirstName = "firstName"
	keyLastName  = "lastName"
)

// Key is forma2's TContactAttributeKey.
type Key struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Key         string    `json:"key"`
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	IsUnique    bool      `json:"isUnique"`
	Type        string    `json:"type"`
	DataType    string    `json:"dataType"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

// Contact is forma2's TContactWithAttributes: a contact with its attribute values as text, by key.
type Contact struct {
	ID          uuid.UUID         `json:"id"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	WorkspaceID uuid.UUID         `json:"workspaceId"`
	Attributes  map[string]string `json:"attributes"`
}

// AttributeDetail is one value of a contact with its key's description, as the contact page shows it.
type AttributeDetail struct {
	Key      string  `json:"key"`
	Name     *string `json:"name"`
	Type     string  `json:"type"`
	Value    string  `json:"value"`
	DataType string  `json:"dataType"`
}

// Service holds the contact rules. Every method takes the acting user and authorizes through authz on the workspace the resource belongs to.
type Service struct {
	repo    *Repository
	authz   *authz.Authorizer
	limiter *ratelimit.Limiter
	audit   *audit.Emitter
	schemas *jsonschema.Validator
	log     *slog.Logger
}

func NewService(repo *Repository, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, schemas *jsonschema.Validator, log *slog.Logger) *Service {
	return &Service{repo: repo, authz: authorizer, limiter: limiter, audit: emitter, schemas: schemas, log: log}
}

// record emits forma2's audit event for a mutation, a failure included, as withAuditLogging did for every error its handler threw.
func (s *Service) record(ctx context.Context, e audit.Event, err error) {
	e.ActorType = audit.ActorUser
	e.Status = audit.StatusSuccess
	if err != nil {
		e.Status, e.OldObject, e.NewObject = audit.StatusFailure, nil, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// read authorizes reading a workspace's contacts.
func (s *Service) read(ctx context.Context, userID, workspaceID uuid.UUID) error {
	return s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID))
}

// write authorizes a change in a workspace and charges forma2's stateMutation limit to it, in that order, as every contact action did.
func (s *Service) write(ctx context.Context, userID, workspaceID uuid.UUID) error {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(workspaceID)); err != nil {
		return err
	}
	return s.limiter.Apply(ctx, ratelimit.ActionStateMutation, workspaceID.String())
}

// organizationOf is the organization of a workspace, for audit events; a missing workspace is forma2's ResourceNotFoundError.
func (s *Service) organizationOf(ctx context.Context, workspaceID uuid.UUID) (uuid.UUID, error) {
	org, err := s.repo.workspaceOrganization(ctx, workspaceID)
	if err != nil {
		return uuid.Nil, err
	}
	if org == nil {
		return uuid.Nil, apperr.NotFound("Workspace", workspaceID.String())
	}
	return *org, nil
}
