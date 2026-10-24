// Package invite is how people join an organization: invites with a role and teams, sent one at a time, in bulk or from onboarding, their links, resending, revoking and re-roling them, and accepting one as a signed-in user or while signing up, with forma2's rules (apps/web/modules/organization/settings/teams, modules/role-management, modules/auth/invite, modules/setup/.../invite). USER_MANAGEMENT_MINIMUM_ROLE governs the organization-level actions through authz's organization.manage_access.
package invite

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// forma2's limits: an invite and each of its links live seven days, one invite names at most 100 teams, a bulk invite at most 500 people, and the members page shows 30 invites a page.
const (
	inviteLifetime  = 7 * 24 * time.Hour
	maxTeams        = 100
	maxBulkInvitees = 500
	pageSize        = 30
	// cloudInviteLimit is Forma Cloud's daily invite budget per organization; self-hosted instances use INVITE_RATE_LIMIT_PER_24_HOURS.
	cloudInviteLimit = 500
)

// acceptPath is the SPA page an invite link opens, forma2's /invite.
const acceptPath = "/invite"

// Stable codes of the invite module's expected failures. The first four are also the per-invitee failure reasons of a bulk invite (forma2's TBulkInviteFailureReason).
const (
	CodeInviteExists      = "invite_already_exists"
	CodeUserAlreadyMember = "user_already_member"
	CodeDuplicateTeamIDs  = "duplicate_team_ids"
	CodeInvalidTeamIDs    = "invalid_team_ids"
	CodeInviteNotFound    = "invite_not_found"
	CodeInviteExpired     = "invite_expired"
	CodeEmailMismatch     = "email_does_not_match"
	CodeEmailSendFailed   = "email_send_failed"
	// failureUnknown is a bulk invitee that failed for any other reason.
	failureUnknown = "unknown"
)

// Organization roles (memberships.role).
const (
	RoleOwner   = "owner"
	RoleManager = "manager"
	RoleMember  = "member"
	RoleBilling = "billing"
)

func validationError(message string) error {
	return apperr.InvalidInput(apperr.CodeValidation, message)
}

func errUserManagementNotAllowed() error {
	return apperr.OperationNotAllowed("User management is not allowed for your role")
}

func errNotAMember() error {
	return apperr.Forbidden("User not a member of this organization")
}

func errTeamAdminTeams() error {
	return apperr.OperationNotAllowed("Team admins can only add users to teams where they are admin")
}

// Invitee is one person to invite. Email is already validated and lowercased.
type Invitee struct {
	Email   string
	Name    string
	Role    string
	TeamIDs []uuid.UUID
}

type Service struct {
	cfg         config.Config
	repo        *Repository
	authz       *authz.Authorizer
	limiter     *ratelimit.Limiter
	inviteLimit ratelimit.Rule
	mailer      *mailer.Mailer
	audit       *audit.Emitter
	log         *slog.Logger
	now         func() time.Time
}

func NewService(cfg config.Config, repo *Repository, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, mail *mailer.Mailer, emitter *audit.Emitter, log *slog.Logger) *Service {
	rule := ratelimit.ActionInviteMember
	rule.Limit = cfg.InviteRateLimitPer24Hours
	if cfg.IsFormaCloud {
		rule.Limit = cloudInviteLimit
	}
	return &Service{cfg: cfg, repo: repo, authz: authorizer, limiter: limiter, inviteLimit: rule, mailer: mail, audit: emitter, log: log, now: time.Now}
}

