package survey

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
)

// Repository reads and writes surveys and the rows that belong to them. Every statement names the survey or workspace it is scoped to.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) withTx(tx *gorm.DB) *Repository { return &Repository{db: tx} }

// transaction runs fn in one transaction.
func (r *Repository) transaction(ctx context.Context, fn func(tx *Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(r.withTx(tx)) })
}

func (r *Repository) exec(ctx context.Context, query string, args ...any) (int64, error) {
	res := r.db.WithContext(ctx).Exec(query, args...)
	return res.RowsAffected, res.Error
}

// jsonParam is a JSON column value: SQL NULL for an absent or JSON-null value.
func jsonParam(raw json.RawMessage) any {
	if isNullJSON(raw) {
		return nil
	}
	return string(raw)
}

func isNullJSON(raw json.RawMessage) bool {
	t := strings.TrimSpace(string(raw))
	return t == "" || t == "null"
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && (constraint == "" || pgErr.ConstraintName == constraint)
}

const surveyColumns = `s.id, s.created_at, s.updated_at, s.workspace_id, s.created_by, s.segment_id, s.name, s.type, s.status,
	s.welcome_card, s.blocks, s.endings, s.hidden_fields, s.variables, s.inline_triggers, s.survey_closed_message,
	s.workspace_overwrites, s.styling, s.single_use, s.recaptcha, s.metadata, s.display_option, s.recontact_days,
	s.display_limit, s.display_percentage::float8 AS display_percentage, s.auto_close, s.auto_complete, s.delay, s.publish_on, s.close_on,
	s.archived_at, s.is_verify_email_enabled, s.is_back_button_hidden, s.is_auto_progressing_enabled, s.is_capture_ip_enabled,
	s.is_anonymize_responses_enabled, s.show_language_switch, s.pin, s.slug, s.custom_head_scripts, s.custom_head_scripts_mode`

type surveyRow struct {
	ID                          uuid.UUID
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
	WorkspaceID                 uuid.UUID
	CreatedBy                   *uuid.UUID
	SegmentID                   *uuid.UUID
	Name                        string
	Type                        string
	Status                      string
	WelcomeCard                 []byte
	Blocks                      []byte
	Endings                     []byte
	HiddenFields                []byte
	Variables                   []byte
	InlineTriggers              []byte
	SurveyClosedMessage         []byte
	WorkspaceOverwrites         []byte
	Styling                     []byte
	SingleUse                   []byte
	Recaptcha                   []byte
	Metadata                    []byte
	DisplayOption               string
	RecontactDays               *int
	DisplayLimit                *int
	DisplayPercentage           *float64
	AutoClose                   *int
	AutoComplete                *int
	Delay                       int
	PublishOn                   *time.Time
	CloseOn                     *time.Time
	ArchivedAt                  *time.Time
	IsVerifyEmailEnabled        bool
	IsBackButtonHidden          bool
	IsAutoProgressingEnabled    bool
	IsCaptureIPEnabled          bool `gorm:"column:is_capture_ip_enabled"`
	IsAnonymizeResponsesEnabled bool
	ShowLanguageSwitch          *bool
	Pin                         *string
	Slug                        *string
	CustomHeadScripts           *string
	CustomHeadScriptsMode       *string
}

func rawOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

func (row surveyRow) survey() *Survey {
	return &Survey{
		ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Name: row.Name, Type: row.Type, WorkspaceID: row.WorkspaceID,
		CreatedBy: row.CreatedBy, Status: row.Status, DisplayOption: row.DisplayOption, AutoClose: row.AutoClose, RecontactDays: row.RecontactDays,
		DisplayLimit: row.DisplayLimit, WelcomeCard: rawOrNull(row.WelcomeCard), Blocks: rawOrNull(row.Blocks), Endings: rawOrNull(row.Endings),
		HiddenFields: rawOrNull(row.HiddenFields), Variables: rawOrNull(row.Variables), Delay: row.Delay, PublishOn: row.PublishOn,
		CloseOn: row.CloseOn, ArchivedAt: row.ArchivedAt, AutoComplete: row.AutoComplete, WorkspaceOverwrites: rawOrNull(row.WorkspaceOverwrites),
		Styling: rawOrNull(row.Styling), ShowLanguageSwitch: row.ShowLanguageSwitch, SurveyClosedMessage: rawOrNull(row.SurveyClosedMessage),
		SingleUse: rawOrNull(row.SingleUse), IsVerifyEmailEnabled: row.IsVerifyEmailEnabled, Recaptcha: rawOrNull(row.Recaptcha),
		IsBackButtonHidden: row.IsBackButtonHidden, IsAutoProgressingEnabled: row.IsAutoProgressingEnabled, IsCaptureIPEnabled: row.IsCaptureIPEnabled,
		IsAnonymizeResponsesEnabled: row.IsAnonymizeResponsesEnabled, Pin: row.Pin, DisplayPercentage: row.DisplayPercentage,
		Metadata: rawOrNull(row.Metadata), Slug: row.Slug, CustomHeadScripts: row.CustomHeadScripts, CustomHeadScriptsMode: row.CustomHeadScriptsMode,
		InlineTriggers: rawOrNull(row.InlineTriggers), Triggers: []Trigger{}, FollowUps: []FollowUp{}, Languages: []SurveyLanguage{},
	}
}

