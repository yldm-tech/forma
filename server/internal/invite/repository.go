package invite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Repository reads and writes invites, their teams and links, and the membership rows an accepted invite creates. Every query names the invite, organization or user it is scoped to.
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

// errInviteExists is an insert that met the one-invite-per-address index.
var errInviteExists = errors.New("invite exists")

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Invite is a pending invite as the members page lists it.
type Invite struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      *string   `json:"name"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// person is a user's name, address and language, for mail and audit.
type person struct {
	ID     uuid.UUID
	Name   string
	Email  string
	Locale string
}

// linked is an invite reached through one of its links.
type linked struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Email          string
	Role           string
	CreatorID      uuid.UUID
	ExpiresAt      time.Time
	TokenExpiresAt time.Time
}

// membershipRole returns the user's role in the organization, or "" when they are not a member.
func (r *Repository) membershipRole(ctx context.Context, organizationID, userID uuid.UUID) (string, error) {
	var roles []string
	if err := r.db.WithContext(ctx).Raw(`SELECT role FROM memberships WHERE organization_id = ? AND user_id = ?`, organizationID, userID).Scan(&roles).Error; err != nil {
		return "", fmt.Errorf("load membership: %w", err)
	}
	if len(roles) == 0 {
		return "", nil
	}
	return roles[0], nil
}

// adminTeamIDs lists the organization's teams the user is an admin of.
func (r *Repository) adminTeamIDs(ctx context.Context, organizationID, userID uuid.UUID) ([]uuid.UUID, error) {
	ids := []uuid.UUID{}
	err := r.db.WithContext(ctx).Raw(
		`SELECT m.team_id FROM team_members m JOIN teams t ON t.id = m.team_id WHERE m.user_id = ? AND m.role = 'admin' AND t.organization_id = ? ORDER BY m.team_id`,
		userID, organizationID,
	).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("list admin teams: %w", err)
	}
	return ids, nil
}

// inviteOrganization returns the organization an invite belongs to, or nil when there is no such invite.
func (r *Repository) inviteOrganization(ctx context.Context, inviteID uuid.UUID) (*uuid.UUID, error) {
	var orgs []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT organization_id FROM invites WHERE id = ?`, inviteID).Scan(&orgs).Error; err != nil {
		return nil, fmt.Errorf("load invite organization: %w", err)
	}
	if len(orgs) == 0 {
		return nil, nil
	}
	return &orgs[0], nil
}

func (r *Repository) person(ctx context.Context, userID uuid.UUID) (*person, error) {
	var rows []person
	if err := r.db.WithContext(ctx).Raw(`SELECT id, name, email, locale FROM users WHERE id = ?`, userID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// creator is the person who sent the invite, whom its mails name as the inviter; nil when there is no such invite.
func (r *Repository) creator(ctx context.Context, inviteID uuid.UUID) (*person, error) {
	var rows []person
	if err := r.db.WithContext(ctx).Raw(`SELECT u.id, u.name, u.email, u.locale FROM invites i JOIN users u ON u.id = i.creator_id WHERE i.id = ?`, inviteID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load invite creator: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *Repository) inviteExists(ctx context.Context, organizationID uuid.UUID, email string) (bool, error) {
	var exists bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM invites WHERE organization_id = ? AND lower(email) = lower(?))`, organizationID, email).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("look up invite: %w", err)
	}
	return exists, nil
}

// userByEmail returns the id of the user with the address, ignoring case, and whether they belong to the organization.
func (r *Repository) userByEmail(ctx context.Context, organizationID uuid.UUID, email string) (id *uuid.UUID, member bool, err error) {
	var rows []struct {
		ID     uuid.UUID
		Member bool
	}
	err = r.db.WithContext(ctx).Raw(
		`SELECT u.id, EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = u.id AND m.organization_id = ?) AS member FROM users u WHERE lower(u.email) = lower(?)`,
		organizationID, email,
	).Scan(&rows).Error
	if err != nil {
		return nil, false, fmt.Errorf("look up invitee: %w", err)
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	return &rows[0].ID, rows[0].Member, nil
}

// countTeams counts how many of the teams belong to the organization.
func (r *Repository) countTeams(ctx context.Context, organizationID uuid.UUID, teamIDs []uuid.UUID) (int, error) {
	var n int
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM teams WHERE organization_id = ? AND id IN ?`, organizationID, teamIDs).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count teams: %w", err)
	}
	return n, nil
}

type newInvite struct {
	OrganizationID uuid.UUID
	Email          string
	Name           string
	Role           string
	CreatorID      uuid.UUID
	AcceptorID     *uuid.UUID
	ExpiresAt      time.Time
	TeamIDs        []uuid.UUID
}

