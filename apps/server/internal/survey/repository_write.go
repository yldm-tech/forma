package survey

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// record is the columns a survey save writes.
type record struct {
	Name                        string
	Type                        string
	Status                      string
	RedirectURL                 *string
	CreatedBy                   *uuid.UUID
	WelcomeCard                 json.RawMessage
	Blocks                      json.RawMessage
	Endings                     json.RawMessage
	HiddenFields                json.RawMessage
	Variables                   json.RawMessage
	Metadata                    json.RawMessage
	InlineTriggers              json.RawMessage
	SurveyClosedMessage         json.RawMessage
	WorkspaceOverwrites         json.RawMessage
	Styling                     json.RawMessage
	SingleUse                   json.RawMessage
	Recaptcha                   json.RawMessage
	DisplayOption               string
	RecontactDays               *int
	DisplayLimit                *int
	DisplayPercentage           *float64
	AutoClose                   *int
	AutoComplete                *int
	Delay                       int
	PublishOn                   *time.Time
	CloseOn                     *time.Time
	IsVerifyEmailEnabled        bool
	IsBackButtonHidden          bool
	IsAutoProgressingEnabled    bool
	IsCaptureIPEnabled          bool
	IsAnonymizeResponsesEnabled bool
	IsProgressSavingEnabled     bool
	ShowLanguageSwitch          *bool
	Pin                         *string
	Slug                        *string
	CustomHeadScripts           *string
	CustomHeadScriptsMode       *string
}

// recordOf is a stored survey's columns, the starting point of an edit.
func recordOf(s *Survey) record {
	return record{
		Name: s.Name, Type: s.Type, Status: s.Status, RedirectURL: s.RedirectURL, CreatedBy: s.CreatedBy, WelcomeCard: s.WelcomeCard, Blocks: s.Blocks, Endings: s.Endings,
		HiddenFields: s.HiddenFields, Variables: s.Variables, Metadata: s.Metadata, InlineTriggers: s.InlineTriggers, SurveyClosedMessage: s.SurveyClosedMessage,
		WorkspaceOverwrites: s.WorkspaceOverwrites, Styling: s.Styling, SingleUse: s.SingleUse, Recaptcha: s.Recaptcha, DisplayOption: s.DisplayOption,
		RecontactDays: s.RecontactDays, DisplayLimit: s.DisplayLimit, DisplayPercentage: s.DisplayPercentage, AutoClose: s.AutoClose, AutoComplete: s.AutoComplete,
		Delay: s.Delay, PublishOn: s.PublishOn, CloseOn: s.CloseOn, IsVerifyEmailEnabled: s.IsVerifyEmailEnabled, IsBackButtonHidden: s.IsBackButtonHidden,
		IsAutoProgressingEnabled: s.IsAutoProgressingEnabled, IsCaptureIPEnabled: s.IsCaptureIPEnabled, IsAnonymizeResponsesEnabled: s.IsAnonymizeResponsesEnabled, IsProgressSavingEnabled: s.IsProgressSavingEnabled,
		ShowLanguageSwitch: s.ShowLanguageSwitch, Pin: s.Pin, Slug: s.Slug, CustomHeadScripts: s.CustomHeadScripts, CustomHeadScriptsMode: s.CustomHeadScriptsMode,
	}
}

