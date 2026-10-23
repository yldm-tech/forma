// Package team is teams and the access they carry: team CRUD, team members with their team roles (admin or contributor), and the workspaces a team may read, write or manage, with forma2's rules (apps/web/modules/teams). SpiceDB follows the rows through the projection outbox; this package never writes it.
package team

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
)

// Stable codes of the team module's expected failures.
const (
	CodeTeamNameTaken      = "team_name_already_exists"
	CodeMembersNotInOrg    = "team_members_not_in_organization"
	CodeWorkspacesNotInOrg = "team_workspaces_not_in_organization"
)

// MemberInput is one member of a team update.
type MemberInput struct {
	UserID uuid.UUID
	Role   string
}

// WorkspaceInput is one workspace grant of a team update.
type WorkspaceInput struct {
	WorkspaceID uuid.UUID
	Permission  string
}

// UpdateInput is forma2's ZTeamSettingsFormSchema: the team's name and its complete member and workspace lists, which replace the stored ones.
type UpdateInput struct {
	Name       string
	Members    []MemberInput
	Workspaces []WorkspaceInput
}

// Teams is the caller's view of an organization's teams.
type Teams struct {
	UserTeams  []UserTeam  `json:"userTeams"`
	OtherTeams []OtherTeam `json:"otherTeams"`
}

type Service struct {
	repo  *Repository
	authz *authz.Authorizer
	audit *audit.Emitter
	log   *slog.Logger
	now   func() time.Time
}

func NewService(repo *Repository, authorizer *authz.Authorizer, emitter *audit.Emitter, log *slog.Logger) *Service {
	return &Service{repo: repo, authz: authorizer, audit: emitter, log: log, now: time.Now}
}

// record emits an audited operation's event, a failure when err is set, as forma2's withAuditLogging did for every outcome of the actions it wrapped.
func (s *Service) record(ctx context.Context, e audit.Event, err error) {
	e.ActorType = audit.ActorUser
	e.Status = audit.StatusSuccess
	if err != nil {
		e.Status = audit.StatusFailure
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// organizationOf resolves a team to its organization; a missing team is forma2's ResourceNotFoundError("team", id).
func (s *Service) organizationOf(ctx context.Context, teamID uuid.UUID) (uuid.UUID, error) {
	org, err := s.repo.teamOrganization(ctx, teamID)
	if err != nil {
		return uuid.Nil, err
	}
	if org == nil {
		return uuid.Nil, apperr.NotFound("team", teamID.String())
	}
	return *org, nil
}

// List returns the teams the caller belongs to, with their role, and the organization's other teams. Billing users, who are kept out of the organization's settings, are refused like non-members.
func (s *Service) List(ctx context.Context, userID, organizationID uuid.UUID) (*Teams, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationReadAccess, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	userTeams, err := s.repo.userTeams(ctx, organizationID, userID)
	if err != nil {
		return nil, err
	}
	otherTeams, err := s.repo.otherTeams(ctx, organizationID, userID)
	if err != nil {
		return nil, err
	}
	return &Teams{UserTeams: userTeams, OtherTeams: otherTeams}, nil
}

// Create adds an empty team. It takes organization.manage (owners and managers), not manage_access, so USER_MANAGEMENT_MINIMUM_ROLE does not govern it. Names are compared exactly, case included.
func (s *Service) Create(ctx context.Context, userID, organizationID uuid.UUID, name string) (id uuid.UUID, err error) {
	ev := audit.Event{Action: audit.ActionCreated, TargetType: audit.TargetTeam, OrganizationID: organizationID.String()}
	defer func() { s.record(ctx, ev, err) }()

	if err := s.authz.Require(ctx, authz.User(userID), authz.OrganizationManage, authz.Organization(organizationID)); err != nil {
		return uuid.Nil, err
	}
	id, created, err := s.repo.insertTeam(ctx, organizationID, name)
	if err != nil {
		return uuid.Nil, err
	}
	if !created {
		return uuid.Nil, errNameTakenInput()
	}
	ev.TargetID = id.String()
	details, err := s.repo.details(ctx, id, false)
	if err != nil {
		return uuid.Nil, err
	}
	ev.NewObject = details
	return id, nil
}

func errNameTakenInput() error {
	return apperr.InvalidInput(CodeTeamNameTaken, "Team name already exists")
}

// Details returns the team with its members and workspaces, for whoever may manage it: its admins, and the organization's owners and managers.
func (s *Service) Details(ctx context.Context, userID, teamID uuid.UUID) (*Details, error) {
	if _, err := s.organizationOf(ctx, teamID); err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.User(userID), authz.TeamManage, authz.Team(teamID)); err != nil {
		return nil, err
	}
	d, err := s.repo.details(ctx, teamID, false)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, apperr.NotFound("team", teamID.String())
	}
	return d, nil
}

