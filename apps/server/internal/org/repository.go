package org

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
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

const organizationColumns = `o.id, o.created_at, o.updated_at, o.name, o.is_ai_smart_tools_enabled, o.is_ai_response_reading_enabled, o.whitelabel, o.display_time_zone, o.survey_scheduling_time_zone, o.two_factor_enforced_at,
	b.organization_id IS NOT NULL AS has_billing, b.stripe_customer_id, b.limits, b.usage_cycle_anchor, b.stripe`

type organizationRow struct {
	ID                         uuid.UUID
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
	Name                       string
	IsAiSmartToolsEnabled      bool
	IsAiResponseReadingEnabled bool
	Whitelabel                 []byte
	DisplayTimeZone            sql.NullString
	SurveySchedulingTimeZone   sql.NullString
	TwoFactorEnforcedAt        sql.NullTime
	HasBilling                 bool
	StripeCustomerID           sql.NullString
	Limits                     []byte
	UsageCycleAnchor           sql.NullTime
	Stripe                     []byte
	Role                       sql.NullString
	SuspendedAt                sql.NullTime
}

// organization maps a row; a missing billing row reads as the defaults, as forma2's mapOrganizationBilling did.
func (o organizationRow) organization(isFormaCloud bool) *Organization {
	out := &Organization{
		ID: o.ID, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, Name: o.Name, IsAISmartToolsEnabled: o.IsAiSmartToolsEnabled, IsAIResponseReadingEnabled: o.IsAiResponseReadingEnabled, Whitelabel: o.Whitelabel,
		Billing: Billing{Limits: defaultLimits(isFormaCloud)},
	}
	if o.DisplayTimeZone.Valid {
		out.DisplayTimeZone = &o.DisplayTimeZone.String
	}
	if o.SurveySchedulingTimeZone.Valid {
		out.SurveySchedulingTimeZone = &o.SurveySchedulingTimeZone.String
	}
	if o.TwoFactorEnforcedAt.Valid {
		out.TwoFactorEnforcedAt = &o.TwoFactorEnforcedAt.Time
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

// Summary is an organization in the caller's list, with the caller's role in it and, when the organization suspended the caller's access, since when: the organization switcher marks it.
type Summary struct {
	*Organization
	Role        string     `json:"role"`
	SuspendedAt *time.Time `json:"suspendedAt"`
}

// listed loads the organizations among ids with the user's role in each, oldest first with the id as tie-break (callers treat the first as "the" organization and paginate), 30 per page when a page is given.
func (r *Repository) listed(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, page int, isFormaCloud bool) ([]Summary, error) {
	if len(ids) == 0 {
		return []Summary{}, nil
	}
	query := `SELECT ` + organizationColumns + `, m.role, m.suspended_at FROM organizations o
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
		if row.SuspendedAt.Valid {
			out[i].SuspendedAt = &row.SuspendedAt.Time
		}
	}
	return out, nil
}

// insertOrganization creates the organization with its default billing row, as forma2's createOrganization did in one statement. timeZone, when set, is stored as both the display and the survey scheduling zone; nil leaves both NULL (UTC and the instance's zone).
func (r *Repository) insertOrganization(ctx context.Context, name string, timeZone *string, isFormaCloud bool) (uuid.UUID, error) {
	var id uuid.UUID
	if err := r.db.WithContext(ctx).Raw(
		`INSERT INTO organizations (name, display_time_zone, survey_scheduling_time_zone) VALUES (?, ?, ?) RETURNING id`, name, timeZone, timeZone,
	).Row().Scan(&id); err != nil {
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

// ownsActiveOrganization reports whether the user is an accepted owner of an organization that has not suspended them.
func (r *Repository) ownsActiveOrganization(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	if err := r.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM memberships WHERE user_id = ? AND role = 'owner' AND accepted AND suspended_at IS NULL)`, userID,
	).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("check for owned organizations: %w", err)
	}
	return exists, nil
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
		`SELECT organization_id, user_id, accepted, role, suspended_at FROM memberships WHERE user_id = ? AND organization_id = ?`, userID, organizationID,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load membership: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// members lists the organization's members with their users and two-factor status, in the order they joined, 30 per page when a page is given.
func (r *Repository) members(ctx context.Context, organizationID uuid.UUID, page int) ([]Member, error) {
	query := `SELECT m.user_id, u.name, u.email, m.accepted, m.role, u.is_active, m.suspended_at, ` + authz.SecondFactorStatusSQL("u.id") + ` AS two_factor
		FROM memberships m JOIN users u ON u.id = m.user_id
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

// secondFactor is the user's two-factor status, and whether the organization's requirement refuses them now: its grace period is over and they must enrol and have not. A user who does not exist reads as SSO and never refused; the callers pass the signed-in user.
func (r *Repository) secondFactor(ctx context.Context, userID, organizationID uuid.UUID) (status string, blocked bool, err error) {
	var rows []struct {
		Status   string
		Enforced bool
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT `+authz.SecondFactorStatusSQL("u.id")+` AS status,
		EXISTS (SELECT 1 FROM organizations o WHERE o.id = ? AND o.two_factor_enforced_at <= now()) AS enforced
		FROM users u WHERE u.id = ?`, organizationID, userID).Scan(&rows).Error; err != nil {
		return "", false, fmt.Errorf("load two-factor status: %w", err)
	}
	if len(rows) == 0 {
		return authz.SecondFactorSSO, false, nil
	}
	return rows[0].Status, rows[0].Enforced && rows[0].Status == authz.SecondFactorDisabled, nil
}

