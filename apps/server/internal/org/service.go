package org

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"time"

	// Display time zones are validated against the IANA database; embedding it keeps validation independent of the host's zoneinfo.
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/ai"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/billing"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/survey/scheduling"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// Service holds the organization and membership rules. Every method takes the acting user and authorizes through authz.
type Service struct {
	cfg        config.Config
	repo       *Repository
	workspaces *workspace.Repository
	navigation *workspace.Service
	authz      *authz.Authorizer
	delivery   *authz.Delivery
	limiter    *ratelimit.Limiter
	audit      *audit.Emitter
	billing    *billing.Lifecycle
	mailer     *mailer.Mailer
	log        *slog.Logger
}

func NewService(cfg config.Config, repo *Repository, workspaces *workspace.Repository, navigation *workspace.Service, authorizer *authz.Authorizer, delivery *authz.Delivery, limiter *ratelimit.Limiter, emitter *audit.Emitter, lifecycle *billing.Lifecycle, m *mailer.Mailer, log *slog.Logger) *Service {
	return &Service{cfg: cfg, repo: repo, workspaces: workspaces, navigation: navigation, authz: authorizer, delivery: delivery, limiter: limiter, audit: emitter, billing: lifecycle, mailer: m, log: log}
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

// CodeOrganizationCreationRestricted refuses a new organization to a user who may not create one: on an invite-only instance, someone who owns no organization.
const CodeOrganizationCreationRestricted = "organization_creation_restricted"

func errOrganizationCreationRestricted() error {
	return &apperr.Error{Status: http.StatusForbidden, Code: CodeOrganizationCreationRestricted, Message: "This instance is invite-only: only the owner of an organization can create another one. Ask an organization owner to invite you."}
}

// CanCreate reports whether the user may create an organization. Where public sign-up is open (Forma Cloud, end-to-end runs) anyone signed in may, as forma2's always-on multi-organization mode allowed. On an invite-only instance only an accepted owner of an organization that has not suspended them may: everyone else got in through an invite (or single sign-on) and works in the organizations they were let into, so they cannot open a tenant of their own; the operator decides who that is. The instance's first user owns the organization sign-up created for them, so they can create more.
func (s *Service) CanCreate(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s.cfg.SignupEnabled() {
		return true, nil
	}
	return s.repo.ownsActiveOrganization(ctx, userID)
}

// Create is forma2's createOrganizationAction, the same in the setup flow and the organization switcher: the organization with its default billing, the caller as its accepted owner, the default workspace, and the caller opted out of per-response alerts for it. Who may call it is CanCreate's rule. Unlike forma2, the steps share one transaction, so a failure cannot leave an organization without its owner or workspace.
//
// timeZone, when set, is a valid IANA zone that becomes both the organization's display zone and its survey scheduling zone, so a creator outside the instance's zone does not get surveys that open and close at the instance's midnight. Nil keeps the defaults (UTC display, the instance's scheduling zone).
func (s *Service) Create(ctx context.Context, userID uuid.UUID, name string, timeZone *string) (*Created, error) {
	created, err := s.create(ctx, userID, name, timeZone)
	id := ""
	var newObject any
	if created != nil {
		id, newObject = created.ID.String(), created.Organization
	}
	s.record(ctx, audit.ActionCreated, audit.TargetOrganization, id, id, err, nil, newObject)
	return created, err
}

func (s *Service) create(ctx context.Context, userID uuid.UUID, name string, timeZone *string) (*Created, error) {
	allowed, err := s.CanCreate(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errOrganizationCreationRestricted()
	}
	var orgID, workspaceID uuid.UUID
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		var err error
		if orgID, err = tx.insertOrganization(ctx, name, timeZone, s.cfg.IsFormaCloud); err != nil {
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
	// The creator's ownership and the first workspace reach SpiceDB before the answer instead of waiting for the worker (see authz.Delivery.DeliverMemberships on what that promises under minimize_latency). The organization's own event projects nothing for a row that exists.
	s.delivery.DeliverMemberships(ctx, userID)
	s.delivery.DeliverWorkspace(ctx, workspaceID)
	// forma2 set up the Stripe customer and Hobby subscription after the organization existed.
	s.billing.OrganizationCreated(ctx, orgID)
	o, err := s.repo.byID(ctx, orgID, s.cfg.IsFormaCloud)
	if err != nil {
		return nil, err
	}
	s.captureCreated(ctx, userID, o, workspaceID)
	return &Created{Organization: o, WorkspaceID: workspaceID}, nil
}

// captureCreated is forma2's product events of a new organization. It is the user's first when they belong to no other, as in forma2's setup flow; the switcher only ever creates further ones.
func (s *Service) captureCreated(ctx context.Context, userID uuid.UUID, o *Organization, workspaceID uuid.UUID) {
	if !analytics.Enabled() {
		return
	}
	var creator struct {
		Email       string
		Memberships int64
	}
	if err := s.repo.db.WithContext(ctx).Raw(`SELECT u.email, (SELECT count(*) FROM memberships m WHERE m.user_id = u.id) AS memberships FROM users u WHERE u.id = ?`, userID).Scan(&creator).Error; err != nil {
		s.log.WarnContext(ctx, "Failed to read the organization's creator for PostHog", "err", err)
		return
	}
	analytics.OrganizationCreated(userID.String(), o.ID.String(), o.Name, workspaceID.String(), workspace.DefaultName, analytics.EmailDomain(creator.Email), creator.Memberships == 1)
}

// List is forma2's getOrganizationsByUserId: the organizations the user may read, oldest first, 30 per page when a page is given. An organization that refuses the user (a suspension, its two-factor requirement) is listed with its name and its two-factor requirement only.
func (s *Service) List(ctx context.Context, userID uuid.UUID, page int) ([]Summary, error) {
	ids, err := s.authz.LookupOrganizationIDs(ctx, authz.User(userID))
	if err != nil {
		return nil, err
	}
	out, err := s.repo.listed(ctx, userID, ids, page, s.cfg.IsFormaCloud)
	if err != nil {
		return nil, err
	}
	blocked, err := s.authz.SecondFactorBlockedOrganizations(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].SuspendedAt != nil || slices.Contains(blocked, out[i].ID) {
			out[i].Organization = out[i].limited()
		}
	}
	return out, nil
}

// Detail is an organization with the caller's membership in it.
type Detail struct {
	*Organization
	Membership Membership `json:"membership"`
	// UserManagementAllowed is whether the caller may manage members and invites under USER_MANAGEMENT_MINIMUM_ROLE.
	UserManagementAllowed bool `json:"userManagementAllowed"`
	// SurveyScheduling is when the organization's scheduled surveys open and close: its own zone or the instance's, at the instance's local time.
	SurveyScheduling scheduling.Settings `json:"surveyScheduling"`
	// TwoFactorStatus is the caller's own two-factor status (authz.SecondFactorEnabled, SecondFactorDisabled or SecondFactorSSO), which with TwoFactorEnforcedAt tells the SPA whether to remind them to turn it on.
	TwoFactorStatus string `json:"twoFactorStatus"`
	// TwoFactorBlocked is whether the organization's two-factor requirement refuses the caller now; the SPA then sends them to enrol instead of opening the organization.
	TwoFactorBlocked bool `json:"twoFactorBlocked"`
}

// Get is forma2's getOrganizationAuth: organization.read and a membership are both required, and a caller without them gets a not-found, so an organization's existence never leaks to a non-member.
func (s *Service) Get(ctx context.Context, userID, organizationID uuid.UUID) (*Detail, error) {
	m, err := s.memberDespiteRefusal(ctx, userID, organizationID)
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
	status, blocked, err := s.repo.secondFactor(ctx, userID, organizationID)
	if err != nil {
		return nil, err
	}
	if m.SuspendedAt != nil || blocked {
		o = o.limited()
	}
	return &Detail{Organization: o, Membership: *m, UserManagementAllowed: UserManagementAllowed(m.Role, s.cfg.UserManagementMinimumRole),
		SurveyScheduling: scheduling.Instance(s.cfg).ForOrganization(o.SurveySchedulingTimeZone).Settings(),
		TwoFactorStatus:  status, TwoFactorBlocked: blocked}, nil
}

func errOrganizationNotFound() *apperr.Error { return apperr.NotFound("Organization", "") }

// memberWith returns the caller's membership when they hold action on the organization, and a not-found otherwise.
func (s *Service) memberWith(ctx context.Context, userID, organizationID uuid.UUID, action authz.Action) (*Membership, error) {
	allowed, err := s.authz.Can(ctx, authz.User(userID), action, authz.Organization(organizationID))
	if err != nil {
		return nil, err
	}
	return s.membershipIf(ctx, userID, organizationID, allowed)
}

// memberDespiteRefusal is memberWith organization.read with the organization's refusals of the member (a suspension, its two-factor requirement) set aside: the organization's page and navigation, which send a member it refuses to the suspension notice or to enrolment, use it.
func (s *Service) memberDespiteRefusal(ctx context.Context, userID, organizationID uuid.UUID) (*Membership, error) {
	allowed, err := s.authz.CanReadOrganizationDespiteRefusal(ctx, authz.User(userID), organizationID)
	if err != nil {
		return nil, err
	}
	return s.membershipIf(ctx, userID, organizationID, allowed)
}

func (s *Service) membershipIf(ctx context.Context, userID, organizationID uuid.UUID, allowed bool) (*Membership, error) {
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
	DisplayTimeZone *TimeZone
	// SurveySchedulingTimeZone is set when the request names the field; a nil Value means the instance's zone.
	SurveySchedulingTimeZone *TimeZone
	IsAISmartToolsEnabled    *bool
	// IsAIResponseReadingEnabled is the "Allow AI to read responses" switch.
	IsAIResponseReadingEnabled *bool
	TwoFactorRequirement       *TwoFactorRequirement
}

// TimeZone is a time zone change.
type TimeZone struct {
	Value *string
}

// TwoFactorRequirement turns the organization's two-factor requirement off, or on with GracePeriodDays (0 to MaxTwoFactorGracePeriodDays) for members to turn it on before they are refused. Turning it on again restarts the grace period from now.
type TwoFactorRequirement struct {
	Required        bool
	GracePeriodDays int
}

// MaxTwoFactorGracePeriodDays bounds the grace period an owner may give.
const MaxTwoFactorGracePeriodDays = 90

// CodeEnableTwoFactorFirst refuses an owner who requires two-factor authentication without using it: the requirement would refuse them at once.
const CodeEnableTwoFactorFirst = "enable_two_factor_first"

// CodeAIInstanceNotConfigured is forma2's ai_instance_not_configured refusal.
const CodeAIInstanceNotConfigured = "ai_instance_not_configured"

// Update is forma2's three organization settings actions: renaming needs organization.write (owners only), the display time zone, the AI smart tools toggle and the "Allow AI to read responses" switch organization.manage (owners and managers). The two-factor requirement needs organization.write (owners only), and an owner who signs in with a password must use two-factor authentication before requiring it. The survey scheduling time zone, which forma2 did not have, needs organization.manage too; changing it moves the organization's scheduled publish and close dates to the same calendar days in the new zone. Enabling AI smart tools needs an AI provider configured on the instance (forma2's assertOrganizationAISettingsUpdateAllowed); disabling is always allowed.
func (s *Service) Update(ctx context.Context, userID, organizationID uuid.UUID, in UpdateInput) (*Organization, error) {
	old, o, err := s.update(ctx, userID, organizationID, in)
	s.record(ctx, audit.ActionUpdated, audit.TargetOrganization, organizationID.String(), organizationID.String(), err, old, o)
	return o, err
}

func (s *Service) update(ctx context.Context, userID, organizationID uuid.UUID, in UpdateInput) (old, updated *Organization, err error) {
	actor, resource := authz.User(userID), authz.Organization(organizationID)
	if in.Name != nil || in.TwoFactorRequirement != nil {
		if err := s.authz.Require(ctx, actor, authz.OrganizationWrite, resource); err != nil {
			return nil, nil, err
		}
	}
	if in.DisplayTimeZone != nil || in.SurveySchedulingTimeZone != nil || in.IsAISmartToolsEnabled != nil || in.IsAIResponseReadingEnabled != nil {
		if err := s.authz.Require(ctx, actor, authz.OrganizationManage, resource); err != nil {
			return nil, nil, err
		}
	}
	if in.Name == nil && in.DisplayTimeZone == nil && in.SurveySchedulingTimeZone == nil && in.IsAISmartToolsEnabled == nil && in.IsAIResponseReadingEnabled == nil && in.TwoFactorRequirement == nil {
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
	if in.SurveySchedulingTimeZone != nil {
		columns = append(columns, column{"survey_scheduling_time_zone", in.SurveySchedulingTimeZone.Value})
	}
	if in.IsAISmartToolsEnabled != nil {
		columns = append(columns, column{"is_ai_smart_tools_enabled", *in.IsAISmartToolsEnabled})
	}
	if in.IsAIResponseReadingEnabled != nil {
		columns = append(columns, column{"is_ai_response_reading_enabled", *in.IsAIResponseReadingEnabled})
	}
	if r := in.TwoFactorRequirement; r != nil {
		var enforcedAt *time.Time
		if r.Required {
			status, _, err := s.repo.secondFactor(ctx, userID, organizationID)
			if err != nil {
				return old, nil, err
			}
			if status == authz.SecondFactorDisabled {
				return old, nil, &apperr.Error{Status: http.StatusForbidden, Code: CodeEnableTwoFactorFirst,
					Message: "Turn on two-factor authentication for your own account before requiring it of the organization's members"}
			}
			at := time.Now().Add(time.Duration(r.GracePeriodDays) * 24 * time.Hour)
			enforcedAt = &at
		}
		columns = append(columns, column{"two_factor_enforced_at", enforcedAt})
	}
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		exists, err := tx.lock(ctx, organizationID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.NotFound("Organization", organizationID.String())
		}
		if in.SurveySchedulingTimeZone != nil {
			if err := s.resnapSchedules(ctx, tx, organizationID, in.SurveySchedulingTimeZone.Value); err != nil {
				return err
			}
		}
		return tx.update(ctx, organizationID, columns)
	})
	if err != nil {
		return old, nil, err
	}
	updated, err = s.repo.byID(ctx, organizationID, s.cfg.IsFormaCloud)
	return old, updated, err
}

