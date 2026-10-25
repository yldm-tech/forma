package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository reads and writes webhooks and the surveys they are scoped to.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// selectWebhook reads webhooks with their triggers, their scope and listed surveys; a webhook scoped to every survey lists none.
const selectWebhook = `SELECT w.id, w.name, w.created_at, w.updated_at, w.url, w.source, w.workspace_id, w.secret, w.all_surveys,
	to_jsonb(w.triggers) AS triggers,
	CASE WHEN w.all_surveys THEN '[]'::jsonb ELSE COALESCE((SELECT jsonb_agg(ws.survey_id ORDER BY ws.survey_id) FROM webhook_surveys ws WHERE ws.webhook_id = w.id), '[]'::jsonb) END AS survey_ids
	FROM webhooks w`

type row struct {
	ID          uuid.UUID
	Name        *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	URL         string
	Source      string
	WorkspaceID uuid.UUID
	Secret      *string
	AllSurveys  bool
	Triggers    []byte
	SurveyIDs   []byte
}

func (r *Repository) find(ctx context.Context, query string, args ...any) ([]*Webhook, error) {
	var rows []row
	if err := r.db.WithContext(ctx).Raw(selectWebhook+" "+query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load webhooks: %w", err)
	}
	out := make([]*Webhook, len(rows))
	for i, x := range rows {
		w := &Webhook{ID: x.ID, Name: x.Name, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt, URL: x.URL, Source: x.Source, WorkspaceID: x.WorkspaceID, Secret: x.Secret, AllSurveys: x.AllSurveys}
		if err := json.Unmarshal(x.Triggers, &w.Triggers); err != nil {
			return nil, fmt.Errorf("decode webhook %s triggers: %w", x.ID, err)
		}
		if err := json.Unmarshal(x.SurveyIDs, &w.SurveyIDs); err != nil {
			return nil, fmt.Errorf("decode webhook %s surveys: %w", x.ID, err)
		}
		out[i] = w
	}
	return out, nil
}

// ByID is the webhook with id, or nil.
func (r *Repository) ByID(ctx context.Context, id uuid.UUID) (*Webhook, error) {
	found, err := r.find(ctx, `WHERE w.id = ?`, id)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// OfWorkspace is forma2's getWebhooks for the settings page: newest first.
func (r *Repository) OfWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*Webhook, error) {
	return r.find(ctx, `WHERE w.workspace_id = ? ORDER BY w.created_at DESC, w.id DESC`, workspaceID)
}

// OfWorkspaces is the v1 API's getWebhooks: every webhook of the workspaces, in no order forma2 promised; creation order here.
func (r *Repository) OfWorkspaces(ctx context.Context, workspaceIDs []uuid.UUID) ([]*Webhook, error) {
	if len(workspaceIDs) == 0 {
		return []*Webhook{}, nil
	}
	return r.find(ctx, `WHERE w.workspace_id = ANY(?::uuid[]) ORDER BY w.created_at, w.id`, uuidArray(workspaceIDs))
}

// Matching is forma2's getWebhooksForPipeline: the workspace's webhooks subscribed to event that cover surveyID, by listing it or by covering every survey.
func (r *Repository) Matching(ctx context.Context, workspaceID uuid.UUID, event string, surveyID uuid.UUID) ([]*Webhook, error) {
	return r.find(ctx, `WHERE w.workspace_id = ? AND ? = ANY(w.triggers)
		AND (w.all_surveys OR EXISTS (SELECT 1 FROM webhook_surveys ws WHERE ws.webhook_id = w.id AND ws.survey_id = ?))
		ORDER BY w.created_at, w.id`, workspaceID, event, surveyID)
}

// responseExists reports whether responseID is still a response to surveyID, a survey of the workspace: deleting the survey deletes its responses.
func (r *Repository) responseExists(ctx context.Context, workspaceID, surveyID, responseID uuid.UUID) (bool, error) {
	var exists bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM responses r JOIN surveys s ON s.id = r.survey_id WHERE r.id = ? AND s.id = ? AND s.workspace_id = ?)`, responseID, surveyID, workspaceID).Row().Scan(&exists); err != nil {
		return false, fmt.Errorf("check webhook event response: %w", err)
	}
	return exists, nil
}

// foreignSurveys returns the ids among surveyIDs that are not surveys of the workspace.
func (r *Repository) foreignSurveys(ctx context.Context, workspaceID uuid.UUID, surveyIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(surveyIDs) == 0 {
		return nil, nil
	}
	var found []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM surveys WHERE workspace_id = ? AND id = ANY(?::uuid[])`, workspaceID, uuidArray(surveyIDs)).Scan(&found).Error; err != nil {
		return nil, fmt.Errorf("check webhook surveys: %w", err)
	}
	own := make(map[uuid.UUID]bool, len(found))
	for _, id := range found {
		own[id] = true
	}
	var foreign []uuid.UUID
	for _, id := range surveyIDs {
		if !own[id] {
			foreign = append(foreign, id)
		}
	}
	return foreign, nil
}

