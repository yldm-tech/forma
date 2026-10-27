package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
)

// Workflow statuses, forma2's ZWorkflowStatus.
const (
	StatusDraft    = "draft"
	StatusEnabled  = "enabled"
	StatusDisabled = "disabled"
	StatusArchived = "archived"
)

// Run and step statuses, forma2's ZWorkflowRunStatus and ZWorkflowRunLogStatus.
const (
	RunQueued    = "queued"
	RunRunning   = "running"
	RunCompleted = "completed"
	RunFailed    = "failed"
	RunCanceled  = "canceled"

	StepPending   = "pending"
	StepRunning   = "running"
	StepSucceeded = "succeeded"
	StepFailed    = "failed"
	StepSkipped   = "skipped"
)

// Workflow is a workflows row with what its resources show next to it: the creator's name, the newest run and the number of runs.
type Workflow struct {
	ID          uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	WorkspaceID uuid.UUID
	CreatedBy   *uuid.UUID
	CreatorName *string
	Name        string
	Description *string
	Status      string
	Definition  json.RawMessage
	LastRun     *Run
	RunCount    int
}

// Run is a workflow_runs row; TriggerPayload and Data are only loaded for a run's detail.
type Run struct {
	ID                uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	WorkflowID        uuid.UUID
	WorkflowName      string
	WorkspaceID       uuid.UUID
	WorkflowVersionID *uuid.UUID
	ResponseID        *uuid.UUID
	SurveyID          *uuid.UUID
	Status            string
	TriggerType       string
	IsDryRun          bool
	IdempotencyKey    *string
	Attempt           int
	NextAttemptAt     *time.Time
	LastErrorAt       *time.Time
	TriggerPayload    json.RawMessage
	Data              json.RawMessage
	Error             *string
	DispatchedAt      *time.Time
	StartedAt         *time.Time
	FinishedAt        *time.Time
	Logs              []RunLog `gorm:"-"`
}