// Update replaces the team's name, members and workspace grants. Team admins may manage members; changing workspace access additionally takes organization.manage. Every member must belong to the team's organization and every workspace to it as well; a repeated id fails the same check.
//
// forma2 enforced the team-role conventions (owners and managers are admins, an admin cannot demote or remove themselves) only in its UI, and so does this: the server stores the roles it is given.
func (s *Service) Update(ctx context.Context, userID, teamID uuid.UUID, in UpdateInput) (updated *Details, err error) {
	ev := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetTeam, TargetID: teamID.String()}
	defer func() { s.record(ctx, ev, err) }()

	organizationID, err := s.organizationOf(ctx, teamID)
	if err != nil {
		return nil, err
	}
	ev.OrganizationID = organizationID.String()
	actor := authz.User(userID)
	if err := s.authz.Require(ctx, actor, authz.TeamManage, authz.Team(teamID)); err != nil {
		return nil, err
	}

	now := s.now()
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		// The workspace gate is decided against the locked row, so a concurrent change cannot slip a grant change past it.
		current, err := tx.details(ctx, teamID, true)
		if err != nil {
			return err
		}
		if current == nil {
			return apperr.NotFound("Team", teamID.String())
		}
		ev.OldObject = current
		if hasWorkspaceAccessChanges(current.Workspaces, in.Workspaces) {
			if err := s.authz.Require(ctx, actor, authz.OrganizationManage, authz.Organization(current.OrganizationID)); err != nil {
				return err
			}
		}
		if err := checkMembers(ctx, tx, current.OrganizationID, in.Members); err != nil {
			return err
		}
		if err := checkWorkspaces(ctx, tx, current.OrganizationID, in.Workspaces); err != nil {
			return err
		}
		if err := tx.replaceMembers(ctx, teamID, in.Members, now); err != nil {
			return err
		}
		if err := tx.replaceWorkspaces(ctx, teamID, in.Workspaces, now); err != nil {
			return err
		}
		if current.Name == in.Name {
			return nil
		}
		err = tx.rename(ctx, teamID, in.Name, now)
		if errors.Is(err, errNameTaken) {
			return errNameTakenInput()
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	updated, err = s.repo.details(ctx, teamID, false)
	if err != nil {
		return nil, err
	}
	ev.NewObject = updated
	return updated, nil
}

// checkMembers is forma2's membership count: the distinct ids must all be members of the organization, so a duplicate fails it too. forma2 threw a plain Error here, which reached the client as a generic failure; it is an expected refusal.
func checkMembers(ctx context.Context, repo *Repository, organizationID uuid.UUID, members []MemberInput) error {
	ids := make([]uuid.UUID, len(members))
	for i, m := range members {
		ids[i] = m.UserID
	}
	n, err := repo.countMembers(ctx, organizationID, ids)
	if err != nil {
		return err
	}
	if n != len(ids) {
		return apperr.InvalidInput(CodeMembersNotInOrg, "Some specified users do not belong to the organization's membership.")
	}
	return nil
}

func checkWorkspaces(ctx context.Context, repo *Repository, organizationID uuid.UUID, workspaces []WorkspaceInput) error {
	ids := make([]uuid.UUID, len(workspaces))
	for i, w := range workspaces {
		ids[i] = w.WorkspaceID
	}
	n, err := repo.countWorkspaces(ctx, organizationID, ids)
	if err != nil {
		return err
	}
	if n != len(ids) {
		return apperr.InvalidInput(CodeWorkspacesNotInOrg, "Some specified workspaces do not belong to the organization.")
	}
	return nil
}

// Delete removes the team with its members and workspace grants; it takes team.delete, which only the organization's owners and managers hold. Pending invites lose the team with it.
func (s *Service) Delete(ctx context.Context, userID, teamID uuid.UUID) (err error) {
	ev := audit.Event{Action: audit.ActionDeleted, TargetType: audit.TargetTeam, TargetID: teamID.String()}
	defer func() { s.record(ctx, ev, err) }()

	organizationID, err := s.organizationOf(ctx, teamID)
	if err != nil {
		return err
	}
	ev.OrganizationID = organizationID.String()
	if err := s.authz.Require(ctx, authz.User(userID), authz.TeamDelete, authz.Team(teamID)); err != nil {
		return err
	}
	old, err := s.repo.details(ctx, teamID, false)
	if err != nil {
		return err
	}
	ev.OldObject = old
	deleted, err := s.repo.deleteTeam(ctx, teamID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperr.NotFound("Team", teamID.String())
	}
	return nil
}

// Role is the caller's own role in a team, or nil when they are not in it. It reveals nothing about other users, so like forma2 it asks no further permission.
func (s *Service) Role(ctx context.Context, userID, teamID uuid.UUID) (*string, error) {
	return s.repo.teamRole(ctx, teamID, userID)
}

// WorkspaceTeams lists the teams with access to a workspace, for anyone who may read the workspace.
func (s *Service) WorkspaceTeams(ctx context.Context, userID, workspaceID uuid.UUID) ([]WorkspaceTeam, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	return s.repo.workspaceTeams(ctx, workspaceID)
}
