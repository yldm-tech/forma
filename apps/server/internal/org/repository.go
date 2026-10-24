package org

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/survey/scheduling"
)

// Repository reads and writes organizations, their billing rows and memberships. Every query names the organization it is scoped to, or the user whose own memberships it reads.
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

// serializable runs fn in a Serializable transaction with database.Serializable's retries. The owner guards read the owner count in the same transaction as the write they protect, so two owners removing or demoting each other concurrently cannot both pass: PostgreSQL aborts one of them.
func (r *Repository) serializable(ctx context.Context, fn func(tx *Repository) error) error {
	return database.Serializable(ctx, r.db, func(tx *gorm.DB) error {
		return fn(&Repository{db: tx})
	})
}

const organizationColumns = `o.id, o.created_at, o.updated_at, o.name, o.is_ai_smart_tools_enabled, o.whitelabel, o.display_time_zone, o.survey_scheduling_time_zone,
	b.organization_id IS NOT NULL AS has_billing, b.stripe_customer_id, b.limits, b.usage_cycle_anchor, b.stripe`

type organizationRow struct {
	ID                       uuid.UUID
	CreatedAt                time.Time
	UpdatedAt                time.Time
	Name                     string
	IsAiSmartToolsEnabled    bool
	Whitelabel               []byte
	DisplayTimeZone          sql.NullString
	SurveySchedulingTimeZone sql.NullString
	HasBilling               bool
	StripeCustomerID         sql.NullString
	Limits                   []byte
	UsageCycleAnchor         sql.NullTime
	Stripe                   []byte
	Role                     sql.NullString
}

// organization maps a row; a missing billing row reads as the defaults, as forma2's mapOrganizationBilling did.
func (o organizationRow) organization(isFormaCloud bool) *Organization {
	out := &Organization{
		ID: o.ID, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, Name: o.Name, IsAISmartToolsEnabled: o.IsAiSmartToolsEnabled, Whitelabel: o.Whitelabel,
		Billing: Billing{Limits: defaultLimits(isFormaCloud)},
	}
	if o.DisplayTimeZone.Valid {
		out.DisplayTimeZone = &o.DisplayTimeZone.String
	}
	if o.SurveySchedulingTimeZone.Valid {
		out.SurveySchedulingTimeZone = &o.SurveySchedulingTimeZone.String
	}
	if o.HasBilling {
		out.Billing = Billing{Limits: o.Limits, Stripe: o.Stripe}
		if o.StripeCustomerID.Valid {
			out.Billing.StripeCustomerID = &o.StripeCustomerID.String
		}
		if o.UsageCycleAnchor.Valid {
			out.Billing.UsageCycleAnchor = &o.UsageCycleAnchor.Time
		}
	}
	return out
}

// byID returns nil when the organization does not exist.
func (r *Repository) byID(ctx context.Context, id uuid.UUID, isFormaCloud bool) (*Organization, error) {
	var rows []organizationRow
	if err := r.db.WithContext(ctx).Raw(
		`SELECT `+organizationColumns+` FROM organizations o LEFT JOIN organization_billing b ON b.organization_id = o.id WHERE o.id = ?`, id,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load organization: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].organization(isFormaCloud), nil
}

// lock takes the organization row for update inside a transaction and reports whether it exists.
func (r *Repository) lock(ctx context.Context, id uuid.UUID) (bool, error) {
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM organizations WHERE id = ? FOR NO KEY UPDATE`, id).Scan(&ids).Error; err != nil {
		return false, fmt.Errorf("lock organization: %w", err)
	}
	return len(ids) == 1, nil
}

// Summary is an organization in the caller's list, with the caller's role in it.
type Summary struct {
	*Organization
	Role string `json:"role"`
}

// listed loads the organizations among ids with the user's role in each, oldest first with the id as tie-break (callers treat the first as "the" organization and paginate), 30 per page when a page is given.
func (r *Repository) listed(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, page int, isFormaCloud bool) ([]Summary, error) {
	if len(ids) == 0 {
		return []Summary{}, nil
	}
	query := `SELECT ` + organizationColumns + `, m.role FROM organizations o
		LEFT JOIN organization_billing b ON b.organization_id = o.id
		LEFT JOIN memberships m ON m.organization_id = o.id AND m.user_id = ?
		WHERE o.id IN ? ORDER BY o.created_at, o.id`
	if page > 0 {
		query += fmt.Sprintf(` LIMIT %d OFFSET %d`, ItemsPerPage, ItemsPerPage*(page-1))
	}
	var rows []organizationRow
	if err := r.db.WithContext(ctx).Raw(query, userID, ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	out := make([]Summary, len(rows))
	for i, row := range rows {
		out[i] = Summary{Organization: row.organization(isFormaCloud), Role: row.Role.String}
	}
	return out, nil
}

// insertOrganization creates the organization with its default billing row, as forma2's createOrganization did in one statement.
func (r *Repository) insertOrganization(ctx context.Context, name string, isFormaCloud bool) (uuid.UUID, error) {
	var id uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`INSERT INTO organizations (name) VALUES (?) RETURNING id`, name).Row().Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("create organization: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO organization_billing (organization_id, limits) VALUES (?, ?::jsonb)`, id, string(defaultLimits(isFormaCloud)),
	).Error; err != nil {
		return uuid.Nil, fmt.Errorf("create organization billing: %w", err)
	}
	return id, nil
}