// record emits an audited operation's event, a failure when err is set, as forma2's withAuditLogging did for every outcome of the actions it wrapped.
func (s *Service) record(ctx context.Context, e audit.Event, err error) {
	if e.ActorType == "" {
		e.ActorType = audit.ActorUser
	}
	e.Status = audit.StatusSuccess
	if err != nil {
		e.Status = audit.StatusFailure
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// chargeRecipients spends the organization's shared daily invite budget, one unit per recipient, whichever path sends the invites.
func (s *Service) chargeRecipients(ctx context.Context, organizationID uuid.UUID, n int) error {
	return s.limiter.ApplyN(ctx, s.inviteLimit, organizationID.String(), n)
}

// chargeMutation is forma2's stateMutation limit on settings writes, per organization.
func (s *Service) chargeMutation(ctx context.Context, organizationID uuid.UUID) error {
	return s.limiter.Apply(ctx, ratelimit.ActionStateMutation, organizationID.String())
}

// requireUserManagement is forma2's pair of gates on managing members and invites: the deployment's USER_MANAGEMENT_MINIMUM_ROLE (through organization.manage_access), then the owner-or-manager capability.
func (s *Service) requireUserManagement(ctx context.Context, actor authz.Actor, organizationID uuid.UUID) error {
	allowed, err := s.authz.Can(ctx, actor, authz.OrganizationManageAccess, authz.Organization(organizationID))
	if err != nil {
		return err
	}
	if !allowed {
		return errUserManagementNotAllowed()
	}
	return s.authz.Require(ctx, actor, authz.OrganizationManage, authz.Organization(organizationID))
}

// checkRole refuses the billing role outside Forma Cloud.
func (s *Service) checkRole(role string) error {
	if role == RoleBilling && !s.cfg.IsFormaCloud {
		return validationError("Billing role is not allowed")
	}
	return nil
}

func (s *Service) inviteURL(token string) string {
	u := *s.cfg.WebAppURL
	u.Path = acceptPath
	u.RawQuery = url.Values{"token": {token}}.Encode()
	u.Fragment = ""
	return u.String()
}

// issueLink stores a new link to the invite, valid as long as the invite and at most seven days, and returns its token.
func (s *Service) issueLink(ctx context.Context, repo *Repository, inviteID uuid.UUID, now time.Time) (string, error) {
	token, err := secret.NewToken()
	if err != nil {
		return "", err
	}
	if err := repo.insertToken(ctx, inviteID, secret.HashToken(token), now.Add(inviteLifetime)); err != nil {
		return "", err
	}
	return token, nil
}

// create stores one invite with its first link: forma2's inviteUser rules, in its order. The checks and the insert share a transaction, and the unique index settles a race between two invites to one address.
func (s *Service) create(ctx context.Context, organizationID, creatorID uuid.UUID, in Invitee) (id uuid.UUID, token string, err error) {
	now := s.now()
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		exists, err := tx.inviteExists(ctx, organizationID, in.Email)
		if err != nil {
			return err
		}
		if exists {
			return apperr.InvalidInput(CodeInviteExists, "Invite already exists")
		}
		acceptor, member, err := tx.userByEmail(ctx, organizationID, in.Email)
		if err != nil {
			return err
		}
		if member {
			return apperr.InvalidInput(CodeUserAlreadyMember, "User is already a member of this organization")
		}
		if hasDuplicates(in.TeamIDs) {
			return apperr.InvalidInput(CodeDuplicateTeamIDs, "teamIds must be unique")
		}
		if len(in.TeamIDs) > 0 {
			n, err := tx.countTeams(ctx, organizationID, in.TeamIDs)
			if err != nil {
				return err
			}
			if n != len(in.TeamIDs) {
				return apperr.InvalidInput(CodeInvalidTeamIDs, "Invalid teamIds")
			}
		}
		id, err = tx.insertInvite(ctx, newInvite{
			OrganizationID: organizationID, Email: in.Email, Name: in.Name, Role: in.Role, CreatorID: creatorID,
			AcceptorID: acceptor, ExpiresAt: now.Add(inviteLifetime), TeamIDs: in.TeamIDs,
		})
		if errors.Is(err, errInviteExists) {
			return apperr.InvalidInput(CodeInviteExists, "Invite already exists")
		}
		if err != nil {
			return err
		}
		token, err = s.issueLink(ctx, tx, id, now)
		return err
	})
	return id, token, err
}

