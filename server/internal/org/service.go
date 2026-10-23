package org

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	// Display time zones are validated against the IANA database; embedding it keeps validation independent of the host's zoneinfo.
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/ai"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/billing"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

// Service holds the organization and membership rules. Every method takes the acting user and authorizes through authz.
type Service struct {
	cfg        config.Config
	repo       *Repository
	workspaces *workspace.Repository
	navigation *workspace.Service
	authz      *authz.Authorizer
	limiter    *ratelimit.Limiter
	audit      *audit.Emitter
	billing    *billing.Lifecycle
	log        *slog.Logger
}

func NewService(cfg config.Config, repo *Repository, workspaces *workspace.Repository, navigation *workspace.Service, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter, lifecycle *billing.Lifecycle, log *slog.Logger) *Service {
	return &Service{cfg: cfg, repo: repo, workspaces: workspaces, navigation: navigation, authz: authorizer, limiter: limiter, audit: emitter, billing: lifecycle, log: log}
}

// record emits forma2's audit event for a mutation, a failure included, as withAuditLogging did for every error its handler threw.
func (s *Service) record(ctx context.Context, action audit.Action, target audit.Target, targetID, organizationID string, err error, oldObject, newObject any) {
	e := audit.Event{Action: action, TargetType: target, TargetID: targetID, OrganizationID: organizationID, ActorType: audit.ActorUser, Status: audit.StatusSuccess, OldObject: oldObject, NewObject: newObject}
	if err != nil {
		e.Status, e.OldObject, e.NewObject = audit.StatusFailure, nil, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// Created is a new organization and the workspace it starts with.
type Created struct {
	*Organization
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

// Create is forma2's createOrganizationAction, the same in the setup flow and the organization switcher: the organization with its default billing, the caller as its accepted owner, the default workspace, and the caller opted out of per-response alerts for it. Any signed-in user may create organizations (multiple organizations are always enabled). Unlike forma2, the steps share one transaction, so a failure cannot leave an organization without its owner or workspace.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, name string) (*Created, error) {
	created, err := s.create(ctx, userID, name)
	id := ""
	var newObject any
	if created != nil {
		id, newObject = created.ID.String(), created.Organization
	}
	s.record(ctx, audit.ActionCreated, audit.TargetOrganization, id, id, err, nil, newObject)
	return created, err
}

func (s *Service) create(ctx context.Context, userID uuid.UUID, name string) (*Created, error) {
	var orgID, workspaceID uuid.UUID
	err := s.repo.transaction(ctx, func(tx *Repository) error {
		var err error
		if orgID, err = tx.insertOrganization(ctx, name, s.cfg.IsFormaCloud); err != nil {
			return err
		}
		if err := tx.insertMembership(ctx, orgID, userID, RoleOwner, true); err != nil {
			return err
		}
		if workspaceID, err = s.workspaces.WithTx(tx.db).Create(ctx, orgID, workspace.NewWorkspace{Name: workspace.DefaultName}); err != nil {
			return err
		}
		return tx.unsubscribeAlerts(ctx, userID, orgID)
	})
	if err != nil {
		return nil, err
	}
	// forma2 set up the Stripe customer and Hobby subscription after the organization existed.
	s.billing.OrganizationCreated(ctx, orgID)
	o, err := s.repo.byID(ctx, orgID, s.cfg.IsFormaCloud)
	if err != nil {
		return nil, err
	}
	return &Created{Organization: o, WorkspaceID: workspaceID}, nil
}

// List is forma2's getOrganizationsByUserId: the organizations the user may read, oldest first, 30 per page when a page is given.
func (s *Service) List(ctx context.Context, userID uuid.UUID, page int) ([]Summary, error) {
	ids, err := s.authz.LookupOrganizationIDs(ctx, authz.User(userID))
	if err != nil {
		return nil, err
	}
	return s.repo.listed(ctx, userID, ids, page, s.cfg.IsFormaCloud)
}

// Detail is an organization with the caller's membership in it.
type Detail struct {
	*Organization
	Membership Membership `json:"membership"`
	// UserManagementAllowed is whether the caller may manage members and invites under USER_MANAGEMENT_MINIMUM_ROLE.
	UserManagementAllowed bool `json:"userManagementAllowed"`
}

// Get is forma2's getOrganizationAuth: organization.read and a membership are both required, and a caller without them gets a not-found, so an organization's existence never leaks to a non-member.
func (s *Service) Get(ctx context.Context, userID, organizationID uuid.UUID) (*Detail, error) {
	m, err := s.memberWith(ctx, userID, organizationID, authz.OrganizationRead)
	if err != nil {
		return nil, err
	}
	o, err := s.repo.byID(ctx, organizationID, s.cfg.IsFormaCloud)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, errOrganizationNotFound()
	}
	return &Detail{Organization: o, Membership: *m, UserManagementAllowed: UserManagementAllowed(m.Role, s.cfg.UserManagementMinimumRole)}, nil
}

func errOrganizationNotFound() *apperr.Error { return apperr.NotFound("Organization", "") }

// memberWith returns the caller's membership when they hold action on the organization, and a not-found otherwise.
func (s *Service) memberWith(ctx context.Context, userID, organizationID uuid.UUID, action authz.Action) (*Membership, error) {
	allowed, err := s.authz.Can(ctx, authz.User(userID), action, authz.Organization(organizationID))
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errOrganizationNotFound()
	}
	m, err := s.repo.membership(ctx, userID, organizationID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errOrganizationNotFound()
	}
	return m, nil
}