// RunLog is a workflow_run_logs row: one step of a run.
type RunLog struct {
	ID         uuid.UUID
	RunID      uuid.UUID
	Sequence   int
	StepID     string
	StepType   string
	Status     string
	Input      json.RawMessage
	Output     json.RawMessage
	Error      *string
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// Repository reads and writes workflows, their versions and their runs.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

const workflowColumns = `w.id, w.created_at, w.updated_at, w.workspace_id, w.created_by, u.name AS creator_name, w.name, w.description, w.status, w.definition`

const runSummaryColumns = `r.id, r.created_at, r.updated_at, r.workflow_id, r.workspace_id, r.workflow_version_id, r.response_id, r.survey_id, r.status, r.trigger_type, r.is_dry_run, r.idempotency_key, r.attempt, r.next_attempt_at, r.last_error_at, r.error, r.dispatched_at, r.started_at, r.finished_at`

// byID loads a workflow with its newest run and run count; nil when there is none.
func (r *Repository) byID(ctx context.Context, id uuid.UUID) (*Workflow, error) {
	return r.byIDOn(ctx, r.db, id)
}

func (r *Repository) byIDOn(ctx context.Context, db *gorm.DB, id uuid.UUID) (*Workflow, error) {
	var rows []Workflow
	if err := db.WithContext(ctx).Raw(`SELECT `+workflowColumns+` FROM workflows w LEFT JOIN users u ON u.id = w.created_by WHERE w.id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workflow: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if err := r.attachRuns(ctx, db, rows[0].WorkspaceID, rows); err != nil {
		return nil, err
	}
	return &rows[0], nil
}

// attachRuns fills each workflow's newest run and run count with one query each for the whole page.
func (r *Repository) attachRuns(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID, rows []Workflow) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, w := range rows {
		ids[i] = w.ID
	}
	var last []Run
	if err := db.WithContext(ctx).Raw(`SELECT DISTINCT ON (r.workflow_id) `+runSummaryColumns+` FROM workflow_runs r
		WHERE r.workflow_id = ANY(?::uuid[]) AND r.workspace_id = ? ORDER BY r.workflow_id, r.created_at DESC, r.id DESC`, uuidArray(ids), workspaceID).Scan(&last).Error; err != nil {
		return fmt.Errorf("load last workflow runs: %w", err)
	}
	var counts []struct {
		WorkflowID uuid.UUID
		Count      int
	}
	if err := db.WithContext(ctx).Raw(`SELECT workflow_id, count(*) AS count FROM workflow_runs WHERE workflow_id = ANY(?::uuid[]) AND workspace_id = ? GROUP BY workflow_id`, uuidArray(ids), workspaceID).Scan(&counts).Error; err != nil {
		return fmt.Errorf("count workflow runs: %w", err)
	}
	for i := range rows {
		for j := range last {
			if last[j].WorkflowID == rows[i].ID {
				rows[i].LastRun = &last[j]
			}
		}
		for _, c := range counts {
			if c.WorkflowID == rows[i].ID {
				rows[i].RunCount = c.Count
			}
		}
	}
	return nil
}

// listQuery is a page of a workspace's workflows.
type listQuery struct {
	WorkspaceID  uuid.UUID
	Limit        int
	Cursor       *listCursor
	StatusIn     []string
	NameContains string
	SortBy       string
}

func uuidArray(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// textArray writes values from a closed set (statuses) as a PostgreSQL array literal.
func textArray(values []string) string {
	return "{" + strings.Join(values, ",") + "}"
}

// escapeLike makes s match itself literally inside an ILIKE pattern.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// list is forma2's listWorkflows: limit+1 rows in the sort's order after the cursor, archived workflows left out unless the statuses are given.
func (r *Repository) list(ctx context.Context, q listQuery) ([]Workflow, bool, error) {
	where := []string{"w.workspace_id = ?"}
	args := []any{q.WorkspaceID}
	if len(q.StatusIn) > 0 {
		where = append(where, "w.status = ANY(?::text[])")
		args = append(args, textArray(q.StatusIn))
	} else {
		where = append(where, "w.status <> 'archived'")
	}
	if q.NameContains != "" {
		where = append(where, `w.name ILIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(q.NameContains)+"%")
	}
	order := "w.updated_at DESC, w.id DESC"
	switch q.SortBy {
	case sortName:
		order = "w.name ASC, w.id ASC"
	case sortCreatedAt:
		order = "w.created_at DESC, w.id DESC"
	}
	if c := q.Cursor; c != nil {
		switch c.SortBy {
		case sortName:
			where = append(where, "(w.name > ? OR (w.name = ? AND w.id > ?))")
			args = append(args, c.Value, c.Value, c.id)
		case sortCreatedAt:
			where = append(where, "(w.created_at < ? OR (w.created_at = ? AND w.id < ?))")
			args = append(args, c.at, c.at, c.id)
		default:
			where = append(where, "(w.updated_at < ? OR (w.updated_at = ? AND w.id < ?))")
			args = append(args, c.at, c.at, c.id)
		}
	}
	args = append(args, q.Limit+1)
	var rows []Workflow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+workflowColumns+` FROM workflows w LEFT JOIN users u ON u.id = w.created_by
		WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order+` LIMIT ?`, args...).Scan(&rows).Error; err != nil {
		return nil, false, fmt.Errorf("list workflows: %w", err)
	}
	more := len(rows) > q.Limit
	if more {
		rows = rows[:q.Limit]
	}
	if err := r.attachRuns(ctx, r.db, q.WorkspaceID, rows); err != nil {
		return nil, false, err
	}
	return rows, more, nil
}

func (r *Repository) create(ctx context.Context, workspaceID uuid.UUID, name string, description *string, definition json.RawMessage, createdBy *uuid.UUID) (*Workflow, error) {
	var id uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`INSERT INTO workflows (workspace_id, name, description, status, definition, created_by) VALUES (?, ?, ?, 'draft', ?::jsonb, ?) RETURNING id`,
		workspaceID, name, description, string(definition), createdBy).Row().Scan(&id); err != nil {
		return nil, fmt.Errorf("create workflow: %w", err)
	}
	return r.byID(ctx, id)
}

// update writes the fields that are set, scoped to the workflow's workspace.
func (r *Repository) update(ctx context.Context, id, workspaceID uuid.UUID, set map[string]any) (*Workflow, error) {
	set["updated_at"] = gorm.Expr("now()")
	if err := r.db.WithContext(ctx).Table("workflows").Where("id = ? AND workspace_id = ?", id, workspaceID).Updates(set).Error; err != nil {
		return nil, fmt.Errorf("update workflow: %w", err)
	}
	return r.byID(ctx, id)
}

func (r *Repository) delete(ctx context.Context, id, workspaceID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM workflows WHERE id = ? AND workspace_id = ?`, id, workspaceID).Error; err != nil {
		return fmt.Errorf("delete workflow: %w", err)
	}
	return nil
}