func hasDuplicates(ids []uuid.UUID) bool {
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

// sendInvite mails the invite link in the inviter's language.
func (s *Service) sendInvite(ctx context.Context, inviter *person, to, inviteeName, token string) error {
	_, err := s.mailer.SendTemplate(ctx, to, inviter.Locale, mailer.Invite{InviteeName: inviteeName, InviterName: inviter.Name, VerifyLink: s.inviteURL(token)})
	return err
}

// sendInviteBestEffort is the settings paths' delivery: the invite is stored and can be shared by link, so a failing mail server must not fail the request and strand the invite behind "Invite already exists". The address is never logged.
func (s *Service) sendInviteBestEffort(ctx context.Context, inviter *person, inviteID uuid.UUID, to, inviteeName, token string) {
	if err := s.sendInvite(ctx, inviter, to, inviteeName, token); err != nil {
		s.log.ErrorContext(ctx, "Failed to send invite email", "err", err, "inviteId", inviteID.String())
	}
}

func (s *Service) inviter(ctx context.Context, userID uuid.UUID) (*person, error) {
	p, err := s.repo.person(ctx, userID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, apperr.NotAuthenticated("Not authenticated")
	}
	return p, nil
}

// Create is forma2's inviteUserAction: owners and managers invite under the user-management gates, team admins invite members into teams they administer.
func (s *Service) Create(ctx context.Context, inviterID, organizationID uuid.UUID, in Invitee) (id uuid.UUID, err error) {
	ev := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetInvite, OrganizationID: organizationID.String()}
	defer func() { s.record(ctx, ev, err) }()

	if s.cfg.InviteDisabled {
		return uuid.Nil, apperr.OperationNotAllowed("Invite disabled")
	}
	if err := s.checkRole(in.Role); err != nil {
		return uuid.Nil, err
	}
	role, err := s.repo.membershipRole(ctx, organizationID, inviterID)
	if err != nil {
		return uuid.Nil, err
	}
	if role == "" {
		return uuid.Nil, errNotAMember()
	}
	ownerOrManager := role == RoleOwner || role == RoleManager
	var adminTeams []uuid.UUID
	if !ownerOrManager {
		if adminTeams, err = s.repo.adminTeamIDs(ctx, organizationID, inviterID); err != nil {
			return uuid.Nil, err
		}
		if len(adminTeams) == 0 {
			return uuid.Nil, apperr.Forbidden("Not authorized to invite members")
		}
	}
	if err := s.requireInviter(ctx, authz.User(inviterID), organizationID, ownerOrManager, in.TeamIDs); err != nil {
		return uuid.Nil, err
	}
	if !ownerOrManager {
		if err := checkTeamAdminInvite(adminTeams, in); err != nil {
			return uuid.Nil, err
		}
	}
	if role == RoleManager && in.Role != RoleMember {
		return uuid.Nil, apperr.OperationNotAllowed("Managers can only invite users as members")
	}
	if err := s.chargeRecipients(ctx, organizationID, 1); err != nil {
		return uuid.Nil, err
	}
	inviter, err := s.inviter(ctx, inviterID)
	if err != nil {
		return uuid.Nil, err
	}
	id, token, err := s.create(ctx, organizationID, inviterID, in)
	if err != nil {
		return uuid.Nil, err
	}
	ev.TargetID = id.String()
	ev.NewObject = map[string]any{"email": in.Email, "name": in.Name, "role": in.Role, "teamIds": in.TeamIDs}
	s.sendInviteBestEffort(ctx, inviter, id, in.Email, in.Name, token)
	analytics.Capture(inviterID.String(), "team_member_invited", analytics.Properties{
		"organization_id": organizationID.String(),
		"invitee_role":    in.Role,
	}, analytics.Groups{OrganizationID: organizationID.String()})
	return id, nil
}

// requireInviter is forma2's assertInviterMayInvite. Owners and managers are decided once at the organization; a team admin needs team.manage on every team the invite names, asked once per distinct team, one at a time, stopping at the first refusal, so the number of decisions is bounded by maxTeams.
//
// Like forma2, the team-admin path does not consult USER_MANAGEMENT_MINIMUM_ROLE: it governs organization-wide user management, and a team admin can only add members to their own teams.
func (s *Service) requireInviter(ctx context.Context, actor authz.Actor, organizationID uuid.UUID, ownerOrManager bool, teamIDs []uuid.UUID) error {
	if ownerOrManager {
		return s.requireUserManagement(ctx, actor, organizationID)
	}
	seen := map[uuid.UUID]bool{}
	for _, teamID := range teamIDs {
		if seen[teamID] {
			continue
		}
		seen[teamID] = true
		allowed, err := s.authz.Can(ctx, actor, authz.TeamManage, authz.Team(teamID))
		if err != nil {
			return err
		}
		if !allowed {
			return errTeamAdminTeams()
		}
	}
	return nil
}