// byID loads a survey with its languages, triggers, segment and follow-ups, or nil when it does not exist.
func (r *Repository) byID(ctx context.Context, id uuid.UUID) (*Survey, error) {
	var rows []surveyRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+surveyColumns+` FROM surveys s WHERE s.id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load survey: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	s := rows[0].survey()
	if err := r.loadRelations(ctx, s, rows[0].SegmentID); err != nil {
		return nil, err
	}
	return s, nil
}

func (r *Repository) loadRelations(ctx context.Context, s *Survey, segmentID *uuid.UUID) error {
	langs, err := r.surveyLanguages(ctx, s.ID)
	if err != nil {
		return err
	}
	s.Languages = langs
	if s.Triggers, err = r.triggers(ctx, s.ID); err != nil {
		return err
	}
	if s.FollowUps, err = r.followUps(ctx, s.ID); err != nil {
		return err
	}
	if segmentID != nil {
		if s.Segment, err = r.segment(ctx, *segmentID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) surveyLanguages(ctx context.Context, surveyID uuid.UUID) ([]SurveyLanguage, error) {
	var rows []struct {
		ID          uuid.UUID
		CreatedAt   time.Time
		UpdatedAt   time.Time
		Code        string
		Alias       *string
		WorkspaceID uuid.UUID
		IsDefault   bool
		Enabled     bool
	}
	err := r.db.WithContext(ctx).Raw(`SELECT l.id, l.created_at, l.updated_at, l.code, l.alias, l.workspace_id, sl.is_default, sl.enabled
		FROM survey_languages sl JOIN languages l ON l.id = sl.language_id WHERE sl.survey_id = ? ORDER BY sl.is_default DESC, l.code, l.id`, surveyID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load survey languages: %w", err)
	}
	out := make([]SurveyLanguage, len(rows))
	for i, l := range rows {
		out[i] = SurveyLanguage{Language: Language{ID: l.ID, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt, Code: l.Code, Alias: l.Alias, WorkspaceID: l.WorkspaceID}, Default: l.IsDefault, Enabled: l.Enabled}
	}
	return out, nil
}

type actionClassRow struct {
	ID           uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Name         string
	Description  *string
	Type         string
	Key          *string
	NoCodeConfig []byte
	WorkspaceID  uuid.UUID
}

func (a actionClassRow) actionClass() ActionClass {
	return ActionClass{ID: a.ID, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, Name: a.Name, Description: a.Description, Type: a.Type, Key: a.Key, NoCodeConfig: rawOrNull(a.NoCodeConfig), WorkspaceID: a.WorkspaceID}
}

const actionClassColumns = `a.id, a.created_at, a.updated_at, a.name, a.description, a.type, a.key, a.no_code_config, a.workspace_id`

func (r *Repository) triggers(ctx context.Context, surveyID uuid.UUID) ([]Trigger, error) {
	var rows []actionClassRow
	err := r.db.WithContext(ctx).Raw(`SELECT `+actionClassColumns+` FROM survey_triggers t JOIN action_classes a ON a.id = t.action_class_id
		WHERE t.survey_id = ? ORDER BY t.created_at, a.id`, surveyID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load survey triggers: %w", err)
	}
	out := make([]Trigger, len(rows))
	for i, a := range rows {
		out[i] = Trigger{ActionClass: a.actionClass()}
	}
	return out, nil
}

// actionClasses lists a workspace's action classes, oldest first, as forma2's getActionClasses did.
func (r *Repository) actionClasses(ctx context.Context, workspaceID uuid.UUID) ([]ActionClass, error) {
	var rows []actionClassRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+actionClassColumns+` FROM action_classes a WHERE a.workspace_id = ? ORDER BY a.created_at, a.id`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load action classes: %w", err)
	}
	out := make([]ActionClass, len(rows))
	for i, a := range rows {
		out[i] = a.actionClass()
	}
	return out, nil
}

// setTriggers makes the survey's triggers exactly the given action classes, keeping the ones it already has.
func (r *Repository) setTriggers(ctx context.Context, surveyID uuid.UUID, actionClassIDs []uuid.UUID) error {
	if _, err := r.exec(ctx, `DELETE FROM survey_triggers WHERE survey_id = ? AND NOT (action_class_id = ANY(?::uuid[]))`, surveyID, uuidArray(actionClassIDs)); err != nil {
		return fmt.Errorf("remove survey triggers: %w", err)
	}
	for _, id := range actionClassIDs {
		if _, err := r.exec(ctx, `INSERT INTO survey_triggers (survey_id, action_class_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, surveyID, id); err != nil {
			return fmt.Errorf("add survey trigger: %w", err)
		}
	}
	return nil
}

