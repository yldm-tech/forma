package response

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/tag"
)

// The writes of the management APIs (forma2's /api/v1/management/responses and /api/v2/management/responses): an API caller replays or edits responses, so its meta, timestamps and contact are taken as sent instead of being captured from a respondent's request. The API module validates the input and authorizes the API key first; these authorize nothing.

// Errors of a management write the API answers in its own format.
var (
	// ErrDisplayNotFound is a displayId that names no display of the response's survey (or, on a replace, one another response already answers).
	ErrDisplayNotFound = errors.New("display not found")
	// ErrContactNotFound is a contactId that names no contact of the response's workspace.
	ErrContactNotFound = errors.New("contact not found")
	// ErrOrganizationNotFound is a workspace without an organization.
	ErrOrganizationNotFound = errors.New("organization not found")
)

// ManagedInput is a response a management API creates, forma2's createResponse input there.
type ManagedInput struct {
	// UserID attaches the response to the workspace contact whose userId attribute holds it, when there is one; no contact is created.
	UserID      *string
	DisplayID   *string
	SingleUseID *string
	Finished    bool
	EndingID    *string
	Language    *string
	Data        map[string]any
	// Variables, TTC and Meta are nil when the caller sent none.
	Variables map[string]any
	TTC       map[string]float64
	Meta      map[string]any
	// ClampTTC applies v1's ZResponseTtcInput, which bounds each time to [0, 24 h]; v2 stores the times as sent.
	ClampTTC  bool
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

// CreateManaged is forma2's management createResponseWithQuotaEvaluation and sendToPipeline: one transaction checks the workspace's organization and the display, resolves and snapshots the contact, inserts the response with the caller's meta under the survey's anonymize policy, evaluates the quotas and enqueues the pipeline events. It returns the response as stored afterwards, a quota's ending included.
func (s *Service) CreateManaged(ctx context.Context, sv *Survey, in ManagedInput) (*Response, error) {
	meta := in.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	meta = anonymize(meta, sv.IsAnonymizeResponsesEnabled)
	ttc := map[string]float64{}
	if in.TTC != nil {
		ttc = in.TTC
		if in.ClampTTC {
			ttc = clampTTC(in.TTC)
		}
		if in.Finished {
			ttc = ttcTotal(ttc)
		}
	}
	variables := in.Variables
	if variables == nil {
		variables = map[string]any{}
	}
	data := in.Data
	if data == nil {
		data = map[string]any{}
	}
	language := normalizeLanguage(in.Language)
	updatedAt := in.UpdatedAt
	if in.CreatedAt != nil && updatedAt == nil {
		updatedAt = in.CreatedAt
	}

	var id uuid.UUID
	err := s.jobs.Transaction(ctx, s.db, func(tx *jobs.Tx) error {
		if err := organizationExists(ctx, tx.DB, sv.WorkspaceID); err != nil {
			return err
		}
		var displayID *uuid.UUID
		if in.DisplayID != nil && *in.DisplayID != "" {
			d, err := displayOfSurvey(ctx, tx.DB, *in.DisplayID, sv.ID)
			if err != nil {
				return err
			}
			displayID = &d
		}
		var owner *contactRef
		if in.UserID != nil && *in.UserID != "" {
			contactID, err := contactIDByUserID(ctx, tx.DB, sv.WorkspaceID, *in.UserID)
			if err != nil {
				return err
			}
			if contactID != nil {
				attrs, err := contactAttributes(ctx, tx.DB, *contactID)
				if err != nil {
					return err
				}
				owner = &contactRef{ID: *contactID, Attributes: attrs}
			}
		}
		row := Response{SurveyID: sv.ID, DisplayID: displayID, ContactAttributes: json.RawMessage("null"), Finished: in.Finished, EndingID: in.EndingID, Tags: []tag.Tag{}, SingleUseID: in.SingleUseID, Language: language}
		var err error
		if row.Data, err = json.Marshal(data); err != nil {
			return fmt.Errorf("encode response data: %w", err)
		}
		if row.Variables, err = json.Marshal(variables); err != nil {
			return fmt.Errorf("encode response variables: %w", err)
		}
		if row.TTC, err = json.Marshal(ttc); err != nil {
			return fmt.Errorf("encode response ttc: %w", err)
		}
		if row.Meta, err = json.Marshal(meta); err != nil {
			return fmt.Errorf("encode response meta: %w", err)
		}
		var contactID *uuid.UUID
		var attributes any
		if owner != nil {
			contactID = &owner.ID
			row.Contact = owner.apiContact()
			if row.ContactAttributes, err = json.Marshal(owner.Attributes); err != nil {
				return fmt.Errorf("encode contact attributes: %w", err)
			}
			attributes = string(row.ContactAttributes)
		}
		var out struct {
			ID        uuid.UUID
			CreatedAt time.Time
			UpdatedAt time.Time
		}
		err = tx.WithContext(ctx).Raw(`INSERT INTO responses (survey_id, contact_id, display_id, finished, ending_id, data, variables, ttc, meta, contact_attributes, single_use_id, language, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?, ?, COALESCE(?, now()), COALESCE(?, now())) RETURNING id, created_at, updated_at`,
			sv.ID, contactID, displayID, in.Finished, in.EndingID, string(row.Data), string(row.Variables), string(row.TTC), string(row.Meta), attributes, in.SingleUseID, language, in.CreatedAt, updatedAt,
		).Scan(&out).Error
		if err != nil {
			return createError(err, in.DisplayID)
		}
		row.ID, row.CreatedAt, row.UpdatedAt = out.ID, out.CreatedAt, out.UpdatedAt
		id = row.ID

		quotaLanguage := "default"
		if language != nil {
			quotaLanguage = *language
		}
		full := s.evaluateQuotas(ctx, tx.DB, sv, screening{
			ResponseID: row.ID, Finished: row.Finished, Data: data, Variables: variables, Language: quotaLanguage,
			Row: valueResponse(row, data, variables, ttc, meta),
		})
		// forma2's management routes sent the pipeline the response as the quota left it.
		if full.endedResponse() {
			refreshed, _, err := loadResponse(ctx, tx.DB, row.ID)
			if err != nil {
				return err
			}
			row = *refreshed
		}
		events := []string{EventResponseCreated}
		if row.Finished {
			events = append(events, EventResponseFinished)
		}
		for _, event := range events {
			if err := tx.Enqueue(ctx, PipelineArgs{Event: event, WorkspaceID: sv.WorkspaceID, SurveyID: sv.ID, Response: row}, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, managedError(err)
	}
	return Load(ctx, s.db, id)
}

// ReplaceInput is a v2 response update: every column the management API lets a caller set, replacing what is stored.
type ReplaceInput struct {
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Finished          bool
	ContactID         *string
	EndingID          *string
	Data              map[string]any
	Variables         map[string]any
	TTC               map[string]float64
	Meta              map[string]any
	ContactAttributes map[string]string
	SingleUseID       *string
	Language          *string
	DisplayID         *string
}

// ReplaceManaged is forma2's v2 updateResponseWithQuotaEvaluation: the contact must be one of the response's workspace and the display one of its survey that no other response answers (ErrContactNotFound, ErrDisplayNotFound, a malformed id counting as unknown); then the columns are replaced, the quotas evaluated and responseUpdated (plus responseFinished when finished) enqueued, in one transaction. A response that disappeared meanwhile is apperr's 404 Response.
func (s *Service) ReplaceManaged(ctx context.Context, sv *Survey, id uuid.UUID, in ReplaceInput) (*Response, error) {
	language := normalizeLanguage(in.Language)
	err := s.jobs.Transaction(ctx, s.db, func(tx *jobs.Tx) error {
		var locked []uuid.UUID
		if err := tx.WithContext(ctx).Raw(`SELECT id FROM responses WHERE id = ? FOR UPDATE`, id).Scan(&locked).Error; err != nil {
			return fmt.Errorf("lock response: %w", err)
		}
		if len(locked) == 0 {
			return apperr.NotFound("Response", id.String())
		}
		var contactID *uuid.UUID
		if in.ContactID != nil {
			cid, err := ResolveID(*in.ContactID)
			if err != nil {
				return ErrContactNotFound
			}
			ok, err := contactInWorkspace(ctx, tx.DB, sv.WorkspaceID, cid)
			if err != nil {
				return err
			}
			if !ok {
				return ErrContactNotFound
			}
			contactID = &cid
		}
		var displayID *uuid.UUID
		if in.DisplayID != nil {
			did, err := ResolveID(*in.DisplayID)
			if err != nil {
				return ErrDisplayNotFound
			}
			d, err := loadDisplayForResponse(ctx, tx.DB, did)
			if err != nil {
				return err
			}
			if d == nil || d.WorkspaceID != sv.WorkspaceID || d.SurveyID != sv.ID || (d.ResponseID != nil && *d.ResponseID != id) {
				return ErrDisplayNotFound
			}
			displayID = &did
		}
		data, variables, ttc, meta := orEmpty(in.Data), orEmpty(in.Variables), in.TTC, orEmpty(in.Meta)
		if ttc == nil {
			ttc = map[string]float64{}
		}
		encoded := map[string]string{}
		for name, v := range map[string]any{"data": data, "variables": variables, "ttc": ttc, "meta": meta} {
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("encode response %s: %w", name, err)
			}
			encoded[name] = string(b)
		}
		var attributes any
		if in.ContactAttributes != nil {
			b, err := json.Marshal(in.ContactAttributes)
			if err != nil {
				return fmt.Errorf("encode contact attributes: %w", err)
			}
			attributes = string(b)
		}
		err := tx.WithContext(ctx).Exec(`UPDATE responses SET created_at = ?, updated_at = ?, finished = ?, contact_id = ?, ending_id = ?, data = ?::jsonb, variables = ?::jsonb,
			ttc = ?::jsonb, meta = ?::jsonb, contact_attributes = ?::jsonb, single_use_id = ?, language = ?, display_id = ? WHERE id = ?`,
			in.CreatedAt, in.UpdatedAt, in.Finished, contactID, in.EndingID, encoded["data"], encoded["variables"], encoded["ttc"], encoded["meta"], attributes,
			in.SingleUseID, language, displayID, id).Error
		if err != nil {
			return createError(err, in.DisplayID)
		}
		row, _, err := loadResponse(ctx, tx.DB, id)
		if err != nil {
			return err
		}
		quotaLanguage := "default"
		if language != nil && *language != "" {
			quotaLanguage = *language
		}
		full := s.evaluateQuotas(ctx, tx.DB, sv, screening{
			ResponseID: id, Finished: in.Finished, Data: data, Variables: variables, Language: quotaLanguage,
			Row: valueResponse(*row, data, variables, ttc, meta),
		})
		// forma2's v2 PUT sent the pipeline the response as the quota left it.
		if full.endedResponse() {
			if row, _, err = loadResponse(ctx, tx.DB, id); err != nil {
				return err
			}
		}
		events := []string{EventResponseUpdated}
		if row.Finished {
			events = append(events, EventResponseFinished)
		}
		for _, event := range events {
			if err := tx.Enqueue(ctx, PipelineArgs{Event: event, WorkspaceID: sv.WorkspaceID, SurveyID: sv.ID, Response: *row}, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, managedError(err)
	}
	return Load(ctx, s.db, id)
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// DeleteManaged is the management APIs' deleteResponse: the response and its display go, quotas keep their counts, and the storage objects its file upload answers point at are deleted by a job enqueued in the same transaction. It returns the response as it was, nil when there was none.
func (s *Service) DeleteManaged(ctx context.Context, id uuid.UUID) (*Response, error) {
	var deleted *Response
	err := s.jobs.Transaction(ctx, s.db, func(tx *jobs.Tx) error {
		var locked []uuid.UUID
		if err := tx.WithContext(ctx).Raw(`SELECT id FROM responses WHERE id = ? FOR UPDATE`, id).Scan(&locked).Error; err != nil {
			return fmt.Errorf("lock response: %w", err)
		}
		if len(locked) == 0 {
			return nil
		}
		r, _, err := loadResponse(ctx, tx.DB, id)
		if err != nil || r == nil {
			return err
		}
		if err := tx.WithContext(ctx).Exec(`DELETE FROM responses WHERE id = ?`, id).Error; err != nil {
			return fmt.Errorf("delete response: %w", err)
		}
		if r.DisplayID != nil {
			if err := tx.WithContext(ctx).Exec(`DELETE FROM displays WHERE id = ?`, *r.DisplayID).Error; err != nil {
				return fmt.Errorf("delete display: %w", err)
			}
		}
		if err := s.enqueueFileCleanup(ctx, tx, r); err != nil {
			return err
		}
		deleted = r
		return nil
	})
	return deleted, err
}

// organizationExists is forma2's getOrganizationByWorkspaceId check on a management create.
func organizationExists(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID) error {
	var n int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM workspaces w JOIN organizations o ON o.id = w.organization_id WHERE w.id = ?`, workspaceID).Scan(&n).Error; err != nil {
		return fmt.Errorf("load workspace organization: %w", err)
	}
	if n == 0 {
		return ErrOrganizationNotFound
	}
	return nil
}

// displayOfSurvey is forma2's management display check: the display must exist and belong to the survey; anything else is one uniform ErrDisplayNotFound, so a display of another tenant cannot be told from a missing one.
func displayOfSurvey(ctx context.Context, db *gorm.DB, raw string, surveyID uuid.UUID) (uuid.UUID, error) {
	id, err := ResolveID(raw)
	if err != nil {
		return uuid.Nil, ErrDisplayNotFound
	}
	d, err := loadDisplayForResponse(ctx, db, id)
	if err != nil {
		return uuid.Nil, err
	}
	if d == nil || d.SurveyID != surveyID {
		return uuid.Nil, ErrDisplayNotFound
	}
	return id, nil
}

// managedError keeps the errors a management write reports and wraps the rest.
func managedError(err error) error {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae
	}
	for _, known := range []error{ErrDisplayNotFound, ErrContactNotFound, ErrOrganizationNotFound} {
		if errors.Is(err, known) {
			return known
		}
	}
	return fmt.Errorf("write response: %w", err)
}

// ManagedQuery selects the responses a management list shows: those of surveys in WorkspaceIDs, optionally of one survey or contact and within a date range, ordered by OrderBy ("created_at" or "updated_at", newest first unless Ascending, then by id the same way), Limit of them after Offset (no bound when zero).
type ManagedQuery struct {
	WorkspaceIDs []uuid.UUID
	SurveyID     *uuid.UUID
	ContactID    *uuid.UUID
	// DateField is the column Start and End bound: "created_at" or "updated_at".
	DateField string
	Start     *time.Time
	End       *time.Time
	OrderBy   string
	Ascending bool
	Limit     int
	Offset    int
}

func (q ManagedQuery) where() (string, []any) {
	where := `WHERE r.survey_id IN (SELECT id FROM surveys WHERE workspace_id = ANY(?::uuid[]))`
	ids := make([]string, len(q.WorkspaceIDs))
	for i, id := range q.WorkspaceIDs {
		ids[i] = id.String()
	}
	args := []any{"{" + strings.Join(ids, ",") + "}"}
	if q.SurveyID != nil {
		where += ` AND r.survey_id = ?`
		args = append(args, *q.SurveyID)
	}
	if q.ContactID != nil {
		where += ` AND r.contact_id = ?`
		args = append(args, *q.ContactID)
	}
	field := "r.created_at"
	if q.DateField == "updated_at" {
		field = "r.updated_at"
	}
	if q.Start != nil {
		where += ` AND ` + field + ` >= ?`
		args = append(args, *q.Start)
	}
	if q.End != nil {
		where += ` AND ` + field + ` <= ?`
		args = append(args, *q.End)
	}
	return where, args
}

// ListManaged reads the responses q selects, each with its contact and tags.
func ListManaged(ctx context.Context, db *gorm.DB, q ManagedQuery) ([]Response, error) {
	if len(q.WorkspaceIDs) == 0 {
		return []Response{}, nil
	}
	where, args := q.where()
	order := "r.created_at"
	if q.OrderBy == "updated_at" {
		order = "r.updated_at"
	}
	direction := " DESC"
	if q.Ascending {
		direction = " ASC"
	}
	query := responseSelect + where + ` ORDER BY ` + order + direction + `, r.id` + direction
	if q.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, q.Limit)
	}
	if q.Offset > 0 {
		query += ` OFFSET ?`
		args = append(args, q.Offset)
	}
	var rows []responseRow
	if err := db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list responses: %w", err)
	}
	out := make([]Response, len(rows))
	for i, row := range rows {
		out[i] = row.response()
	}
	if err := loadTags(ctx, db, out); err != nil {
		return nil, err
	}
	return out, nil
}

// CountManaged counts the responses q selects, ignoring its order and page.
func CountManaged(ctx context.Context, db *gorm.DB, q ManagedQuery) (int64, error) {
	if len(q.WorkspaceIDs) == 0 {
		return 0, nil
	}
	where, args := q.where()
	var n int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM responses r `+where, args...).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count responses: %w", err)
	}
	return n, nil
}