var (
	errNotEnableable  = errors.New("the workflow is no longer in a state that can be enabled")
	errVersionRace    = errors.New("the workflow was just enabled by another request")
	errWorkflowMissed = errors.New("the workflow no longer exists")
)

// enable is forma2's enableWorkflow: in one transaction, flip a draft or disabled workflow to enabled with the trigger's survey, then publish the definition as the next version.
func (r *Repository) enable(ctx context.Context, id, workspaceID uuid.UUID, definition json.RawMessage, surveyID uuid.UUID, publishedBy *uuid.UUID) (*Workflow, error) {
	var out *Workflow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE workflows SET status = 'enabled', trigger_survey_id = ?, updated_at = now() WHERE id = ? AND workspace_id = ? AND status IN ('draft', 'disabled')`, surveyID, id, workspaceID)
		if res.Error != nil {
			return fmt.Errorf("enable workflow: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return errNotEnableable
		}
		if err := tx.Exec(`INSERT INTO workflow_versions (workflow_id, workspace_id, version, definition, published_by)
			VALUES (?, ?, (SELECT coalesce(max(version), 0) + 1 FROM workflow_versions WHERE workflow_id = ?), ?::jsonb, ?)`,
			id, workspaceID, id, string(definition), publishedBy).Error; err != nil {
			if isUniqueViolation(err) {
				return errVersionRace
			}
			return fmt.Errorf("publish workflow version: %w", err)
		}
		w, err := r.byIDOn(ctx, tx, id)
		if err != nil {
			return err
		}
		if w == nil {
			return errWorkflowMissed
		}
		out = w
		return nil
	})
	return out, err
}

// surveyEndings is what enable checks of the survey a trigger names: whether it is in the workspace, and its ending ids.
func (r *Repository) surveyEndings(ctx context.Context, surveyID, workspaceID uuid.UUID) ([]string, bool, error) {
	var rows []struct{ Endings []byte }
	if err := r.db.WithContext(ctx).Raw(`SELECT endings FROM surveys WHERE id = ? AND workspace_id = ?`, surveyID, workspaceID).Scan(&rows).Error; err != nil {
		return nil, false, fmt.Errorf("load trigger survey: %w", err)
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	var endings []struct {
		ID string `json:"id"`
	}
	if len(rows[0].Endings) > 0 {
		if err := json.Unmarshal(rows[0].Endings, &endings); err != nil {
			return nil, false, fmt.Errorf("decode survey endings: %w", err)
		}
	}
	ids := make([]string, len(endings))
	for i, e := range endings {
		ids[i] = e.ID
	}
	return ids, true, nil
}

// Member is someone who can access a workspace, as the send_email recipient picker lists them.
type Member struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Members is forma2's getWorkspaceMembers: the active users of the workspace's organization that it does not refuse (neither suspended nor refused by its two-factor requirement, authz.MemberNotRefusedSQL) who are its owners or managers, or who are on a team with access to the workspace. It is also the send_email allowlist, so a refused member is not mailed the responses the app refuses them.
func Members(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID) ([]Member, error) {
	var rows []Member
	if err := db.WithContext(ctx).Raw(`SELECT u.name, u.email FROM memberships m
		JOIN workspaces w ON w.organization_id = m.organization_id
		JOIN users u ON u.id = m.user_id
		WHERE w.id = ? AND u.is_active AND `+authz.MemberNotRefusedSQL("m")+` AND u.email <> ''
		AND (m.role IN ('owner', 'manager') OR EXISTS (
			SELECT 1 FROM team_members tm JOIN workspace_teams wt ON wt.team_id = tm.team_id WHERE tm.user_id = u.id AND wt.workspace_id = w.id))
		ORDER BY u.name, u.email`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workspace members: %w", err)
	}
	return rows, nil
}

// normalizeEmail is forma2's normalizeEmailForComparison.
func normalizeEmail(email string) string {
	return strings.ToLower(jsvalue.Trim(email))
}

// MemberEmails is forma2's getWorkspaceMemberEmails: the recipient allowlist of send_email, normalized for comparison.
func MemberEmails(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID) (map[string]bool, error) {
	members, err := Members(ctx, db, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(members))
	for _, m := range members {
		out[normalizeEmail(m.Email)] = true
	}
	return out, nil
}

// organizationOf is the workspace's organization, for audit events.
func (r *Repository) organizationOf(ctx context.Context, workspaceID uuid.UUID) (uuid.UUID, error) {
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT organization_id FROM workspaces WHERE id = ?`, workspaceID).Scan(&ids).Error; err != nil {
		return uuid.Nil, fmt.Errorf("load workspace organization: %w", err)
	}
	if len(ids) == 0 {
		return uuid.Nil, nil
	}
	return ids[0], nil
}