func uuidArray(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (r *Repository) segment(ctx context.Context, id uuid.UUID) (*Segment, error) {
	var rows []struct {
		ID          uuid.UUID
		CreatedAt   time.Time
		UpdatedAt   time.Time
		Title       string
		Description *string
		IsPrivate   bool
		Filters     []byte
		WorkspaceID uuid.UUID
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, title, description, is_private, filters, workspace_id FROM segments WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load segment: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	s := rows[0]
	var surveys []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM surveys WHERE segment_id = ? ORDER BY created_at, id`, id).Scan(&surveys).Error; err != nil {
		return nil, fmt.Errorf("load segment surveys: %w", err)
	}
	if surveys == nil {
		surveys = []uuid.UUID{}
	}
	return &Segment{ID: s.ID, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Title: s.Title, Description: s.Description, IsPrivate: s.IsPrivate, Filters: rawOrNull(s.Filters), WorkspaceID: s.WorkspaceID, Surveys: surveys}, nil
}

// segmentInWorkspace loads a segment only when it belongs to the workspace.
func (r *Repository) segmentInWorkspace(ctx context.Context, id, workspaceID uuid.UUID) (*Segment, error) {
	s, err := r.segment(ctx, id)
	if err != nil || s == nil || s.WorkspaceID != workspaceID {
		return nil, err
	}
	return s, nil
}

// createSegment inserts a segment and returns its id.
func (r *Repository) createSegment(ctx context.Context, workspaceID uuid.UUID, title string, isPrivate bool, filters json.RawMessage) (uuid.UUID, error) {
	if isNullJSON(filters) {
		filters = json.RawMessage(`[]`)
	}
	var id uuid.UUID
	err := r.db.WithContext(ctx).Raw(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, ?, ?, ?::jsonb) RETURNING id`,
		workspaceID, title, isPrivate, string(filters)).Row().Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create segment: %w", err)
	}
	return id, nil
}

func (r *Repository) segmentTitleExists(ctx context.Context, workspaceID uuid.UUID, title string) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM segments WHERE workspace_id = ? AND title = ?)`, workspaceID, title).Scan(&exists).Error
	if err != nil {
		return false, fmt.Errorf("check segment title: %w", err)
	}
	return exists, nil
}

func (r *Repository) setSegmentFilters(ctx context.Context, segmentID uuid.UUID, filters json.RawMessage) error {
	if _, err := r.exec(ctx, `UPDATE segments SET filters = ?::jsonb, updated_at = now() WHERE id = ?`, string(filters), segmentID); err != nil {
		return fmt.Errorf("update segment filters: %w", err)
	}
	return nil
}

func (r *Repository) deleteSegment(ctx context.Context, segmentID uuid.UUID) error {
	if _, err := r.exec(ctx, `DELETE FROM segments WHERE id = ?`, segmentID); err != nil {
		return fmt.Errorf("delete segment: %w", err)
	}
	return nil
}

func (r *Repository) setSurveySegment(ctx context.Context, surveyID uuid.UUID, segmentID *uuid.UUID) error {
	if _, err := r.exec(ctx, `UPDATE surveys SET segment_id = ? WHERE id = ?`, segmentID, surveyID); err != nil {
		return fmt.Errorf("set survey segment: %w", err)
	}
	return nil
}

func (r *Repository) followUps(ctx context.Context, surveyID uuid.UUID) ([]FollowUp, error) {
	var rows []struct {
		ID        uuid.UUID
		CreatedAt time.Time
		UpdatedAt time.Time
		Name      string
		Trigger   []byte
		Action    []byte
		SurveyID  uuid.UUID
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, name, trigger, action, survey_id FROM survey_follow_ups WHERE survey_id = ? ORDER BY created_at, id`, surveyID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load follow-ups: %w", err)
	}
	out := make([]FollowUp, len(rows))
	for i, f := range rows {
		out[i] = FollowUp{ID: f.ID, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt, Name: f.Name, Trigger: f.Trigger, Action: f.Action, SurveyID: f.SurveyID}
	}
	return out, nil
}