// checkTeamAdminInvite is forma2's validateTeamAdminInvitePermissions: a team admin invites members only, into their own teams, and into at least one.
func checkTeamAdminInvite(adminTeams []uuid.UUID, in Invitee) error {
	if in.Role != RoleMember {
		return apperr.OperationNotAllowed("Team admins can only invite users as members")
	}
	for _, id := range in.TeamIDs {
		if !slices.Contains(adminTeams, id) {
			return errTeamAdminTeams()
		}
	}
	if len(in.TeamIDs) == 0 {
		return validationError("Team admins must add invited users to at least one team")
	}
	return nil
}

// BulkResult is one invitee's outcome; FailureReason is one of the bulk failure codes or "unknown".
type BulkResult struct {
	Email         string `json:"email"`
	Success       bool   `json:"success"`
	FailureReason string `json:"failureReason,omitempty"`
}

// BulkInvitee is a bulk row as received: team ids that are not ids at all fail that row alone, as forma2's string ids did.
type BulkInvitee struct {
	Invitee
	InvalidTeamIDs bool
}

// Bulk is forma2's bulkInviteUsersAction: owners and managers only, the role rules checked for the whole batch, the budget charged for every recipient up front, then each invitee created in turn with its own outcome.
func (s *Service) Bulk(ctx context.Context, inviterID, organizationID uuid.UUID, invitees []BulkInvitee) (results []BulkResult, err error) {
	ev := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetInvite, OrganizationID: organizationID.String()}
	defer func() { s.record(ctx, ev, err) }()

	if s.cfg.InviteDisabled {
		return nil, apperr.OperationNotAllowed("Invite disabled")
	}
	if len(invitees) == 0 {
		return nil, validationError("No invitees provided")
	}
	if len(invitees) > maxBulkInvitees {
		return nil, validationError("A bulk invite is limited to 500 members at a time")
	}
	role, err := s.repo.membershipRole(ctx, organizationID, inviterID)
	if err != nil {
		return nil, err
	}
	if role == "" {
		return nil, errNotAMember()
	}
	if err := s.requireUserManagement(ctx, authz.User(inviterID), organizationID); err != nil {
		return nil, err
	}
	for _, in := range invitees {
		if err := s.checkRole(in.Role); err != nil {
			return nil, err
		}
	}
	if role == RoleManager && slices.ContainsFunc(invitees, func(in BulkInvitee) bool { return in.Role != RoleMember }) {
		return nil, apperr.OperationNotAllowed("Managers can only invite users as members")
	}
	if err := s.chargeRecipients(ctx, organizationID, len(invitees)); err != nil {
		return nil, err
	}
	inviter, err := s.inviter(ctx, inviterID)
	if err != nil {
		return nil, err
	}

	results = make([]BulkResult, 0, len(invitees))
	invited := []string{}
	for _, in := range invitees {
		if in.InvalidTeamIDs {
			results = append(results, BulkResult{Email: in.Email, FailureReason: CodeInvalidTeamIDs})
			continue
		}
		id, token, err := s.create(ctx, organizationID, inviterID, in.Invitee)
		if err != nil {
			results = append(results, BulkResult{Email: in.Email, FailureReason: s.failureReason(ctx, err)})
			continue
		}
		s.sendInviteBestEffort(ctx, inviter, id, in.Email, in.Name, token)
		invited = append(invited, in.Email)
		results = append(results, BulkResult{Email: in.Email, Success: true})
	}
	ev.NewObject = map[string]any{"invitedCount": len(invited), "totalCount": len(invitees), "emails": invited}
	analytics.Capture(inviterID.String(), "team_members_bulk_invited", analytics.Properties{
		"organization_id": organizationID.String(),
		"invited_count":   len(invited),
		"total_count":     len(invitees),
	}, analytics.Groups{OrganizationID: organizationID.String()})
	return results, nil
}

