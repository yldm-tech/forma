package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository reads and writes organization_billing and the few organization facts billing needs. Every query names the organization it is scoped to, or the Stripe customer that identifies one.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

type recordRow struct {
	OrganizationID   uuid.UUID
	StripeCustomerID sql.NullString
	Limits           []byte
	UsageCycleAnchor sql.NullTime
	Stripe           []byte
}

func (r recordRow) record() (*Record, error) {
	rec := &Record{OrganizationID: r.OrganizationID}
	if r.StripeCustomerID.Valid {
		rec.StripeCustomerID = &r.StripeCustomerID.String
	}
	if r.UsageCycleAnchor.Valid {
		t := r.UsageCycleAnchor.Time.UTC()
		rec.UsageCycleAnchor = &t
	}
	if err := json.Unmarshal(r.Limits, &rec.Limits); err != nil {
		return nil, fmt.Errorf("decode billing limits: %w", err)
	}
	if len(r.Stripe) > 0 && string(r.Stripe) != "null" {
		rec.Stripe = &Snapshot{}
		if err := json.Unmarshal(r.Stripe, rec.Stripe); err != nil {
			return nil, fmt.Errorf("decode billing snapshot: %w", err)
		}
	}
	return rec, nil
}

const recordColumns = `organization_id, stripe_customer_id, limits, usage_cycle_anchor, stripe`

func (r *Repository) scanRecord(ctx context.Context, db *gorm.DB, query string, args ...any) (*Record, error) {
	var rows []recordRow
	if err := db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load organization billing: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].record()
}

// load is the organization's billing row as it is; nil when it has none.
func (r *Repository) load(ctx context.Context, organizationID uuid.UUID) (*Record, error) {
	return r.scanRecord(ctx, r.db, `SELECT `+recordColumns+` FROM organization_billing WHERE organization_id = ?`, organizationID)
}

// ensure is forma2's ensureOrganizationBillingRecord: the organization's billing row, created with the default limits when the organization has none (organizations created at sign-up get theirs here). Nil when the organization does not exist.
func (r *Repository) ensure(ctx context.Context, organizationID uuid.UUID, defaults Limits) (*Record, error) {
	rec, err := r.load(ctx, organizationID)
	if err != nil || rec != nil {
		return rec, err
	}
	limits, err := json.Marshal(defaults)
	if err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO organization_billing (organization_id, limits) SELECT id, ?::jsonb FROM organizations WHERE id = ? ON CONFLICT (organization_id) DO NOTHING`,
		string(limits), organizationID,
	).Error; err != nil {
		return nil, fmt.Errorf("create organization billing: %w", err)
	}
	return r.load(ctx, organizationID)
}

// organizationByCustomer is forma2's findOrganizationIdByStripeCustomerId; uuid.Nil when no organization has the customer.
func (r *Repository) organizationByCustomer(ctx context.Context, customerID string) (uuid.UUID, error) {
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT organization_id FROM organization_billing WHERE stripe_customer_id = ?`, customerID).Scan(&ids).Error; err != nil {
		return uuid.Nil, fmt.Errorf("find organization by Stripe customer: %w", err)
	}
	if len(ids) == 0 {
		return uuid.Nil, nil
	}
	return ids[0], nil
}

// organizationName returns "" and false when the organization does not exist.
func (r *Repository) organizationName(ctx context.Context, organizationID uuid.UUID) (string, bool, error) {
	var names []string
	if err := r.db.WithContext(ctx).Raw(`SELECT name FROM organizations WHERE id = ?`, organizationID).Scan(&names).Error; err != nil {
		return "", false, fmt.Errorf("load organization: %w", err)
	}
	if len(names) == 0 {
		return "", false, nil
	}
	return names[0], true, nil
}

// owner is the organization's first owner, forma2's getOrganizationOwner.
type owner struct {
	ID    uuid.UUID
	Name  string
	Email string
}