// columns lists the record as column, SQL placeholder and value, in one order shared by insert and update.
func (rec *record) columns() ([]string, []string, []any) {
	cols := []struct {
		name  string
		json  bool
		value any
	}{
		{"name", false, rec.Name}, {"type", false, rec.Type}, {"status", false, rec.Status}, {"redirect_url", false, rec.RedirectURL},
		{"welcome_card", true, jsonParam(rec.WelcomeCard)}, {"blocks", true, jsonParam(rec.Blocks)}, {"endings", true, jsonParam(rec.Endings)},
		{"hidden_fields", true, jsonParam(rec.HiddenFields)}, {"variables", true, jsonParam(rec.Variables)}, {"metadata", true, jsonParam(rec.Metadata)},
		{"inline_triggers", true, jsonParam(rec.InlineTriggers)}, {"survey_closed_message", true, jsonParam(rec.SurveyClosedMessage)},
		{"workspace_overwrites", true, jsonParam(rec.WorkspaceOverwrites)}, {"styling", true, jsonParam(rec.Styling)},
		{"single_use", true, jsonParam(rec.SingleUse)}, {"recaptcha", true, jsonParam(rec.Recaptcha)},
		{"display_option", false, rec.DisplayOption}, {"recontact_days", false, rec.RecontactDays}, {"display_limit", false, rec.DisplayLimit},
		{"display_percentage", false, rec.DisplayPercentage}, {"auto_close", false, rec.AutoClose}, {"auto_complete", false, rec.AutoComplete},
		{"delay", false, rec.Delay}, {"publish_on", false, rec.PublishOn}, {"close_on", false, rec.CloseOn},
		{"is_verify_email_enabled", false, rec.IsVerifyEmailEnabled}, {"is_back_button_hidden", false, rec.IsBackButtonHidden},
		{"is_auto_progressing_enabled", false, rec.IsAutoProgressingEnabled}, {"is_capture_ip_enabled", false, rec.IsCaptureIPEnabled},
		{"is_anonymize_responses_enabled", false, rec.IsAnonymizeResponsesEnabled}, {"is_progress_saving_enabled", false, rec.IsProgressSavingEnabled}, {"show_language_switch", false, rec.ShowLanguageSwitch},
		{"pin", false, rec.Pin}, {"slug", false, rec.Slug}, {"custom_head_scripts", false, rec.CustomHeadScripts},
		{"custom_head_scripts_mode", false, rec.CustomHeadScriptsMode},
	}
	names := make([]string, len(cols))
	holders := make([]string, len(cols))
	values := make([]any, len(cols))
	for i, c := range cols {
		names[i], values[i] = c.name, c.value
		holders[i] = "?"
		if c.json {
			holders[i] = "?::jsonb"
		}
	}
	return names, holders, values
}

// insert creates a survey under the given id. A slug that another survey holds is reported as errSlugTaken.
func (r *Repository) insert(ctx context.Context, id, workspaceID uuid.UUID, rec record) error {
	names, holders, values := rec.columns()
	names = append([]string{"id", "workspace_id", "created_by"}, names...)
	holders = append([]string{"?", "?", "?"}, holders...)
	values = append([]any{id, workspaceID, rec.CreatedBy}, values...)
	query := `INSERT INTO surveys (` + strings.Join(names, ", ") + `) VALUES (` + strings.Join(holders, ", ") + `)`
	if _, err := r.exec(ctx, query, values...); err != nil {
		if isUniqueViolation(err, "surveys_slug_key") {
			return errSlugTaken
		}
		return fmt.Errorf("create survey: %w", err)
	}
	return nil
}

// update writes every column of the record and stamps updated_at.
func (r *Repository) update(ctx context.Context, id uuid.UUID, rec record) error {
	names, holders, values := rec.columns()
	sets := make([]string, len(names))
	for i := range names {
		sets[i] = names[i] + " = " + holders[i]
	}
	values = append(values, id)
	if _, err := r.exec(ctx, `UPDATE surveys SET `+strings.Join(sets, ", ")+`, updated_at = now() WHERE id = ?`, values...); err != nil {
		if isUniqueViolation(err, "surveys_slug_key") {
			return errSlugTaken
		}
		return fmt.Errorf("update survey: %w", err)
	}
	return nil
}