// fields are the writable columns of a webhook. AllSurveys is set exactly when SurveyIDs is empty.
type fields struct {
	Name       *string
	URL        string
	Source     string
	Triggers   []string
	SurveyIDs  []uuid.UUID
	AllSurveys bool
}

// create inserts a webhook and the surveys it is scoped to, in one transaction.
func (r *Repository) create(ctx context.Context, workspaceID uuid.UUID, f fields, secret string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`INSERT INTO webhooks (workspace_id, name, url, source, triggers, secret, all_surveys) VALUES (?, ?, ?, ?, ?::text[], ?, ?) RETURNING id`,
			workspaceID, f.Name, f.URL, f.Source, textArray(f.Triggers), secret, f.AllSurveys).Row().Scan(&id); err != nil {
			return fmt.Errorf("insert webhook: %w", err)
		}
		return setSurveys(tx, id, f.SurveyIDs)
	})
	return id, err
}

// update is forma2's updateWebhook: name, URL, triggers and surveys; the source never changes.
func (r *Repository) update(ctx context.Context, id uuid.UUID, f fields) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE webhooks SET name = ?, url = ?, triggers = ?::text[], all_surveys = ?, updated_at = now() WHERE id = ?`,
			f.Name, f.URL, textArray(f.Triggers), f.AllSurveys, id).Error; err != nil {
			return fmt.Errorf("update webhook: %w", err)
		}
		if err := tx.Exec(`DELETE FROM webhook_surveys WHERE webhook_id = ?`, id).Error; err != nil {
			return fmt.Errorf("clear webhook surveys: %w", err)
		}
		return setSurveys(tx, id, f.SurveyIDs)
	})
}

// replace is forma2's v2 updateWebhook: every writable column, the source included.
func (r *Repository) replace(ctx context.Context, id uuid.UUID, f fields) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE webhooks SET name = ?, url = ?, source = ?, triggers = ?::text[], all_surveys = ?, updated_at = now() WHERE id = ?`,
			f.Name, f.URL, f.Source, textArray(f.Triggers), f.AllSurveys, id).Error; err != nil {
			return fmt.Errorf("update webhook: %w", err)
		}
		if err := tx.Exec(`DELETE FROM webhook_surveys WHERE webhook_id = ?`, id).Error; err != nil {
			return fmt.Errorf("clear webhook surveys: %w", err)
		}
		return setSurveys(tx, id, f.SurveyIDs)
	})
}

func setSurveys(tx *gorm.DB, id uuid.UUID, surveyIDs []uuid.UUID) error {
	if len(surveyIDs) == 0 {
		return nil
	}
	if err := tx.Exec(`INSERT INTO webhook_surveys (webhook_id, survey_id) SELECT ?, unnest(?::uuid[]) ON CONFLICT DO NOTHING`, id, uuidArray(surveyIDs)).Error; err != nil {
		return fmt.Errorf("insert webhook surveys: %w", err)
	}
	return nil
}

// delete removes the webhook and reports whether there was one.
func (r *Repository) delete(ctx context.Context, id uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM webhooks WHERE id = ?`, id)
	if res.Error != nil {
		return false, fmt.Errorf("delete webhook: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// environmentIDs is the environmentId the v1 payloads carry for each workspace that exists: the workspace's own id.
func (r *Repository) environmentIDs(ctx context.Context, workspaceIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM workspaces WHERE id = ANY(?::uuid[])`, uuidArray(workspaceIDs)).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("load workspaces: %w", err)
	}
	for _, id := range ids {
		out[id] = id.String()
	}
	return out, nil
}

// organizationOf is the organization that owns the workspace, or nil.
func (r *Repository) organizationOf(ctx context.Context, workspaceID uuid.UUID) (*uuid.UUID, error) {
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT organization_id FROM workspaces WHERE id = ?`, workspaceID).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("load workspace organization: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return &ids[0], nil
}

func uuidArray(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// textArray writes values from a closed set (trigger names) as a PostgreSQL array literal.
func textArray(values []string) string {
	return "{" + strings.Join(values, ",") + "}"
}