// UpdateInput is a validated change to an organization's settings; a nil field is left as it is.
type UpdateInput struct {
	Name *string
	// DisplayTimeZone is set when the request names the field; a nil Value means UTC.
	DisplayTimeZone       *TimeZone
	IsAISmartToolsEnabled *bool
}

// TimeZone is a display time zone change.
type TimeZone struct {
	Value *string
}

// CodeAIInstanceNotConfigured is forma2's ai_instance_not_configured refusal.
const CodeAIInstanceNotConfigured = "ai_instance_not_configured"

// Update is forma2's three organization settings actions: renaming needs organization.write (owners only), the display time zone and the AI smart tools toggle organization.manage (owners and managers). Enabling AI smart tools needs an AI provider configured on the instance (forma2's assertOrganizationAISettingsUpdateAllowed); disabling is always allowed.
func (s *Service) Update(ctx context.Context, userID, organizationID uuid.UUID, in UpdateInput) (*Organization, error) {
	old, o, err := s.update(ctx, userID, organizationID, in)
	s.record(ctx, audit.ActionUpdated, audit.TargetOrganization, organizationID.String(), organizationID.String(), err, old, o)
	return o, err
}

func (s *Service) update(ctx context.Context, userID, organizationID uuid.UUID, in UpdateInput) (old, updated *Organization, err error) {
	actor, resource := authz.User(userID), authz.Organization(organizationID)
	if in.Name != nil {
		if err := s.authz.Require(ctx, actor, authz.OrganizationWrite, resource); err != nil {
			return nil, nil, err
		}
	}
	if in.DisplayTimeZone != nil || in.IsAISmartToolsEnabled != nil {
		if err := s.authz.Require(ctx, actor, authz.OrganizationManage, resource); err != nil {
			return nil, nil, err
		}
	}
	if in.Name == nil && in.DisplayTimeZone == nil && in.IsAISmartToolsEnabled == nil {
		if err := s.authz.Require(ctx, actor, authz.OrganizationRead, resource); err != nil {
			return nil, nil, err
		}
	}
	if old, err = s.repo.byID(ctx, organizationID, s.cfg.IsFormaCloud); err != nil {
		return nil, nil, err
	}
	if old == nil {
		return nil, nil, apperr.NotFound("Organization", organizationID.String())
	}
	if in.IsAISmartToolsEnabled != nil && *in.IsAISmartToolsEnabled && !old.IsAISmartToolsEnabled && !ai.InstanceConfigured(s.cfg.AI) {
		return old, nil, &apperr.Error{Status: http.StatusForbidden, Code: CodeAIInstanceNotConfigured,
			Message: "AI is configured at the instance level via environment variables. Ask your administrator to set AI_PROVIDER, AI_MODEL, and the matching provider credentials before enabling AI features."}
	}
	var columns []column
	if in.Name != nil {
		columns = append(columns, column{"name", *in.Name})
	}
	if in.DisplayTimeZone != nil {
		columns = append(columns, column{"display_time_zone", in.DisplayTimeZone.Value})
	}
	if in.IsAISmartToolsEnabled != nil {
		columns = append(columns, column{"is_ai_smart_tools_enabled", *in.IsAISmartToolsEnabled})
	}
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		exists, err := tx.lock(ctx, organizationID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.NotFound("Organization", organizationID.String())
		}
		return tx.update(ctx, organizationID, columns)
	})
	if err != nil {
		return old, nil, err
	}
	updated, err = s.repo.byID(ctx, organizationID, s.cfg.IsFormaCloud)
	return old, updated, err
}

// ValidTimeZone is forma2's isValidIanaTimeZone.
func ValidTimeZone(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// Delete is forma2's deleteOrganizationAction: owners only (organization.write). There is no "last organization" check; a user left without one is sent to create one by the root page. Everything the organization holds cascades, the SpiceDB cleanup follows from the triggers, and on Forma Cloud its Stripe subscriptions are cancelled.
func (s *Service) Delete(ctx context.Context, userID, organizationID uuid.UUID) error {
	old, err := s.delete(ctx, userID, organizationID)
	s.record(ctx, audit.ActionDeleted, audit.TargetOrganization, organizationID.String(), organizationID.String(), err, old, nil)
	return err
}

func (s *Service) delete(ctx context.Context, userID, organizationID uuid.UUID) (*Organization, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationWrite, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	old, err := s.repo.byID(ctx, organizationID, s.cfg.IsFormaCloud)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, apperr.NotFound("Organization", organizationID.String())
	}
	if err := s.repo.delete(ctx, organizationID); err != nil {
		return old, err
	}
	s.billing.OrganizationDeleted(ctx, old.Billing.StripeCustomerID)
	return old, nil
}