// insertInvite stores the invite and its teams, returning errInviteExists when the organization already holds one for the address.
func (r *Repository) insertInvite(ctx context.Context, in newInvite) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.WithContext(ctx).Raw(
		`INSERT INTO invites (organization_id, email, name, creator_id, acceptor_id, expires_at, role) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		in.OrganizationID, in.Email, in.Name, in.CreatorID, nullUUID(in.AcceptorID), in.ExpiresAt, in.Role,
	).Row().Scan(&id)
	if isUniqueViolation(err) {
		return uuid.Nil, errInviteExists
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert invite: %w", err)
	}
	for _, teamID := range in.TeamIDs {
		if err := r.db.WithContext(ctx).Exec(`INSERT INTO invite_teams (invite_id, team_id) VALUES (?, ?)`, id, teamID).Error; err != nil {
			return uuid.Nil, fmt.Errorf("insert invite team: %w", err)
		}
	}
	return id, nil
}

// insertToken stores a link's hash. The hash is not the first value: GORM spreads a slice bound right after an opening parenthesis into a list, and a []byte is a slice.
func (r *Repository) insertToken(ctx context.Context, inviteID uuid.UUID, hash []byte, expiresAt time.Time) error {
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO invite_tokens (invite_id, token_hash, expires_at) VALUES (?, ?, ?)`, inviteID, hash, expiresAt).Error; err != nil {
		return fmt.Errorf("insert invite link: %w", err)
	}
	return nil
}

// deleteTokens revokes the invite's links: all of them, or with expiredOnly only those past their expiry.
func (r *Repository) deleteTokens(ctx context.Context, inviteID uuid.UUID, expiredOnly bool, now time.Time) error {
	query := `DELETE FROM invite_tokens WHERE invite_id = ?`
	args := []any{inviteID}
	if expiredOnly {
		query += ` AND expires_at <= ?`
		args = append(args, now)
	}
	if err := r.db.WithContext(ctx).Exec(query, args...).Error; err != nil {
		return fmt.Errorf("revoke invite links: %w", err)
	}
	return nil
}