// failureReason maps a bulk invitee's failure to its reason; an unexpected error is logged, without the address, and reported as unknown.
func (s *Service) failureReason(ctx context.Context, err error) string {
	if e, ok := apperr.As(err); ok {
		switch e.Code {
		case CodeInviteExists, CodeUserAlreadyMember, CodeDuplicateTeamIDs, CodeInvalidTeamIDs:
			return e.Code
		}
		return failureUnknown
	}
	s.log.ErrorContext(ctx, "Bulk invite failed for an invitee", "err", err)
	return failureUnknown
}

// Setup is forma2's onboarding invite: only an owner may send it, because it always invites an owner, with no teams.
func (s *Service) Setup(ctx context.Context, inviterID, organizationID uuid.UUID, email, name string) (id uuid.UUID, err error) {
	ev := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetInvite, OrganizationID: organizationID.String()}
	defer func() { s.record(ctx, ev, err) }()

	if s.cfg.InviteDisabled {
		return uuid.Nil, apperr.OperationNotAllowed("Invite disabled")
	}
	if err := s.authz.Require(ctx, authz.User(inviterID), authz.OrganizationWrite, authz.Organization(organizationID)); err != nil {
		return uuid.Nil, err
	}
	if err := s.chargeRecipients(ctx, organizationID, 1); err != nil {
		return uuid.Nil, err
	}
	inviter, err := s.inviter(ctx, inviterID)
	if err != nil {
		return uuid.Nil, err
	}
	id, token, err := s.create(ctx, organizationID, inviterID, Invitee{Email: email, Name: name, Role: RoleOwner})
	if err != nil {
		return uuid.Nil, err
	}
	ev.TargetID = id.String()
	ev.NewObject = map[string]any{"invitedUserId": id.String(), "email": email, "name": name}
	s.sendInviteBestEffort(ctx, inviter, id, email, name, token)
	return id, nil
}

// List returns the organization's pending invites, expired ones included, to its product members.
func (s *Service) List(ctx context.Context, userID, organizationID uuid.UUID, page int) ([]Invite, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationReadAccess, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	return s.repo.list(ctx, organizationID, page)
}

// Access is what the members page offers the caller: organization-wide user management (owners and managers, as USER_MANAGEMENT_MINIMUM_ROLE allows), and inviting into the teams they administer.
type Access struct {
	CanManageUsers     bool        `json:"canManageUsers"`
	AdminTeamIDs       []uuid.UUID `json:"adminTeamIds"`
	InviteDisabled     bool        `json:"inviteDisabled"`
	BillingRoleAllowed bool        `json:"billingRoleAllowed"`
}

