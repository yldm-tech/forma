package mgmtapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/apikey/disable"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
)

// ---- teams ----

// teamRow is forma2's raw Team row.
type teamRow struct {
	ID             uuid.UUID `json:"id"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Name           string    `json:"name"`
	OrganizationID uuid.UUID `json:"organizationId"`
}

// teamWithWorkspaces is a team with the workspaces it has access to, as forma2's team update and delete answered.
type teamWithWorkspaces struct {
	teamRow
	WorkspaceTeams []workspaceRef `json:"workspaceTeams"`
}

type workspaceRef struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

const teamColumns = `id, created_at, updated_at, name, organization_id`

// errTeamNameTaken is a team name the organization already uses.
var errTeamNameTaken = errors.New("team name taken")

func (r *Repository) teamPage(ctx context.Context, organizationID uuid.UUID, f listFilter) ([]teamRow, int64, error) {
	where, args := filterWhere(`organization_id = ?`, []any{organizationID}, f)
	var total int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM teams WHERE `+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count teams: %w", err)
	}
	out := []teamRow{}
	page, pageArgs := pageClause(f)
	if err := r.db.WithContext(ctx).Raw(`SELECT `+teamColumns+` FROM teams WHERE `+where+page, append(args, pageArgs...)...).Scan(&out).Error; err != nil {
		return nil, 0, fmt.Errorf("list teams: %w", err)
	}
	return out, total, nil
}