// deleteTokensExcept revokes every link to the invite but the one whose hash is given.
func (r *Repository) deleteTokensExcept(ctx context.Context, inviteID uuid.UUID, keep []byte) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM invite_tokens WHERE invite_id = ? AND token_hash <> ?`, inviteID, keep).Error; err != nil {
		return fmt.Errorf("revoke earlier invite links: %w", err)
	}
	return nil
}

// setExpiry moves the invite's expiry; false when there is no such invite.
func (r *Repository) setExpiry(ctx context.Context, inviteID uuid.UUID, expiresAt time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`UPDATE invites SET expires_at = ? WHERE id = ?`, expiresAt, inviteID)
	if res.Error != nil {
		return false, fmt.Errorf("refresh invite expiry: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// invite returns one invite, or nil.
func (r *Repository) invite(ctx context.Context, inviteID uuid.UUID) (*Invite, error) {
	var rows []Invite
	if err := r.db.WithContext(ctx).Raw(`SELECT id, email, name, role, expires_at, created_at FROM invites WHERE id = ?`, inviteID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load invite: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// list returns the organization's invites, expired ones included, oldest first; page (from 1) takes pageSize of them, 0 takes all.
func (r *Repository) list(ctx context.Context, organizationID uuid.UUID, page int) ([]Invite, error) {
	query := `SELECT id, email, name, role, expires_at, created_at FROM invites WHERE organization_id = ? ORDER BY created_at, id`
	args := []any{organizationID}
	if page > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, pageSize, pageSize*(page-1))
	}
	invites := []Invite{}
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&invites).Error; err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	return invites, nil
}

// snapshot is what the audit event of a revoked invite records: forma2 kept the role because the address and the creator's name are masked as personal data.
func (r *Repository) snapshot(ctx context.Context, inviteID uuid.UUID) (map[string]any, error) {
	var rows []struct {
		Email       string
		Role        string
		CreatorName string
	}
	err := r.db.WithContext(ctx).Raw(`SELECT i.email, i.role, u.name AS creator_name FROM invites i JOIN users u ON u.id = i.creator_id WHERE i.id = ?`, inviteID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load invite: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return map[string]any{"email": rows[0].Email, "role": rows[0].Role, "creator": map[string]any{"name": rows[0].CreatorName}}, nil
}

func (r *Repository) deleteInvite(ctx context.Context, inviteID uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM invites WHERE id = ?`, inviteID)
	if res.Error != nil {
		return false, fmt.Errorf("delete invite: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// setRole returns the previous role, or "" when there is no such invite.
func (r *Repository) setRole(ctx context.Context, inviteID uuid.UUID, role string) (string, error) {
	var previous []string
	err := r.db.WithContext(ctx).Raw(
		`UPDATE invites i SET role = ? FROM (SELECT id, role FROM invites WHERE id = ? FOR UPDATE) old WHERE i.id = old.id RETURNING old.role`,
		role, inviteID,
	).Scan(&previous).Error
	if err != nil {
		return "", fmt.Errorf("update invite role: %w", err)
	}
	if len(previous) == 0 {
		return "", nil
	}
	return previous[0], nil
}

// byToken finds the invite a link opens, or nil when no stored link has this hash. With lock set the invite row is locked, so one invite is accepted once.
func (r *Repository) byToken(ctx context.Context, hash []byte, lock bool) (*linked, error) {
	query := `SELECT i.id, i.organization_id, i.email, i.role, i.creator_id, i.expires_at, t.expires_at AS token_expires_at
	          FROM invite_tokens t JOIN invites i ON i.id = t.invite_id WHERE t.token_hash = ?`
	if lock {
		query += ` FOR UPDATE OF i`
	}
	var rows []linked
	if err := r.db.WithContext(ctx).Raw(query, hash).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load invite link: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// inviteTeams lists the invite's teams that still exist in its organization.
func (r *Repository) inviteTeams(ctx context.Context, inviteID, organizationID uuid.UUID) ([]uuid.UUID, error) {
	ids := []uuid.UUID{}
	err := r.db.WithContext(ctx).Raw(
		`SELECT it.team_id FROM invite_teams it JOIN teams t ON t.id = it.team_id WHERE it.invite_id = ? AND t.organization_id = ? ORDER BY it.team_id`,
		inviteID, organizationID,
	).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("list invite teams: %w", err)
	}
	return ids, nil
}

// joinOrganization makes the user an accepted member with the role, or accepts their existing membership without touching its role. It returns the role they hold afterwards.
func (r *Repository) joinOrganization(ctx context.Context, organizationID, userID uuid.UUID, role string, now time.Time) (string, error) {
	db := r.db.WithContext(ctx)
	if err := db.Exec(
		`INSERT INTO memberships (organization_id, user_id, role, accepted, created_at, updated_at) VALUES (?, ?, ?, true, ?, ?)
		 ON CONFLICT (user_id, organization_id) DO UPDATE SET accepted = true, updated_at = EXCLUDED.updated_at WHERE NOT memberships.accepted`,
		organizationID, userID, role, now, now,
	).Error; err != nil {
		return "", fmt.Errorf("create membership: %w", err)
	}
	var held string
	if err := db.Raw(`SELECT role FROM memberships WHERE organization_id = ? AND user_id = ?`, organizationID, userID).Row().Scan(&held); err != nil {
		return "", fmt.Errorf("load membership: %w", err)
	}
	return held, nil
}

// joinTeam adds the user to the team unless they are already in it.
func (r *Repository) joinTeam(ctx context.Context, teamID, userID uuid.UUID, role string, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO team_members (team_id, user_id, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT (team_id, user_id) DO NOTHING`,
		teamID, userID, role, now, now,
	).Error; err != nil {
		return fmt.Errorf("add team member: %w", err)
	}
	return nil
}

// unsubscribe switches the organization's alerts off for the user, as joining an organization did in forma2: the organization joins unsubscribedOrganizationIds once and every other setting is kept.
func (r *Repository) unsubscribe(ctx context.Context, userID, organizationID uuid.UUID, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE users SET updated_at = ?, notification_settings = notification_settings || jsonb_build_object(
		     'alert', COALESCE(notification_settings -> 'alert', '{}'::jsonb),
		     'unsubscribedOrganizationIds', CASE
		         WHEN COALESCE(notification_settings -> 'unsubscribedOrganizationIds', '[]'::jsonb) @> jsonb_build_array(?::text)
		         THEN notification_settings -> 'unsubscribedOrganizationIds'
		         ELSE COALESCE(notification_settings -> 'unsubscribedOrganizationIds', '[]'::jsonb) || jsonb_build_array(?::text)
		     END)
		 WHERE id = ?`,
		now, organizationID.String(), organizationID.String(), userID,
	).Error; err != nil {
		return fmt.Errorf("update notification settings: %w", err)
	}
	return nil
}

// nullUUID is a nullable uuid argument.
func nullUUID(id *uuid.UUID) sql.NullString {
	if id == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: id.String(), Valid: true}
}