func (r *Repository) insertMembership(ctx context.Context, organizationID, userID uuid.UUID, role string, accepted bool) error {
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, ?)`, userID, organizationID, role, accepted,
	).Error; err != nil {
		return fmt.Errorf("create membership: %w", err)
	}
	return nil
}

// unsubscribeAlerts adds the organization to the user's unsubscribedOrganizationIds, once: every organization-creation path opted its creator out of per-response alerts for the organization they had just made.
func (r *Repository) unsubscribeAlerts(ctx context.Context, userID, organizationID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE users SET notification_settings = jsonb_build_object(
		     'alert', COALESCE(notification_settings -> 'alert', '{}'::jsonb),
		     'unsubscribedOrganizationIds',
		     CASE WHEN COALESCE(notification_settings -> 'unsubscribedOrganizationIds', '[]'::jsonb) @> jsonb_build_array(?::text)
		          THEN notification_settings -> 'unsubscribedOrganizationIds'
		          ELSE COALESCE(notification_settings -> 'unsubscribedOrganizationIds', '[]'::jsonb) || jsonb_build_array(?::text) END
		 ) || (notification_settings - 'alert' - 'unsubscribedOrganizationIds'),
		 updated_at = now()
		 WHERE id = ?`,
		organizationID.String(), organizationID.String(), userID,
	).Error; err != nil {
		return fmt.Errorf("update notification settings: %w", err)
	}
	return nil
}

// column is one assignment of an organization update; names are constants chosen by the service.
type column struct {
	name  string
	value any
}

func (r *Repository) update(ctx context.Context, id uuid.UUID, columns []column) error {
	if len(columns) == 0 {
		return nil
	}
	sets := make([]string, 0, len(columns)+1)
	args := make([]any, 0, len(columns)+1)
	for _, c := range columns {
		sets = append(sets, c.name+" = ?")
		args = append(args, c.value)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	if err := r.db.WithContext(ctx).Exec(`UPDATE organizations SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...).Error; err != nil {
		return fmt.Errorf("update organization: %w", err)
	}
	return nil
}

// delete removes the organization; workspaces, memberships, teams, invites, API keys and billing cascade, and each table's trigger enqueues the SpiceDB cleanup.
func (r *Repository) delete(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM organizations WHERE id = ?`, id).Error; err != nil {
		return fmt.Errorf("delete organization: %w", err)
	}
	return nil
}

// hasAnyUser reports whether the instance has a user; without one it is a fresh instance.
func (r *Repository) hasAnyUser(ctx context.Context) (bool, error) {
	var exists bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("check for users: %w", err)
	}
	return exists, nil
}

// membership returns nil when the user is not a member of the organization.
func (r *Repository) membership(ctx context.Context, userID, organizationID uuid.UUID) (*Membership, error) {
	var rows []Membership
	if err := r.db.WithContext(ctx).Raw(
		`SELECT organization_id, user_id, accepted, role FROM memberships WHERE user_id = ? AND organization_id = ?`, userID, organizationID,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load membership: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// members lists the organization's members with their users, in the order they joined, 30 per page when a page is given.
func (r *Repository) members(ctx context.Context, organizationID uuid.UUID, page int) ([]Member, error) {
	query := `SELECT m.user_id, u.name, u.email, m.accepted, m.role, u.is_active FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = ? ORDER BY m.created_at, m.user_id`
	if page > 0 {
		query += fmt.Sprintf(` LIMIT %d OFFSET %d`, ItemsPerPage, ItemsPerPage*(page-1))
	}
	rows := []Member{}
	if err := r.db.WithContext(ctx).Raw(query, organizationID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return rows, nil
}

// activeOwnerCount counts the organization's owners who can still sign in. A deactivated owner can never sign in again, so counting them would let a guard pass while leaving no owner who can act.
func (r *Repository) activeOwnerCount(ctx context.Context, organizationID uuid.UUID) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.organization_id = ? AND m.role = 'owner' AND u.is_active`, organizationID,
	).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count owners: %w", err)
	}
	return n, nil
}

// hasMembershipOutside reports whether the user belongs to an organization other than the given ones.
func (r *Repository) hasMembershipOutside(ctx context.Context, userID uuid.UUID, organizationIDs []uuid.UUID) (bool, error) {
	query := `SELECT EXISTS (SELECT 1 FROM memberships WHERE user_id = ?`
	args := []any{userID}
	// NOT IN with an empty list would compare against NULL and match nothing.
	if len(organizationIDs) > 0 {
		query += ` AND organization_id NOT IN ?`
		args = append(args, organizationIDs)
	}
	var exists bool
	if err := r.db.WithContext(ctx).Raw(query+`)`, args...).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("check memberships: %w", err)
	}
	return exists, nil
}

// membershipCount is the number of organizations the user belongs to.
func (r *Repository) membershipCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM memberships WHERE user_id = ?`, userID).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count memberships: %w", err)
	}
	return n, nil
}