// followUpWrite is one follow-up of a save.
type followUpWrite struct {
	ID      uuid.UUID
	Name    string
	Trigger json.RawMessage
	Action  json.RawMessage
	Deleted bool
}

// applyFollowUps is forma2's follow-up diff on save: deleted ones are removed, known ones updated, the rest created under the id the client chose.
func (r *Repository) applyFollowUps(ctx context.Context, surveyID uuid.UUID, writes []followUpWrite) error {
	for _, f := range writes {
		if f.Deleted {
			if _, err := r.exec(ctx, `DELETE FROM survey_follow_ups WHERE id = ? AND survey_id = ?`, f.ID, surveyID); err != nil {
				return fmt.Errorf("delete follow-up: %w", err)
			}
			continue
		}
		n, err := r.exec(ctx, `UPDATE survey_follow_ups SET name = ?, trigger = ?::jsonb, action = ?::jsonb, updated_at = now() WHERE id = ? AND survey_id = ?`,
			f.Name, string(f.Trigger), string(f.Action), f.ID, surveyID)
		if err != nil {
			return fmt.Errorf("update follow-up: %w", err)
		}
		if n > 0 {
			continue
		}
		if _, err := r.exec(ctx, `INSERT INTO survey_follow_ups (id, survey_id, name, trigger, action) VALUES (?, ?, ?, ?::jsonb, ?::jsonb)`,
			f.ID, surveyID, f.Name, string(f.Trigger), string(f.Action)); err != nil {
			return fmt.Errorf("create follow-up: %w", err)
		}
	}
	return nil
}

// workspaceLanguages lists the given languages of a workspace.
func (r *Repository) workspaceLanguagesByID(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Language, error) {
	var rows []Language
	if err := r.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, code, alias, workspace_id FROM languages WHERE workspace_id = ? AND id = ANY(?::uuid[])`,
		workspaceID, uuidArray(ids)).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workspace languages: %w", err)
	}
	out := make(map[uuid.UUID]Language, len(rows))
	for _, l := range rows {
		out[l.ID] = l
	}
	return out, nil
}

// ensureLanguages is forma2's connect-or-create of workspace languages by code; it returns them by lowercased code.
func (r *Repository) ensureLanguages(ctx context.Context, workspaceID uuid.UUID, codes []string) (map[string]Language, error) {
	for _, code := range codes {
		if _, err := r.exec(ctx, `INSERT INTO languages (workspace_id, code) VALUES (?, ?) ON CONFLICT (workspace_id, code) DO NOTHING`, workspaceID, code); err != nil {
			return nil, fmt.Errorf("create workspace language: %w", err)
		}
	}
	var rows []Language
	if err := r.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, code, alias, workspace_id FROM languages WHERE workspace_id = ? AND code = ANY(?::text[])`,
		workspaceID, textArray(codes)).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workspace languages: %w", err)
	}
	out := make(map[string]Language, len(rows))
	for _, l := range rows {
		out[strings.ToLower(l.Code)] = l
	}
	return out, nil
}

func textArray(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// languageLink is one survey language to store.
type languageLink struct {
	LanguageID uuid.UUID
	Default    bool
	Enabled    bool
}

// setLanguages makes the survey's languages exactly links.
func (r *Repository) setLanguages(ctx context.Context, surveyID uuid.UUID, links []languageLink) error {
	if _, err := r.exec(ctx, `DELETE FROM survey_languages WHERE survey_id = ?`, surveyID); err != nil {
		return fmt.Errorf("clear survey languages: %w", err)
	}
	for _, l := range links {
		if _, err := r.exec(ctx, `INSERT INTO survey_languages (survey_id, language_id, is_default, enabled) VALUES (?, ?, ?, ?)`, surveyID, l.LanguageID, l.Default, l.Enabled); err != nil {
			return fmt.Errorf("add survey language: %w", err)
		}
	}
	return nil
}

// organizationOf is the organization of a workspace, or nil when the workspace does not exist.
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

// workspaceName is the name of a workspace, "" when it does not exist.
func (r *Repository) workspaceName(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	var names []string
	if err := r.db.WithContext(ctx).Raw(`SELECT name FROM workspaces WHERE id = ?`, workspaceID).Scan(&names).Error; err != nil {
		return "", fmt.Errorf("load workspace: %w", err)
	}
	if len(names) == 0 {
		return "", nil
	}
	return names[0], nil
}
