package team

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Repository reads and writes teams, team_members and workspace_teams. Every query names the team, organization or workspace it is scoped to.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) transaction(ctx context.Context, fn func(tx *Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&Repository{db: tx})
	})
}

// errNameTaken is a rename onto a name another team of the organization holds.
var errNameTaken = errors.New("team name taken")

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// UserTeam is a team the caller belongs to, with their role in it.
type UserTeam struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	UserRole    string    `json:"userRole"`
	MemberCount int       `json:"memberCount"`
}

// OtherTeam is a team of the organization the caller does not belong to.
type OtherTeam struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	MemberCount int       `json:"memberCount"`
}

// Member is one team member with their team role.
type Member struct {
	UserID uuid.UUID `json:"userId"`
	Name   string    `json:"name"`
	Role   string    `json:"role"`
}

// WorkspaceAccess is one workspace a team may use, with the permission it holds there.
type WorkspaceAccess struct {
	WorkspaceID   uuid.UUID `json:"workspaceId"`
	WorkspaceName string    `json:"workspaceName"`
	Permission    string    `json:"permission"`
}

// Details is forma2's TTeamDetails.
type Details struct {
	ID             uuid.UUID         `json:"id"`
	Name           string            `json:"name"`
	OrganizationID uuid.UUID         `json:"organizationId"`
	Members        []Member          `json:"members"`
	Workspaces     []WorkspaceAccess `json:"workspaces"`
}

// WorkspaceTeam is a team with access to one workspace.
type WorkspaceTeam struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Permission  string    `json:"permission"`
	MemberCount int       `json:"memberCount"`
}

// teamOrganization returns the organization a team belongs to, or nil when there is no such team.
func (r *Repository) teamOrganization(ctx context.Context, teamID uuid.UUID) (*uuid.UUID, error) {
	var orgs []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT organization_id FROM teams WHERE id = ?`, teamID).Scan(&orgs).Error; err != nil {
		return nil, fmt.Errorf("load team organization: %w", err)
	}
	if len(orgs) == 0 {
		return nil, nil
	}
	return &orgs[0], nil
}

func (r *Repository) userTeams(ctx context.Context, organizationID, userID uuid.UUID) ([]UserTeam, error) {
	teams := []UserTeam{}
	err := r.db.WithContext(ctx).Raw(
		`SELECT t.id, t.name, m.role AS user_role, (SELECT count(*) FROM team_members c WHERE c.team_id = t.id) AS member_count
		 FROM teams t JOIN team_members m ON m.team_id = t.id AND m.user_id = ?
		 WHERE t.organization_id = ?
		 ORDER BY t.name, t.id`,
		userID, organizationID,
	).Scan(&teams).Error
	if err != nil {
		return nil, fmt.Errorf("list user teams: %w", err)
	}
	return teams, nil
}

func (r *Repository) otherTeams(ctx context.Context, organizationID, userID uuid.UUID) ([]OtherTeam, error) {
	teams := []OtherTeam{}
	err := r.db.WithContext(ctx).Raw(
		`SELECT t.id, t.name, (SELECT count(*) FROM team_members c WHERE c.team_id = t.id) AS member_count
		 FROM teams t
		 WHERE t.organization_id = ? AND NOT EXISTS (SELECT 1 FROM team_members m WHERE m.team_id = t.id AND m.user_id = ?)
		 ORDER BY t.name, t.id`,
		organizationID, userID,
	).Scan(&teams).Error
	if err != nil {
		return nil, fmt.Errorf("list other teams: %w", err)
	}
	return teams, nil
}

// insertTeam creates a team. created is false, and nothing is written, when the organization already has a team of exactly that name.
func (r *Repository) insertTeam(ctx context.Context, organizationID uuid.UUID, name string) (id uuid.UUID, created bool, err error) {
	var ids []uuid.UUID
	err = r.db.WithContext(ctx).Raw(
		`INSERT INTO teams (organization_id, name) VALUES (?, ?) ON CONFLICT (organization_id, name) DO NOTHING RETURNING id`,
		organizationID, name,
	).Scan(&ids).Error
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("insert team: %w", err)
	}
	if len(ids) == 0 {
		return uuid.Nil, false, nil
	}
	return ids[0], true, nil
}

// details returns the team with its members and workspaces, or nil when there is no such team. With lock set it also locks the team row, so concurrent updates of one team serialize.
func (r *Repository) details(ctx context.Context, teamID uuid.UUID, lock bool) (*Details, error) {
	query := `SELECT id, name, organization_id FROM teams WHERE id = ?`
	if lock {
		query += ` FOR UPDATE`
	}
	var rows []struct {
		ID             uuid.UUID
		Name           string
		OrganizationID uuid.UUID
	}
	if err := r.db.WithContext(ctx).Raw(query, teamID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load team: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	d := Details{ID: rows[0].ID, Name: rows[0].Name, OrganizationID: rows[0].OrganizationID, Members: []Member{}}
	if err := r.db.WithContext(ctx).Raw(
		`SELECT m.user_id, u.name, m.role FROM team_members m JOIN users u ON u.id = m.user_id WHERE m.team_id = ? ORDER BY m.created_at, m.user_id`,
		teamID,
	).Scan(&d.Members).Error; err != nil {
		return nil, fmt.Errorf("load team members: %w", err)
	}
	d.Workspaces = []WorkspaceAccess{}
	if err := r.db.WithContext(ctx).Raw(
		`SELECT wt.workspace_id, w.name AS workspace_name, wt.permission FROM workspace_teams wt JOIN workspaces w ON w.id = wt.workspace_id WHERE wt.team_id = ? ORDER BY wt.created_at, wt.workspace_id`,
		teamID,
	).Scan(&d.Workspaces).Error; err != nil {
		return nil, fmt.Errorf("load team workspaces: %w", err)
	}
	return &d, nil
}

// countMembers counts how many of the users are members of the organization.
func (r *Repository) countMembers(ctx context.Context, organizationID uuid.UUID, userIDs []uuid.UUID) (int, error) {
	var n int
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM memberships WHERE organization_id = ? AND user_id IN ?`, organizationID, userIDs).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count organization members: %w", err)
	}
	return n, nil
}