// runQuery is a page of a workspace's runs.
type runQuery struct {
	WorkspaceID uuid.UUID
	Limit       int
	Cursor      *runCursor
	WorkflowID  *uuid.UUID
	ResponseID  *uuid.UUID
	StatusIn    []string
	IsDryRun    *bool
}

// listRuns is forma2's listWorkflowRuns: newest first, without the payload and data columns, with the workflow's name.
func (r *Repository) listRuns(ctx context.Context, q runQuery) ([]Run, bool, error) {
	where := []string{"r.workspace_id = ?"}
	args := []any{q.WorkspaceID}
	if q.WorkflowID != nil {
		where = append(where, "r.workflow_id = ?")
		args = append(args, *q.WorkflowID)
	}
	if q.ResponseID != nil {
		where = append(where, "r.response_id = ?")
		args = append(args, *q.ResponseID)
	}
	if len(q.StatusIn) > 0 {
		where = append(where, "r.status = ANY(?::text[])")
		args = append(args, textArray(q.StatusIn))
	}
	if q.IsDryRun != nil {
		where = append(where, "r.is_dry_run = ?")
		args = append(args, *q.IsDryRun)
	}
	if c := q.Cursor; c != nil {
		where = append(where, "(r.created_at < ? OR (r.created_at = ? AND r.id < ?))")
		args = append(args, c.at, c.at, c.id)
	}
	args = append(args, q.Limit+1)
	var rows []Run
	if err := r.db.WithContext(ctx).Raw(`SELECT `+runSummaryColumns+`, w.name AS workflow_name FROM workflow_runs r JOIN workflows w ON w.id = r.workflow_id
		WHERE `+strings.Join(where, " AND ")+` ORDER BY r.created_at DESC, r.id DESC LIMIT ?`, args...).Scan(&rows).Error; err != nil {
		return nil, false, fmt.Errorf("list workflow runs: %w", err)
	}
	more := len(rows) > q.Limit
	if more {
		rows = rows[:q.Limit]
	}
	return rows, more, nil
}

// run loads a run with its payload, data and step logs in sequence order; nil when there is none.
func (r *Repository) run(ctx context.Context, id uuid.UUID) (*Run, error) {
	var rows []Run
	if err := r.db.WithContext(ctx).Raw(`SELECT `+runSummaryColumns+`, r.trigger_payload, r.data FROM workflow_runs r WHERE r.id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workflow run: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	run := &rows[0]
	if err := r.db.WithContext(ctx).Raw(`SELECT id, run_id, sequence, step_id, step_type, status, input, output, error, started_at, finished_at
		FROM workflow_run_logs WHERE run_id = ? ORDER BY sequence, id`, id).Scan(&run.Logs).Error; err != nil {
		return nil, fmt.Errorf("load workflow run logs: %w", err)
	}
	if run.Logs == nil {
		run.Logs = []RunLog{}
	}
	return run, nil
}