// resnapSchedules moves the organization's scheduled dates to the schedule of its new zone, keeping each date's calendar day. It runs under the organization's row lock, and reads the current zone there, so two concurrent changes re-snap one after the other.
func (s *Service) resnapSchedules(ctx context.Context, tx *Repository, organizationID uuid.UUID, zone *string) error {
	current, err := tx.byID(ctx, organizationID, s.cfg.IsFormaCloud)
	if err != nil {
		return err
	}
	if current == nil {
		return apperr.NotFound("Organization", organizationID.String())
	}
	instance := scheduling.Instance(s.cfg)
	from, to := instance.ForOrganization(current.SurveySchedulingTimeZone), instance.ForOrganization(zone)
	if from.Location().String() == to.Location().String() {
		return nil
	}
	n, err := tx.resnapSchedules(ctx, organizationID, from, to)
	if err != nil {
		return err
	}
	s.log.InfoContext(ctx, "Survey schedules moved to the organization's new time zone", "organizationId", organizationID.String(), "from", from.Location().String(), "to", to.Location().String(), "surveys", n)
	return nil
}

// ValidTimeZone is forma2's isValidIanaTimeZone, for both the display and the survey scheduling time zone.
func ValidTimeZone(name string) bool {
	return scheduling.ValidZone(name)
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
	// What the deletion takes with it is read first: once the rows are gone, nothing says which of the revocations it queues are its own.
	contents, err := authz.ReadOrganizations(ctx, s.repo.db, organizationID)
	if err != nil {
		return old, err
	}
	if err := s.repo.delete(ctx, organizationID); err != nil {
		return old, err
	}
	s.delivery.DeliverOrganizations(ctx, contents)
	s.billing.OrganizationDeleted(ctx, old.Billing.StripeCustomerID)
	return old, nil
}