// updateRole sets a member's role and reports whether the membership exists. Promotion to owner or manager makes the user admin of every team of the organization they belong to, as forma2's updateMembership did.
func (r *Repository) updateRole(ctx context.Context, userID, organizationID uuid.UUID, role string) (bool, error) {
	db := r.db.WithContext(ctx)
	res := db.Exec(`UPDATE memberships SET role = ?, updated_at = now() WHERE user_id = ? AND organization_id = ?`, role, userID, organizationID)
	if res.Error != nil {
		return false, fmt.Errorf("update membership: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	if role == RoleOwner || role == RoleManager {
		if err := db.Exec(
			`UPDATE team_members SET role = 'admin', updated_at = now() WHERE user_id = ? AND role <> 'admin' AND team_id IN (SELECT id FROM teams WHERE organization_id = ?)`,
			userID, organizationID,
		).Error; err != nil {
			return false, fmt.Errorf("promote team memberships: %w", err)
		}
	}
	return true, nil
}

// deleteMembership removes the user from the organization and from every team of it.
func (r *Repository) deleteMembership(ctx context.Context, userID, organizationID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	if err := db.Exec(`DELETE FROM team_members WHERE user_id = ? AND team_id IN (SELECT id FROM teams WHERE organization_id = ?)`, userID, organizationID).Error; err != nil {
		return fmt.Errorf("delete team memberships: %w", err)
	}
	if err := db.Exec(`DELETE FROM memberships WHERE user_id = ? AND organization_id = ?`, userID, organizationID).Error; err != nil {
		return fmt.Errorf("delete membership: %w", err)
	}
	return nil
}

// surveySchedulingTimeZone is the organization's own survey scheduling zone, nil when it has none.
func (r *Repository) surveySchedulingTimeZone(ctx context.Context, organizationID uuid.UUID) (*string, error) {
	var rows []struct{ SurveySchedulingTimeZone *string }
	if err := r.db.WithContext(ctx).Raw(`SELECT survey_scheduling_time_zone FROM organizations WHERE id = ?`, organizationID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load organization scheduling time zone: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].SurveySchedulingTimeZone, nil
}

// resnapSchedules moves the publish and close dates of the organization's surveys, archived ones included, from one schedule to another, keeping each date's calendar day, and reports how many surveys changed. The rows are locked first, so a concurrent save of one of them either lands before and is moved, or waits and snaps in the new zone.
func (r *Repository) resnapSchedules(ctx context.Context, organizationID uuid.UUID, from, to scheduling.Schedule) (int, error) {
	var rows []struct {
		ID        uuid.UUID
		PublishOn *time.Time
		CloseOn   *time.Time
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT s.id, s.publish_on, s.close_on FROM surveys s JOIN workspaces w ON w.id = s.workspace_id
		WHERE w.organization_id = ? AND (s.publish_on IS NOT NULL OR s.close_on IS NOT NULL) ORDER BY s.id FOR NO KEY UPDATE OF s`, organizationID).Scan(&rows).Error; err != nil {
		return 0, fmt.Errorf("load scheduled surveys: %w", err)
	}
	for _, row := range rows {
		if err := r.db.WithContext(ctx).Exec(`UPDATE surveys SET publish_on = ?, close_on = ?, updated_at = now() WHERE id = ?`,
			to.Resnap(from, row.PublishOn), to.Resnap(from, row.CloseOn), row.ID).Error; err != nil {
			return 0, fmt.Errorf("move survey schedule: %w", err)
		}
	}
	return len(rows), nil
}