func (s *Service) Access(ctx context.Context, userID, organizationID uuid.UUID) (*Access, error) {
	actor := authz.User(userID)
	if err := s.authz.Require(ctx, actor, authz.OrganizationReadAccess, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	manage, err := s.authz.Can(ctx, actor, authz.OrganizationManageAccess, authz.Organization(organizationID))
	if err != nil {
		return nil, err
	}
	teams, err := s.repo.adminTeamIDs(ctx, organizationID, userID)
	if err != nil {
		return nil, err
	}
	return &Access{CanManageUsers: manage, AdminTeamIDs: teams, InviteDisabled: s.cfg.InviteDisabled, BillingRoleAllowed: s.cfg.IsFormaCloud}, nil
}

// organizationOf resolves an invite to its organization; a missing invite is forma2's ResourceNotFoundError("invite", id).
func (s *Service) organizationOf(ctx context.Context, inviteID uuid.UUID) (uuid.UUID, error) {
	org, err := s.repo.inviteOrganization(ctx, inviteID)
	if err != nil {
		return uuid.Nil, err
	}
	if org == nil {
		return uuid.Nil, apperr.NotFound("invite", inviteID.String())
	}
	return *org, nil
}

// Delete revokes an invite and every link to it. Unlike sending, it is allowed while invites are disabled.
func (s *Service) Delete(ctx context.Context, userID, inviteID uuid.UUID) (err error) {
	ev := audit.Event{Action: audit.ActionDeleted, TargetType: audit.TargetInvite, TargetID: inviteID.String()}
	defer func() { s.record(ctx, ev, err) }()

	organizationID, err := s.organizationOf(ctx, inviteID)
	if err != nil {
		return err
	}
	ev.OrganizationID = organizationID.String()
	if err := s.requireUserManagement(ctx, authz.User(userID), organizationID); err != nil {
		return err
	}
	if err := s.chargeMutation(ctx, organizationID); err != nil {
		return err
	}
	snapshot, err := s.repo.snapshot(ctx, inviteID)
	if err != nil {
		return err
	}
	ev.OldObject = snapshot
	deleted, err := s.repo.deleteInvite(ctx, inviteID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperr.NotFound("Invite", inviteID.String())
	}
	return nil
}

// Resend mails a fresh link and gives the invite another seven days. Once the mail is sent, every earlier link stops working, which forma2's signed links could not do. A mail failure fails the request, as in forma2, after the invite was refreshed, and revokes nothing: the links already mailed or shared keep working, as forma2's earlier links did.
func (s *Service) Resend(ctx context.Context, userID, organizationID, inviteID uuid.UUID) (resent *Invite, err error) {
	ev := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetInvite, TargetID: inviteID.String(), OrganizationID: organizationID.String()}
	defer func() { s.record(ctx, ev, err) }()

	if s.cfg.InviteDisabled {
		return nil, apperr.OperationNotAllowed("Invite are disabled")
	}
	inviteOrganization, err := s.organizationOf(ctx, inviteID)
	if err != nil {
		return nil, err
	}
	if inviteOrganization != organizationID {
		return nil, validationError("Invite does not belong to the organization")
	}
	if err := s.requireUserManagement(ctx, authz.User(userID), organizationID); err != nil {
		return nil, err
	}
	if err := s.chargeMutation(ctx, organizationID); err != nil {
		return nil, err
	}
	if err := s.chargeRecipients(ctx, organizationID, 1); err != nil {
		return nil, err
	}
	old, err := s.repo.snapshot(ctx, inviteID)
	if err != nil {
		return nil, err
	}
	ev.OldObject = old
	inviter, err := s.repo.creator(ctx, inviteID)
	if err != nil {
		return nil, err
	}
	if inviter == nil {
		return nil, apperr.NotFound("Invite", inviteID.String())
	}

	now := s.now()
	var token string
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		found, err := tx.setExpiry(ctx, inviteID, now.Add(inviteLifetime))
		if err != nil {
			return err
		}
		if !found {
			return apperr.NotFound("Invite", inviteID.String())
		}
		token, err = s.issueLink(ctx, tx, inviteID, now)
		if err != nil {
			return err
		}
		resent, err = tx.invite(ctx, inviteID)
		return err
	})
	if err != nil {
		return nil, err
	}
	ev.NewObject = map[string]any{"email": resent.Email, "name": resent.Name}
	name := ""
	if resent.Name != nil {
		name = *resent.Name
	}
	if err := s.sendInvite(ctx, inviter, resent.Email, name, token); err != nil {
		s.log.ErrorContext(ctx, "Failed to resend invite email", "err", err, "inviteId", inviteID.String())
		return nil, apperr.InvalidInput(CodeEmailSendFailed, "Failed to send email")
	}
	if err := s.repo.deleteTokensExcept(ctx, inviteID, secret.HashToken(token)); err != nil {
		return nil, err
	}
	return resent, nil
}

// Link is a share link to an invite: the token and the page URL that carries it.
type Link struct {
	Token string `json:"inviteToken"`
	URL   string `json:"inviteUrl"`
}

// Link is forma2's createInviteTokenAction: a new link to copy, and seven more days for the invite. Existing links keep working until their own expiry, so the one already mailed stays valid. Like forma2 it is allowed while invites are disabled and does not spend the invite budget.
func (s *Service) Link(ctx context.Context, userID, inviteID uuid.UUID) (link *Link, err error) {
	ev := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetInvite, TargetID: inviteID.String()}
	defer func() { s.record(ctx, ev, err) }()

	organizationID, err := s.organizationOf(ctx, inviteID)
	if err != nil {
		return nil, err
	}
	ev.OrganizationID = organizationID.String()
	if err := s.requireUserManagement(ctx, authz.User(userID), organizationID); err != nil {
		return nil, err
	}
	if err := s.chargeMutation(ctx, organizationID); err != nil {
		return nil, err
	}
	now := s.now()
	var token string
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		old, err := tx.invite(ctx, inviteID)
		if err != nil {
			return err
		}
		if old == nil {
			return validationError("Invite not found")
		}
		expires := now.Add(inviteLifetime)
		ev.OldObject = map[string]any{"expiresAt": old.ExpiresAt}
		ev.NewObject = map[string]any{"expiresAt": expires}
		if _, err := tx.setExpiry(ctx, inviteID, expires); err != nil {
			return err
		}
		if err := tx.deleteTokens(ctx, inviteID, true, now); err != nil {
			return err
		}
		token, err = s.issueLink(ctx, tx, inviteID, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Link{Token: token, URL: s.inviteURL(token)}, nil
}