// lockForUpdate loads a survey with its relations and locks its row for the rest of the transaction, or returns nil when it does not exist.
func (r *Repository) lockForUpdate(ctx context.Context, id uuid.UUID) (*Survey, error) {
	var rows []surveyRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+surveyColumns+` FROM surveys s WHERE s.id = ? FOR UPDATE`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("lock survey: %w", err)
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

func (r *Repository) setSlug(ctx context.Context, id uuid.UUID, slug *string) (bool, error) {
	n, err := r.exec(ctx, `UPDATE surveys SET slug = ?, updated_at = now() WHERE id = ?`, slug, id)
	if err != nil {
		if isUniqueViolation(err, "surveys_slug_key") {
			return false, errSlugTaken
		}
		return false, fmt.Errorf("set survey slug: %w", err)
	}
	return n > 0, nil
}

// archive is forma2's archiveSurvey: an archived survey stays as it is; otherwise it is stamped and a running one is paused, keeping its publish date.
func (r *Repository) archive(ctx context.Context, id uuid.UUID, now time.Time) (*Lifecycle, error) {
	var out *Lifecycle
	err := r.transaction(ctx, func(tx *Repository) error {
		var rows []Lifecycle
		if err := tx.db.WithContext(ctx).Raw(`SELECT id, status, archived_at FROM surveys WHERE id = ? FOR UPDATE`, id).Scan(&rows).Error; err != nil {
			return fmt.Errorf("lock survey: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		if rows[0].ArchivedAt != nil {
			out = &rows[0]
			return nil
		}
		var updated []Lifecycle
		if err := tx.db.WithContext(ctx).Raw(`UPDATE surveys SET archived_at = ?, status = CASE WHEN status = 'inProgress' THEN 'paused' ELSE status END, updated_at = now()
			WHERE id = ? RETURNING id, status, archived_at`, now, id).Scan(&updated).Error; err != nil {
			return fmt.Errorf("archive survey: %w", err)
		}
		out = &updated[0]
		return nil
	})
	return out, err
}

// restore is forma2's restoreSurvey: the archive stamp is cleared and the status left as it is.
func (r *Repository) restore(ctx context.Context, id uuid.UUID) (*Lifecycle, error) {
	var rows []Lifecycle
	if err := r.db.WithContext(ctx).Raw(`UPDATE surveys SET archived_at = NULL, updated_at = now() WHERE id = ? RETURNING id, status, archived_at`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("restore survey: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// deleteSurvey is forma2's deleteSurvey: the survey goes, its responses, displays, triggers, languages, follow-ups, quotas and embedded data go with it through the foreign keys, and an app survey's private segment is deleted too. With archivedBefore set, only a survey still archived before that instant is deleted (the purge's re-check under a row lock). It returns the deleted survey, or nil when none was deleted.
func (r *Repository) deleteSurvey(ctx context.Context, id uuid.UUID, archivedBefore *time.Time) (*Survey, error) {
	var deleted *Survey
	err := r.transaction(ctx, func(tx *Repository) error {
		s, err := tx.lockForUpdate(ctx, id)
		if err != nil || s == nil {
			return err
		}
		if archivedBefore != nil && (s.ArchivedAt == nil || !s.ArchivedAt.Before(*archivedBefore)) {
			return nil
		}
		if _, err := tx.exec(ctx, `DELETE FROM surveys WHERE id = ?`, id); err != nil {
			return fmt.Errorf("delete survey: %w", err)
		}
		if s.Type == TypeApp && s.Segment != nil && s.Segment.IsPrivate {
			if err := tx.deleteSegment(ctx, s.Segment.ID); err != nil {
				return err
			}
		}
		deleted = s
		return nil
	})
	return deleted, err
}

// workspaceOfSurveys maps each given survey id to its workspace.
func (r *Repository) workspaceOfSurveys(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	var rows []struct {
		ID          uuid.UUID
		WorkspaceID uuid.UUID
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT id, workspace_id FROM surveys WHERE id = ANY(?::uuid[])`, uuidArray(ids)).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load survey workspaces: %w", err)
	}
	out := make(map[uuid.UUID]uuid.UUID, len(rows))
	for _, row := range rows {
		out[row.ID] = row.WorkspaceID
	}
	return out, nil
}

// connectSegmentSurveys points the given surveys at a segment.
func (r *Repository) connectSegmentSurveys(ctx context.Context, segmentID uuid.UUID, surveyIDs []uuid.UUID) error {
	if _, err := r.exec(ctx, `UPDATE surveys SET segment_id = ? WHERE id = ANY(?::uuid[])`, segmentID, uuidArray(surveyIDs)); err != nil {
		return fmt.Errorf("connect segment surveys: %w", err)
	}
	return nil
}

// updateSegment writes a segment's own mutable fields.
func (r *Repository) updateSegment(ctx context.Context, segmentID uuid.UUID, title string, description *string, isPrivate bool, filters json.RawMessage) error {
	if _, err := r.exec(ctx, `UPDATE segments SET title = ?, description = ?, is_private = ?, filters = ?::jsonb, updated_at = now() WHERE id = ?`,
		title, description, isPrivate, string(filters), segmentID); err != nil {
		return fmt.Errorf("update segment: %w", err)
	}
	return nil
}