// otherActiveOwners counts the organization's owners other than the user who can still act in it: their account is active and their access is not suspended. A deactivated or suspended owner cannot administer the organization, so counting them would let a guard pass while leaving no owner who can act; leaving the user out lets the guards act on an owner who is already suspended while another owner remains.
func (r *Repository) otherActiveOwners(ctx context.Context, organizationID, userID uuid.UUID) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.organization_id = ? AND m.role = 'owner' AND u.is_active AND m.suspended_at IS NULL AND m.user_id <> ?`, organizationID, userID,
	).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count owners: %w", err)
	}
	return n, nil
}

// setSuspension suspends the member's access to the organization, keeping the time of a suspension already in place, or restores it, and reports whether the membership exists.
func (r *Repository) setSuspension(ctx context.Context, userID, organizationID uuid.UUID, suspend bool) (bool, error) {
	res := r.db.WithContext(ctx).Exec(
		`UPDATE memberships SET suspended_at = CASE WHEN ? THEN COALESCE(suspended_at, now()) END, updated_at = now() WHERE user_id = ? AND organization_id = ?`, suspend, userID, organizationID,
	)
	if res.Error != nil {
		return false, fmt.Errorf("update membership suspension: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
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

// updateRole sets a member's role and reports whether the membership exists. Promotion to owner or manager makes the user admin of every team of the organization they belong to, as forma2's updateMembership did; demotion from owner or manager to member or billing makes them a contributor of those teams again, so a former manager keeps no team admin seat they only held through their role. A plain member's role sent unchanged leaves the team admin seats they were given alone.
func (r *Repository) updateRole(ctx context.Context, userID, organizationID uuid.UUID, role string) (bool, error) {
	db := r.db.WithContext(ctx)
	var previous []string
	if err := db.Raw(`SELECT role FROM memberships WHERE user_id = ? AND organization_id = ?`, userID, organizationID).Scan(&previous).Error; err != nil {
		return false, fmt.Errorf("load membership role: %w", err)
	}
	res := db.Exec(`UPDATE memberships SET role = ?, updated_at = now() WHERE user_id = ? AND organization_id = ?`, role, userID, organizationID)
	if res.Error != nil {
		return false, fmt.Errorf("update membership: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	switch {
	case managesTeams(role):
		if err := db.Exec(
			`UPDATE team_members SET role = 'admin', updated_at = now() WHERE user_id = ? AND role <> 'admin' AND team_id IN (SELECT id FROM teams WHERE organization_id = ?)`,
			userID, organizationID,
		).Error; err != nil {
			return false, fmt.Errorf("promote team memberships: %w", err)
		}
	case len(previous) > 0 && managesTeams(previous[0]):
		if err := db.Exec(
			`UPDATE team_members SET role = 'contributor', updated_at = now() WHERE user_id = ? AND role = 'admin' AND team_id IN (SELECT id FROM teams WHERE organization_id = ?)`,
			userID, organizationID,
		).Error; err != nil {
			return false, fmt.Errorf("demote team memberships: %w", err)
		}
	}
	return true, nil
}

// managesTeams is whether the organization role administers every team of the organization.
func managesTeams(role string) bool { return role == RoleOwner || role == RoleManager }

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

// resnapSchedules moves the publish and close dates of the organization's surveys, archived ones included, from one schedule to another, keeping each date's calendar day and local time of day, and reports how many surveys changed. Surveys whose dates are the same instants in both zones (Europe/Berlin and Europe/Paris, or Asia/Calcutta and Asia/Kolkata) are left alone, so an open editor is not told the survey changed. The rows are locked first; a concurrent save that gives a survey its first dates holds the organization's row in share mode (survey.Repository.schedulingTimeZone), so it either committed before this lock and is moved here, or waits for this change and snaps in the new zone.
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
	type move struct {
		ID        uuid.UUID  `json:"id"`
		PublishOn *time.Time `json:"publish_on"`
		CloseOn   *time.Time `json:"close_on"`
	}
	var moves []move
	for _, row := range rows {
		publishOn, closeOn := to.Resnap(from, row.PublishOn), to.Resnap(from, row.CloseOn)
		if sameInstant(publishOn, row.PublishOn) && sameInstant(closeOn, row.CloseOn) {
			continue
		}
		moves = append(moves, move{ID: row.ID, PublishOn: publishOn, CloseOn: closeOn})
	}
	if len(moves) == 0 {
		return 0, nil
	}
	payload, err := json.Marshal(moves)
	if err != nil {
		return 0, fmt.Errorf("encode survey schedules: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`UPDATE surveys s SET publish_on = m.publish_on, close_on = m.close_on, updated_at = now()
		FROM jsonb_to_recordset(?::jsonb) AS m(id uuid, publish_on timestamptz, close_on timestamptz) WHERE s.id = m.id`, string(payload)).Error; err != nil {
		return 0, fmt.Errorf("move survey schedules: %w", err)
	}
	return len(moves), nil
}

func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