// UpdateRole is forma2's updateInviteAction: the role an invite grants, under the user-management gates, with the billing and manager rules. There is no last-owner rule for an invite.
func (s *Service) UpdateRole(ctx context.Context, userID, inviteID uuid.UUID, role string) (err error) {
	ev := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetInvite, TargetID: inviteID.String()}
	defer func() { s.record(ctx, ev, err) }()

	organizationID, err := s.organizationOf(ctx, inviteID)
	if err != nil {
		return err
	}
	ev.OrganizationID = organizationID.String()
	callerRole, err := s.repo.membershipRole(ctx, organizationID, userID)
	if err != nil {
		return err
	}
	if callerRole == "" {
		return errNotAMember()
	}
	if err := s.requireUserManagement(ctx, authz.User(userID), organizationID); err != nil {
		return err
	}
	if err := s.chargeMutation(ctx, organizationID); err != nil {
		return err
	}
	if err := s.checkRole(role); err != nil {
		return err
	}
	if callerRole == RoleManager && role != RoleMember {
		return apperr.OperationNotAllowed("Managers can only invite members")
	}
	previous, err := s.repo.setRole(ctx, inviteID, role)
	if err != nil {
		return err
	}
	if previous == "" {
		return apperr.NotFound("Invite", inviteID.String())
	}
	// The role is the only field that changes and the only one the audit entry must show, or the diff would be empty and an escalation would leave no trace.
	ev.OldObject = map[string]any{"role": previous}
	ev.NewObject = map[string]any{"role": role}
	return nil
}

// Status is what an invite link shows the person who opens it.
type Status string

const (
	StatusValid         Status = "valid"
	StatusNotFound      Status = "not_found"
	StatusExpired       Status = "expired"
	StatusEmailMismatch Status = "email_mismatch"
)

// lookup classifies a link without changing anything. A link that is unknown or past its own expiry is not found, as forma2's expired signature was; an invite past its expiry is expired.
func (s *Service) lookup(ctx context.Context, repo *Repository, token string, lock bool) (*linked, Status, error) {
	if token == "" {
		return nil, StatusNotFound, nil
	}
	inv, err := repo.byToken(ctx, secret.HashToken(token), lock)
	if err != nil {
		return nil, "", err
	}
	now := s.now()
	switch {
	case inv == nil || !inv.TokenExpiresAt.After(now):
		return nil, StatusNotFound, nil
	case inv.ExpiresAt.Before(now):
		return inv, StatusExpired, nil
	}
	return inv, StatusValid, nil
}