func (r *Repository) owner(ctx context.Context, organizationID uuid.UUID) (*owner, error) {
	var rows []owner
	if err := r.db.WithContext(ctx).Raw(
		`SELECT u.id, u.name, u.email FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.organization_id = ? AND m.role = 'owner' ORDER BY m.created_at, u.id LIMIT 1`, organizationID,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load organization owner: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// setCustomer stores the organization's Stripe customer, creating the row when it is missing. A customer already stored is kept: billing must never repoint an organization at a second customer while its subscription stays on the first. Returns the customer the row holds.
func (r *Repository) setCustomer(ctx context.Context, organizationID uuid.UUID, customerID string, defaults Limits, snapshot Snapshot) (string, error) {
	limits, err := json.Marshal(defaults)
	if err != nil {
		return "", err
	}
	stripeJSON, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	var stored string
	if err := r.db.WithContext(ctx).Raw(
		`INSERT INTO organization_billing (organization_id, stripe_customer_id, limits, stripe) VALUES (?, ?, ?::jsonb, ?::jsonb)
		 ON CONFLICT (organization_id) DO UPDATE SET stripe_customer_id = COALESCE(organization_billing.stripe_customer_id, EXCLUDED.stripe_customer_id), updated_at = now()
		 RETURNING stripe_customer_id`,
		organizationID, customerID, string(limits), string(stripeJSON),
	).Row().Scan(&stored); err != nil {
		return "", fmt.Errorf("store Stripe customer: %w", err)
	}
	return stored, nil
}

// writeSync stores what a sync read from Stripe.
func (r *Repository) writeSync(ctx context.Context, rec *Record) error {
	limits, err := json.Marshal(rec.Limits)
	if err != nil {
		return err
	}
	stripeJSON, err := json.Marshal(rec.Stripe)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE organization_billing SET stripe_customer_id = ?, limits = ?::jsonb, usage_cycle_anchor = ?, stripe = ?::jsonb, updated_at = now() WHERE organization_id = ?`,
		rec.StripeCustomerID, string(limits), rec.UsageCycleAnchor, string(stripeJSON), rec.OrganizationID,
	).Error; err != nil {
		return fmt.Errorf("store billing snapshot: %w", err)
	}
	return nil
}

// updateSnapshot changes the snapshot under a row lock, so two concurrent edits of different fields both land. fn sees the row's snapshot, nil when it has none, and returns false to leave the row as it is. Nothing happens when the organization has no billing row.
func (r *Repository) updateSnapshot(ctx context.Context, organizationID uuid.UUID, fn func(rec *Record) bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rec, err := r.scanRecord(ctx, tx, `SELECT `+recordColumns+` FROM organization_billing WHERE organization_id = ? FOR UPDATE`, organizationID)
		if err != nil || rec == nil {
			return err
		}
		if !fn(rec) {
			return nil
		}
		stripeJSON, err := json.Marshal(rec.Stripe)
		if err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE organization_billing SET stripe = ?::jsonb, updated_at = now() WHERE organization_id = ?`, string(stripeJSON), organizationID).Error; err != nil {
			return fmt.Errorf("update billing snapshot: %w", err)
		}
		return nil
	})
}

// Usage is the organization's consumption in the current usage cycle and its workspace count, what the billing page shows next to the limits.
type Usage struct {
	Responses    int64 `json:"responses"`
	WorkflowRuns int64 `json:"workflowRuns"`
	Workspaces   int64 `json:"workspaces"`
}

// usage is forma2's getMonthlyOrganizationResponseCount, getMonthlyOrganizationWorkflowRunCount (dry runs are never billed, so never counted) and getOrganizationWorkspacesCount.
func (r *Repository) usage(ctx context.Context, organizationID uuid.UUID, start, end time.Time) (Usage, error) {
	var u Usage
	if err := r.db.WithContext(ctx).Raw(`SELECT
		(SELECT count(*) FROM responses r JOIN surveys s ON s.id = r.survey_id JOIN workspaces w ON w.id = s.workspace_id
		  WHERE w.organization_id = ? AND r.created_at >= ? AND r.created_at < ?) AS responses,
		(SELECT count(*) FROM workflow_runs wr JOIN workspaces w ON w.id = wr.workspace_id
		  WHERE w.organization_id = ? AND NOT wr.is_dry_run AND wr.created_at >= ? AND wr.created_at < ?) AS workflow_runs,
		(SELECT count(*) FROM workspaces WHERE organization_id = ?) AS workspaces`,
		organizationID, start, end, organizationID, start, end, organizationID,
	).Row().Scan(&u.Responses, &u.WorkflowRuns, &u.Workspaces); err != nil {
		return Usage{}, fmt.Errorf("count organization usage: %w", err)
	}
	return u, nil
}

// role is the user's role in the organization, "" when they are not a member.
func (r *Repository) role(ctx context.Context, userID, organizationID uuid.UUID) (string, error) {
	var roles []string
	if err := r.db.WithContext(ctx).Raw(`SELECT role FROM memberships WHERE user_id = ? AND organization_id = ?`, userID, organizationID).Scan(&roles).Error; err != nil {
		return "", fmt.Errorf("load membership: %w", err)
	}
	if len(roles) == 0 {
		return "", nil
	}
	return roles[0], nil
}