// countWorkspaces counts how many of the workspaces belong to the organization.
func (r *Repository) countWorkspaces(ctx context.Context, organizationID uuid.UUID, workspaceIDs []uuid.UUID) (int, error) {
	var n int
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM workspaces WHERE organization_id = ? AND id IN ?`, organizationID, workspaceIDs).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count organization workspaces: %w", err)
	}
	return n, nil
}

// replaceMembers makes the team's members exactly the given ones. A row whose role does not change is left untouched, so it enqueues no projection event.
func (r *Repository) replaceMembers(ctx context.Context, teamID uuid.UUID, members []MemberInput, now time.Time) error {
	ids := make([]uuid.UUID, len(members))
	for i, m := range members {
		ids[i] = m.UserID
	}
	db := r.db.WithContext(ctx)
	if err := db.Exec(`DELETE FROM team_members WHERE team_id = ? AND user_id NOT IN ?`, teamID, ids).Error; err != nil {
		return fmt.Errorf("remove team members: %w", err)
	}
	for _, m := range members {
		if err := db.Exec(
			`INSERT INTO team_members (team_id, user_id, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role, updated_at = EXCLUDED.updated_at
			 WHERE team_members.role IS DISTINCT FROM EXCLUDED.role`,
			teamID, m.UserID, m.Role, now, now,
		).Error; err != nil {
			return fmt.Errorf("upsert team member: %w", err)
		}
	}
	return nil
}

// replaceWorkspaces makes the team's workspace grants exactly the given ones, leaving unchanged grants untouched.
func (r *Repository) replaceWorkspaces(ctx context.Context, teamID uuid.UUID, workspaces []WorkspaceInput, now time.Time) error {
	ids := make([]uuid.UUID, len(workspaces))
	for i, w := range workspaces {
		ids[i] = w.WorkspaceID
	}
	db := r.db.WithContext(ctx)
	if err := db.Exec(`DELETE FROM workspace_teams WHERE team_id = ? AND workspace_id NOT IN ?`, teamID, ids).Error; err != nil {
		return fmt.Errorf("remove workspace grants: %w", err)
	}
	for _, w := range workspaces {
		if err := db.Exec(
			`INSERT INTO workspace_teams (workspace_id, team_id, permission, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT (workspace_id, team_id) DO UPDATE SET permission = EXCLUDED.permission, updated_at = EXCLUDED.updated_at
			 WHERE workspace_teams.permission IS DISTINCT FROM EXCLUDED.permission`,
			w.WorkspaceID, teamID, w.Permission, now, now,
		).Error; err != nil {
			return fmt.Errorf("upsert workspace grant: %w", err)
		}
	}
	return nil
}

// rename returns errNameTaken when another team of the organization has the name.
func (r *Repository) rename(ctx context.Context, teamID uuid.UUID, name string, now time.Time) error {
	err := r.db.WithContext(ctx).Exec(`UPDATE teams SET name = ?, updated_at = ? WHERE id = ?`, name, now, teamID).Error
	if isUniqueViolation(err) {
		return errNameTaken
	}
	if err != nil {
		return fmt.Errorf("rename team: %w", err)
	}
	return nil
}

// deleteTeam removes the team; its members, workspace grants and invite assignments cascade.
func (r *Repository) deleteTeam(ctx context.Context, teamID uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM teams WHERE id = ?`, teamID)
	if res.Error != nil {
		return false, fmt.Errorf("delete team: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// teamRole returns the user's role in the team, or nil when they are not in it.
func (r *Repository) teamRole(ctx context.Context, teamID, userID uuid.UUID) (*string, error) {
	var roles []string
	if err := r.db.WithContext(ctx).Raw(`SELECT role FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID).Scan(&roles).Error; err != nil {
		return nil, fmt.Errorf("load team role: %w", err)
	}
	if len(roles) == 0 {
		return nil, nil
	}
	return &roles[0], nil
}

func (r *Repository) workspaceTeams(ctx context.Context, workspaceID uuid.UUID) ([]WorkspaceTeam, error) {
	teams := []WorkspaceTeam{}
	err := r.db.WithContext(ctx).Raw(
		`SELECT t.id, t.name, wt.permission, (SELECT count(*) FROM team_members c WHERE c.team_id = t.id) AS member_count
		 FROM workspace_teams wt JOIN teams t ON t.id = wt.team_id
		 WHERE wt.workspace_id = ?
		 ORDER BY t.name, t.id`,
		workspaceID,
	).Scan(&teams).Error
	if err != nil {
		return nil, fmt.Errorf("list workspace teams: %w", err)
	}
	return teams, nil
}