func (r *Repository) team(ctx context.Context, organizationID, id uuid.UUID) (*teamRow, error) {
	var rows []teamRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+teamColumns+` FROM teams WHERE id = ? AND organization_id = ?`, id, organizationID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load team: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *Repository) createTeam(ctx context.Context, organizationID uuid.UUID, name string) (*teamRow, error) {
	var t teamRow
	err := r.db.WithContext(ctx).Raw(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING `+teamColumns, organizationID, name).Scan(&t).Error
	if isUniqueViolation(err) {
		return nil, errTeamNameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	return &t, nil
}

func (r *Repository) teamWorkspaces(ctx context.Context, id uuid.UUID) ([]workspaceRef, error) {
	out := []workspaceRef{}
	if err := r.db.WithContext(ctx).Raw(`SELECT workspace_id FROM workspace_teams WHERE team_id = ? ORDER BY created_at, workspace_id`, id).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("load team workspaces: %w", err)
	}
	return out, nil
}

func (r *Repository) renameTeam(ctx context.Context, organizationID, id uuid.UUID, name string) (*teamWithWorkspaces, error) {
	var rows []teamRow
	err := r.db.WithContext(ctx).Raw(`UPDATE teams SET name = ?, updated_at = now() WHERE id = ? AND organization_id = ? RETURNING `+teamColumns, name, id, organizationID).Scan(&rows).Error
	if isUniqueViolation(err) {
		return nil, errTeamNameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("rename team: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	workspaces, err := r.teamWorkspaces(ctx, id)
	if err != nil {
		return nil, err
	}
	return &teamWithWorkspaces{teamRow: rows[0], WorkspaceTeams: workspaces}, nil
}

func (r *Repository) deleteTeam(ctx context.Context, organizationID, id uuid.UUID) (*teamWithWorkspaces, error) {
	var out *teamWithWorkspaces
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := &Repository{db: tx}
		workspaces, err := repo.teamWorkspaces(ctx, id)
		if err != nil {
			return err
		}
		var rows []teamRow
		if err := tx.Raw(`DELETE FROM teams WHERE id = ? AND organization_id = ? RETURNING `+teamColumns, id, organizationID).Scan(&rows).Error; err != nil {
			return fmt.Errorf("delete team: %w", err)
		}
		if len(rows) > 0 {
			out = &teamWithWorkspaces{teamRow: rows[0], WorkspaceTeams: workspaces}
		}
		return nil
	})
	return out, err
}

// creatorRole is forma2's getApiKeyCreatorRole: the organization role of the user who created the key, "" when the key has no creator or the creator is no longer a member.
func (r *Repository) creatorRole(ctx context.Context, apiKeyID, organizationID uuid.UUID) (string, error) {
	var roles []string
	if err := r.db.WithContext(ctx).Raw(`SELECT m.role FROM api_keys k JOIN memberships m ON m.user_id = k.created_by AND m.organization_id = ? WHERE k.id = ?`, organizationID, apiKeyID).Scan(&roles).Error; err != nil {
		return "", fmt.Errorf("load api key creator role: %w", err)
	}
	if len(roles) == 0 {
		return "", nil
	}
	return roles[0], nil
}

// ---- users ----

// orgUserActive selects orgUser.IsActive from users u and memberships m.
const orgUserActive = `(u.is_active AND m.suspended_at IS NULL) AS is_active`

// orgUser is a user as forma2's v2 users API answered: its organization role and the names of its teams.
//
// IsActive is the user's access to this organization: their account is active and the organization has not suspended their membership. forma2 read and wrote the account's own switch, which locked a user out of every organization they belong to; here isActive belongs to the membership (owner decision D-AO01), so one organization's key suspends the user there only.
type orgUser struct {
	ID          uuid.UUID  `json:"id"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
	IsActive    bool       `json:"isActive"`
	Role        string     `json:"role"`
	Teams       []string   `json:"teams" gorm:"-"`
}

// userFilter is the users list's own filters beside the common ones.
type userFilter struct {
	ID    *uuid.UUID
	Email *string
}

// escapeLike escapes LIKE's wildcards so a filter matches its text literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (r *Repository) userPage(ctx context.Context, organizationID uuid.UUID, uf userFilter, f listFilter) ([]orgUser, int64, error) {
	where := `m.organization_id = ?`
	args := []any{organizationID}
	if uf.Email != nil {
		where += ` AND u.email ILIKE ?`
		args = append(args, "%"+escapeLike(*uf.Email)+"%")
	}
	if uf.ID != nil {
		where += ` AND u.id = ?`
		args = append(args, *uf.ID)
	}
	f.DateField = "u." + f.DateField
	where, args = filterWhere(where, args, f)
	from := ` FROM users u JOIN memberships m ON m.user_id = u.id WHERE `
	var total int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*)`+from+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	page, pageArgs := pageClause(f, "u.id")
	page = strings.Replace(page, " ORDER BY "+f.orderColumn(), " ORDER BY u."+f.orderColumn(), 1)
	out := []orgUser{}
	if err := r.db.WithContext(ctx).Raw(`SELECT u.id, u.created_at, u.updated_at, u.email, u.name, u.last_login_at, `+orgUserActive+`, m.role`+from+where+page, append(args, pageArgs...)...).Scan(&out).Error; err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	for i := range out {
		teams, err := r.userTeams(ctx, out[i].ID, nil)
		if err != nil {
			return nil, 0, err
		}
		out[i].Teams = teams
	}
	return out, total, nil
}

// userTeams are the names of the user's teams, in one organization or, as forma2's users list read them, in every one.
func (r *Repository) userTeams(ctx context.Context, userID uuid.UUID, organizationID *uuid.UUID) ([]string, error) {
	query := `SELECT t.name FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE tm.user_id = ?`
	args := []any{userID}
	if organizationID != nil {
		query += ` AND t.organization_id = ?`
		args = append(args, *organizationID)
	}
	names := []string{}
	if err := r.db.WithContext(ctx).Raw(query+` ORDER BY t.name`, args...).Scan(&names).Error; err != nil {
		return nil, fmt.Errorf("load user teams: %w", err)
	}
	return names, nil
}

// orgUserByEmail is the member of the organization with the email, nil when there is none.
func (r *Repository) orgUserByEmail(ctx context.Context, organizationID uuid.UUID, email string) (*orgUser, error) {
	var rows []orgUser
	if err := r.db.WithContext(ctx).Raw(`SELECT u.id, u.created_at, u.updated_at, u.email, u.name, u.last_login_at, `+orgUserActive+`, m.role FROM users u JOIN memberships m ON m.user_id = u.id
		WHERE m.organization_id = ? AND u.email = ?`, organizationID, email).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// teamsByName are the organization's teams among names, id by name.
func (r *Repository) teamsByName(ctx context.Context, organizationID uuid.UUID, names []string) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	if len(names) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   uuid.UUID
		Name string
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT id, name FROM teams WHERE organization_id = ? AND name IN ?`, organizationID, names).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load teams: %w", err)
	}
	for _, row := range rows {
		out[row.Name] = row.ID
	}
	return out, nil
}