func sameEmail(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// Inspection is the invite page's view of a link. Email, the invited address, is set only for a valid link, so the page can offer to sign up or sign in with it.
type Inspection struct {
	Status Status `json:"status"`
	Email  string `json:"email,omitempty"`
}

// Inspect tells the invite page what a link is, for the signed-in user when there is one: forma2's InvitePage, without accepting anything.
func (s *Service) Inspect(ctx context.Context, token string, userID *uuid.UUID) (*Inspection, error) {
	inv, status, err := s.lookup(ctx, s.repo, token, false)
	if err != nil || status != StatusValid {
		return &Inspection{Status: status}, err
	}
	if userID != nil {
		p, err := s.repo.person(ctx, *userID)
		if err != nil {
			return nil, err
		}
		if p == nil || !sameEmail(p.Email, inv.Email) {
			return &Inspection{Status: StatusEmailMismatch}, nil
		}
	}
	return &Inspection{Status: StatusValid, Email: inv.Email}, nil
}

func lookupError(status Status) error {
	switch status {
	case StatusExpired:
		return apperr.InvalidInput(CodeInviteExpired, "Invite expired")
	case StatusEmailMismatch:
		return &apperr.Error{Status: http.StatusForbidden, Code: CodeEmailMismatch, Message: "Email does not match"}
	default:
		return &apperr.Error{Status: http.StatusNotFound, Code: CodeInviteNotFound, Message: "Invite not found"}
	}
}

// acceptance is a granted invite, for what follows the commit.
type acceptance struct {
	inviteID       uuid.UUID
	organizationID uuid.UUID
	creatorID      uuid.UUID
	role           string
}

// grant gives the user what the invite grants and consumes it, inside the caller's transaction: an accepted membership with the invite's role, the invite's teams that still exist in its organization (admin for an owner or manager, contributor otherwise), the organization's alerts switched off, and the invite with its links deleted.
//
// A user who is already a member keeps the role they have: forma2 overwrote it, which let a stale invite demote the last owner or hand a member the role an invite was created with before they joined some other way.
func (s *Service) grant(ctx context.Context, tx *Repository, inv *linked, userID uuid.UUID) (*acceptance, error) {
	now := s.now()
	role, err := tx.joinOrganization(ctx, inv.OrganizationID, userID, inv.Role, now)
	if err != nil {
		return nil, err
	}
	teams, err := tx.inviteTeams(ctx, inv.ID, inv.OrganizationID)
	if err != nil {
		return nil, err
	}
	teamRole := "contributor"
	if role == RoleOwner || role == RoleManager {
		teamRole = "admin"
	}
	for _, teamID := range teams {
		if err := tx.joinTeam(ctx, teamID, userID, teamRole, now); err != nil {
			return nil, err
		}
	}
	if err := tx.unsubscribe(ctx, userID, inv.OrganizationID, now); err != nil {
		return nil, err
	}
	if _, err := tx.deleteInvite(ctx, inv.ID); err != nil {
		return nil, err
	}
	return &acceptance{inviteID: inv.ID, organizationID: inv.OrganizationID, creatorID: inv.CreatorID, role: role}, nil
}

// accepted tells the inviter, in their language, that the invitee joined, and records the new membership. Neither may fail an acceptance that has committed.
func (s *Service) accepted(ctx context.Context, a *acceptance, userID uuid.UUID, inviteeName string) {
	s.record(ctx, audit.Event{
		Action: audit.ActionCreated, TargetType: audit.TargetMembership, TargetID: userID.String() + "-" + a.organizationID.String(),
		ActorID: userID.String(), OrganizationID: a.organizationID.String(), NewObject: map[string]any{"role": a.role, "accepted": true, "inviteId": a.inviteID.String()},
	}, nil)
	creator, err := s.repo.person(ctx, a.creatorID)
	if err == nil && creator != nil {
		_, err = s.mailer.SendTemplate(ctx, creator.Email, creator.Locale, mailer.InviteAccepted{InviterName: creator.Name, InviteeName: inviteeName})
	}
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to send invite accepted email", "err", err, "inviteId", a.inviteID.String())
	}
}

// Accept is the signed-in half of forma2's InvitePage: the link must be live and issued to the user's own address, compared without regard to case. forma2 accepted during the GET that rendered the page and reported success before the work ran; here acceptance is its own request and answers once it has committed.
func (s *Service) Accept(ctx context.Context, userID uuid.UUID, token string) (uuid.UUID, error) {
	user, err := s.repo.person(ctx, userID)
	if err != nil {
		return uuid.Nil, err
	}
	if user == nil {
		return uuid.Nil, apperr.NotAuthenticated("Not authenticated")
	}
	var a *acceptance
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		inv, status, err := s.lookup(ctx, tx, token, true)
		if err != nil {
			return err
		}
		if status == StatusValid && !sameEmail(user.Email, inv.Email) {
			status = StatusEmailMismatch
		}
		if status != StatusValid {
			return lookupError(status)
		}
		a, err = s.grant(ctx, tx, inv, userID)
		return err
	})
	if err != nil {
		return uuid.Nil, err
	}
	s.accepted(ctx, a, userID, user.Name)
	return a.organizationID, nil
}
