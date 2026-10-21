package apikey

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// Service holds the key management rules. Every operation needs organization.manage_api_keys (owners and managers) on the organization in the path; forma2 checked apiKey.manage for update and delete, which SpiceDB derives from the same permission of the key's organization. Checking the path's organization first, and scoping the SQL to it, means a caller who may not manage keys learns nothing about which key ids exist, which forma2's 404-before-authorization leaked.
type Service struct {
	repo  *Repository
	authz *authz.Authorizer
	audit *audit.Emitter
}

func NewService(repo *Repository, authorizer *authz.Authorizer, emitter *audit.Emitter) *Service {
	return &Service{repo: repo, authz: authorizer, audit: emitter}
}

// CreateInput is a validated create request.
type CreateInput struct {
	Label                string
	WorkspacePermissions []WorkspacePermissionInput
	OrganizationAccess   OrganizationAccess
}

// WorkspacePermissionInput is one requested workspace grant.
type WorkspacePermissionInput struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	Permission  string    `json:"permission"`
}

func (s *Service) authorize(ctx context.Context, user httpapi.Actor, organizationID uuid.UUID) error {
	return s.authz.Require(ctx, authz.User(user.UserID), authz.OrganizationManageAPIKeys, authz.Organization(organizationID))
}

// List returns the organization's keys.
func (s *Service) List(ctx context.Context, user httpapi.Actor, organizationID uuid.UUID) ([]View, error) {
	if err := s.authorize(ctx, user, organizationID); err != nil {
		return nil, err
	}
	return s.repo.list(ctx, organizationID)
}

// Create mints a key and returns it with its plaintext, which is never retrievable again. Every requested workspace must belong to the organization, checked before any secret is generated (forma2's ENG-1749 guard against minting a key on another tenant's workspace).
func (s *Service) Create(ctx context.Context, user httpapi.Actor, organizationID uuid.UUID, in CreateInput) (created *Created, err error) {
	event := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetAPIKey, OrganizationID: organizationID.String()}
	defer func() {
		if created != nil {
			event.TargetID = created.ID.String()
			event.NewObject = auditInput(in)
		}
		s.logEvent(ctx, event, err)
	}()

	if err := s.authorize(ctx, user, organizationID); err != nil {
		return nil, err
	}
	names, err := s.ownedWorkspaces(ctx, organizationID, in.WorkspacePermissions)
	if err != nil {
		return nil, err
	}

	key, err := newKey()
	if err != nil {
		return nil, err
	}
	access, err := json.Marshal(in.OrganizationAccess)
	if err != nil {
		return nil, fmt.Errorf("encode organization access: %w", err)
	}
	row := newKeyRow{OrganizationID: organizationID, CreatedBy: user.UserID, Label: in.Label, HashedKey: key.HashedKey, LookupHash: key.LookupHash, OrganizationAccess: access}
	grants := make([]WorkspaceGrant, 0, len(in.WorkspacePermissions))
	for _, p := range in.WorkspacePermissions {
		row.Grants = append(row.Grants, grantInput(p))
		grants = append(grants, WorkspaceGrant{WorkspaceID: p.WorkspaceID, WorkspaceName: names[p.WorkspaceID], Permission: p.Permission})
	}
	id, createdAt, err := s.repo.create(ctx, row)
	if err != nil {
		return nil, err
	}
	view := View{ID: id, Label: in.Label, CreatedAt: createdAt, OrganizationAccess: in.OrganizationAccess, WorkspacePermissions: grants}
	return &Created{View: view, Key: key.Key}, nil
}

// ownedWorkspaces returns the names of the requested workspaces, or OperationNotAllowed naming the first that is not the organization's. A workspace that does not exist at all is refused the same way, rather than failing on the foreign key as forma2 did.
func (s *Service) ownedWorkspaces(ctx context.Context, organizationID uuid.UUID, perms []WorkspacePermissionInput) (map[uuid.UUID]string, error) {
	if len(perms) == 0 {
		return map[uuid.UUID]string{}, nil
	}
	ids := make([]uuid.UUID, len(perms))
	for i, p := range perms {
		ids[i] = p.WorkspaceID
	}
	names, err := s.repo.workspacesOf(ctx, organizationID, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, ok := names[id]; !ok {
			return nil, apperr.OperationNotAllowed(fmt.Sprintf("Workspace %s does not belong to organization %s", id, organizationID))
		}
	}
	return names, nil
}

// UpdateLabel relabels a key; its grants and organization access never change after creation.
func (s *Service) UpdateLabel(ctx context.Context, user httpapi.Actor, organizationID, id uuid.UUID, label string) (view *View, err error) {
	event := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetAPIKey, TargetID: id.String(), OrganizationID: organizationID.String()}
	defer func() {
		if view != nil {
			event.NewObject = map[string]string{"label": label}
		}
		s.logEvent(ctx, event, err)
	}()

	if err := s.authorize(ctx, user, organizationID); err != nil {
		return nil, err
	}
	if err := s.repo.updateLabel(ctx, organizationID, id, label); err != nil {
		return nil, err
	}
	view, err = s.repo.byID(ctx, organizationID, id)
	if err != nil {
		return nil, err
	}
	if view == nil {
		return nil, apperr.NotFound("apiKey", id.String())
	}
	return view, nil
}

// Delete revokes a key. Deletion is the only revocation: keys never expire.
func (s *Service) Delete(ctx context.Context, user httpapi.Actor, organizationID, id uuid.UUID) (err error) {
	event := audit.Event{Action: audit.ActionDeleted, TargetType: audit.TargetAPIKey, TargetID: id.String(), OrganizationID: organizationID.String()}
	var deleted *View
	defer func() {
		if deleted != nil {
			// The public view only: forma2 recorded the deleted row, whose lookupHash the redaction did not cover.
			event.OldObject = deleted
		}
		s.logEvent(ctx, event, err)
	}()

	if err := s.authorize(ctx, user, organizationID); err != nil {
		return err
	}
	deleted, err = s.repo.delete(ctx, organizationID, id)
	if err != nil {
		return err
	}
	if deleted == nil {
		return apperr.NotFound("apiKey", id.String())
	}
	return nil
}

// auditInput is the create request as forma2 recorded it (newObject = apiKeyData).
func auditInput(in CreateInput) map[string]any {
	perms := in.WorkspacePermissions
	if perms == nil {
		perms = []WorkspacePermissionInput{}
	}
	return map[string]any{"label": in.Label, "workspacePermissions": perms, "organizationAccess": in.OrganizationAccess}
}

// logEvent records the operation's audit event, a failure included, as forma2's withAuditLogging did.
func (s *Service) logEvent(ctx context.Context, e audit.Event, err error) {
	e.ActorType = audit.ActorUser
	e.Status = audit.StatusSuccess
	if err != nil {
		e.Status = audit.StatusFailure
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}