// errEmailTaken is an email another user has.
var errEmailTaken = errors.New("email taken")

// newOrgUser is a user the users API creates.
type newOrgUser struct {
	Name     string
	Email    string
	Role     string
	IsActive *bool
	TeamIDs  []uuid.UUID
}

// createOrgUser creates a user with an accepted membership and contributor seats on the teams, in one transaction. isActive false suspends the new membership; the account itself is always active, as the organization's switch is the membership's.
func (r *Repository) createOrgUser(ctx context.Context, organizationID uuid.UUID, in newOrgUser) (*orgUser, error) {
	var id uuid.UUID
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		active := true
		if in.IsActive != nil {
			active = *in.IsActive
		}
		if err := tx.Raw(`INSERT INTO users (name, email) VALUES (?, ?) RETURNING id`, in.Name, in.Email).Row().Scan(&id); err != nil {
			if isUniqueViolation(err) {
				return errEmailTaken
			}
			return fmt.Errorf("create user: %w", err)
		}
		if err := tx.Exec(`INSERT INTO memberships (user_id, organization_id, role, accepted, suspended_at) VALUES (?, ?, ?, true, CASE WHEN ? THEN NULL ELSE now() END)`, id, organizationID, in.Role, active).Error; err != nil {
			return fmt.Errorf("create membership: %w", err)
		}
		for _, teamID := range in.TeamIDs {
			if err := tx.Exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, teamID, id).Error; err != nil {
				return fmt.Errorf("add team member: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.orgUserByID(ctx, organizationID, id)
}

func (r *Repository) orgUserByID(ctx context.Context, organizationID, id uuid.UUID) (*orgUser, error) {
	var rows []orgUser
	if err := r.db.WithContext(ctx).Raw(`SELECT u.id, u.created_at, u.updated_at, u.email, u.name, u.last_login_at, `+orgUserActive+`, m.role FROM users u JOIN memberships m ON m.user_id = u.id
		WHERE m.organization_id = ? AND u.id = ?`, organizationID, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// errLastOwner is a role change that would leave the organization without an active owner; errLastActiveOwner is the same through deactivating that owner.
var (
	errLastOwner       = errors.New("last owner")
	errLastActiveOwner = errors.New("last active owner")
)

// errSharedAccount is a change to a user's account (their name) refused because the user also belongs to another organization.
var errSharedAccount = errors.New("account shared with another organization")

// userPatch is the users API's update: nil leaves a field as it is; Teams, when set, replaces the user's teams in the organization.
type userPatch struct {
	Name     *string
	IsActive *bool
	Role     *string
	TeamIDs  map[string]uuid.UUID
	SetTeams bool
}

// managesTeams is whether the organization role administers every team of the organization (owner, manager).
func managesTeams(role string) bool { return role == "owner" || role == "manager" }

// updateOrgUser applies a patch to a member of the organization. Demoting or deactivating the organization's last active owner is refused (errLastOwner, errLastActiveOwner), and so is a change to the name of a user who also belongs to another organization (errSharedAccount): it lives on the user's account, which every organization of the user shares. isActive is the membership's: false suspends the user's access to this organization, true restores it (a deactivated account stays inactive; only the instance's operator turns it back on). The checks and the write run in one Serializable transaction, so two concurrent changes cannot both pass them. Demotion from owner or manager to a lower role makes the user a contributor of the organization's teams they administered, the reverse of what promotion through the app does; a plain member who is a team admin stays one when their role is sent unchanged. Demotion from owner or manager to a role below manager disables the API keys the user created in the organization, which it returns, as the app's role change does; a role that was already below manager, sent again, disables nothing. The role it compares is read again in the transaction.
func (r *Repository) updateOrgUser(ctx context.Context, organizationID uuid.UUID, current *orgUser, p userPatch) ([]disable.Key, error) {
	activeOwner := current.Role == "owner" && current.IsActive
	demoting := activeOwner && p.Role != nil && *p.Role != "owner"
	deactivating := activeOwner && p.IsActive != nil && !*p.IsActive
	var disabled []disable.Key
	err := database.Serializable(ctx, r.db, func(tx *gorm.DB) error {
		// Assigned, not appended: a retried transaction runs this again.
		disabled = nil
		if demoting || deactivating {
			var others int64
			if err := tx.Raw(`SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.organization_id = ? AND m.role = 'owner' AND u.is_active AND m.suspended_at IS NULL AND u.id <> ?`, organizationID, current.ID).Scan(&others).Error; err != nil {
				return fmt.Errorf("count owners: %w", err)
			}
			if others == 0 {
				if demoting {
					return errLastOwner
				}
				return errLastActiveOwner
			}
		}
		if p.Name != nil {
			var shared bool
			if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM memberships WHERE user_id = ? AND organization_id <> ?)`, current.ID, organizationID).Scan(&shared).Error; err != nil {
				return fmt.Errorf("check other memberships: %w", err)
			}
			if shared {
				return errSharedAccount
			}
		}
		if p.SetTeams {
			ids := make([]uuid.UUID, 0, len(p.TeamIDs))
			for _, id := range p.TeamIDs {
				ids = append(ids, id)
			}
			if err := tx.Exec(`DELETE FROM team_members WHERE user_id = ? AND team_id IN (SELECT id FROM teams WHERE organization_id = ?) AND NOT (team_id = ANY(?::uuid[]))`,
				current.ID, organizationID, uuidArray(ids)).Error; err != nil {
				return fmt.Errorf("remove team members: %w", err)
			}
			for _, id := range ids {
				if err := tx.Exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor') ON CONFLICT DO NOTHING`, id, current.ID).Error; err != nil {
					return fmt.Errorf("add team member: %w", err)
				}
			}
		}
		if err := tx.Exec(`UPDATE users SET name = COALESCE(?, name), updated_at = now() WHERE id = ?`, p.Name, current.ID).Error; err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		if p.IsActive != nil {
			// A suspension already in place keeps its time.
			if err := tx.Exec(`UPDATE memberships SET suspended_at = CASE WHEN ? THEN NULL ELSE COALESCE(suspended_at, now()) END, updated_at = now() WHERE user_id = ? AND organization_id = ?`, *p.IsActive, current.ID, organizationID).Error; err != nil {
				return fmt.Errorf("update membership suspension: %w", err)
			}
		}
		if p.Role != nil {
			// Read under the transaction: only a role that may manage keys, replaced by one that may not, disables them.
			var was string
			if err := tx.Raw(`SELECT role FROM memberships WHERE user_id = ? AND organization_id = ?`, current.ID, organizationID).Scan(&was).Error; err != nil {
				return fmt.Errorf("read membership role: %w", err)
			}
			if err := tx.Exec(`UPDATE memberships SET role = ?, updated_at = now() WHERE user_id = ? AND organization_id = ?`, *p.Role, current.ID, organizationID).Error; err != nil {
				return fmt.Errorf("update membership: %w", err)
			}
			if managesTeams(current.Role) && !managesTeams(*p.Role) {
				if err := tx.Exec(`UPDATE team_members SET role = 'contributor', updated_at = now() WHERE user_id = ? AND role = 'admin' AND team_id IN (SELECT id FROM teams WHERE organization_id = ?)`, current.ID, organizationID).Error; err != nil {
					return fmt.Errorf("demote team memberships: %w", err)
				}
			}
			if disable.ManagesKeys(was) && !disable.ManagesKeys(*p.Role) {
				var err error
				if disabled, err = disable.CreatedBy(ctx, tx, current.ID, &organizationID, disable.ReasonCreatorDemoted); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return disabled, nil
}

// ---- workspace teams ----

// workspaceTeamRow is forma2's raw WorkspaceTeam row with the legacy projectId beside workspaceId.
type workspaceTeamRow struct {
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
	TeamID      uuid.UUID `json:"teamId"`
	Permission  string    `json:"permission"`
	ProjectID   uuid.UUID `json:"projectId" gorm:"-"`
}

const workspaceTeamColumns = `wt.created_at, wt.updated_at, wt.workspace_id, wt.team_id, wt.permission`

func withProjectIDs(rows []workspaceTeamRow) []workspaceTeamRow {
	for i := range rows {
		rows[i].ProjectID = rows[i].WorkspaceID
	}
	return rows
}

func (r *Repository) workspaceTeamPage(ctx context.Context, organizationID uuid.UUID, teamID, workspaceID *uuid.UUID, f listFilter) ([]workspaceTeamRow, int64, error) {
	where := `t.organization_id = ?`
	args := []any{organizationID}
	if teamID != nil {
		where += ` AND wt.team_id = ?`
		args = append(args, *teamID)
	}
	if workspaceID != nil {
		where += ` AND wt.workspace_id = ? AND w.organization_id = ?`
		args = append(args, *workspaceID, organizationID)
	}
	f.DateField = "wt." + f.DateField
	where, args = filterWhere(where, args, f)
	from := ` FROM workspace_teams wt JOIN teams t ON t.id = wt.team_id JOIN workspaces w ON w.id = wt.workspace_id WHERE `
	var total int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*)`+from+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count workspace teams: %w", err)
	}
	page, pageArgs := pageClause(f, "wt.workspace_id", "wt.team_id")
	page = strings.Replace(page, " ORDER BY "+f.orderColumn(), " ORDER BY wt."+f.orderColumn(), 1)
	out := []workspaceTeamRow{}
	if err := r.db.WithContext(ctx).Raw(`SELECT `+workspaceTeamColumns+from+where+page, append(args, pageArgs...)...).Scan(&out).Error; err != nil {
		return nil, 0, fmt.Errorf("list workspace teams: %w", err)
	}
	return withProjectIDs(out), total, nil
}

// teamAndWorkspaceInOrganization is forma2's validateTeamIdAndWorkspaceId: both belong to the organization.
func (r *Repository) teamAndWorkspaceInOrganization(ctx context.Context, organizationID, teamID, workspaceID uuid.UUID) (bool, error) {
	var ok bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM teams WHERE id = ? AND organization_id = ?) AND EXISTS (SELECT 1 FROM workspaces WHERE id = ? AND organization_id = ?)`,
		teamID, organizationID, workspaceID, organizationID).Scan(&ok).Error; err != nil {
		return false, fmt.Errorf("check team and workspace: %w", err)
	}
	return ok, nil
}

func (r *Repository) workspaceTeam(ctx context.Context, teamID, workspaceID uuid.UUID) (*workspaceTeamRow, error) {
	var rows []workspaceTeamRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+workspaceTeamColumns+` FROM workspace_teams wt WHERE wt.team_id = ? AND wt.workspace_id = ?`, teamID, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workspace team: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &withProjectIDs(rows)[0], nil
}

// errWorkspaceTeamExists is a grant the team already has on the workspace.
var errWorkspaceTeamExists = errors.New("workspace team exists")

func (r *Repository) createWorkspaceTeam(ctx context.Context, teamID, workspaceID uuid.UUID, permission string) (*workspaceTeamRow, error) {
	var rows []workspaceTeamRow
	err := r.db.WithContext(ctx).Raw(`INSERT INTO workspace_teams AS wt (workspace_id, team_id, permission) VALUES (?, ?, ?) RETURNING `+workspaceTeamColumns, workspaceID, teamID, permission).Scan(&rows).Error
	if isUniqueViolation(err) {
		return nil, errWorkspaceTeamExists
	}
	if err != nil {
		return nil, fmt.Errorf("create workspace team: %w", err)
	}
	return &withProjectIDs(rows)[0], nil
}

func (r *Repository) updateWorkspaceTeam(ctx context.Context, teamID, workspaceID uuid.UUID, permission string) (*workspaceTeamRow, error) {
	var rows []workspaceTeamRow
	if err := r.db.WithContext(ctx).Raw(`UPDATE workspace_teams AS wt SET permission = ?, updated_at = now() WHERE team_id = ? AND workspace_id = ? RETURNING `+workspaceTeamColumns,
		permission, teamID, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("update workspace team: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &withProjectIDs(rows)[0], nil
}

func (r *Repository) deleteWorkspaceTeam(ctx context.Context, teamID, workspaceID uuid.UUID) (*workspaceTeamRow, error) {
	var rows []workspaceTeamRow
	if err := r.db.WithContext(ctx).Raw(`DELETE FROM workspace_teams AS wt WHERE team_id = ? AND workspace_id = ? RETURNING `+workspaceTeamColumns, teamID, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("delete workspace team: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &withProjectIDs(rows)[0], nil
}
