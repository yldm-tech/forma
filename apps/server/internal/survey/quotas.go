package survey

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// quotaWrite is the columns a quota save writes.
type quotaWrite struct {
	Name                    string
	Limit                   int
	Logic                   json.RawMessage
	Action                  string
	EndingCardID            *string
	CountPartialSubmissions bool
}

const quotaColumns = `id, created_at, updated_at, survey_id, name, "limit", logic, action, ending_card_id, count_partial_submissions`

type quotaRow struct {
	ID                      uuid.UUID
	CreatedAt               time.Time
	UpdatedAt               time.Time
	SurveyID                uuid.UUID
	Name                    string
	Limit                   int
	Logic                   []byte
	Action                  string
	EndingCardID            *string
	CountPartialSubmissions bool
}

func (q quotaRow) quota() Quota {
	return Quota{ID: q.ID, CreatedAt: q.CreatedAt, UpdatedAt: q.UpdatedAt, SurveyID: q.SurveyID, Name: q.Name, Limit: q.Limit, Logic: rawOrNull(q.Logic), Action: q.Action, EndingCardID: q.EndingCardID, CountPartialSubmissions: q.CountPartialSubmissions}
}

// quotas lists a survey's quotas, newest first, as forma2's getQuotas did.
func (r *Repository) quotas(ctx context.Context, surveyID uuid.UUID) ([]Quota, error) {
	var rows []quotaRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+quotaColumns+` FROM survey_quotas WHERE survey_id = ? ORDER BY created_at DESC, id DESC`, surveyID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load quotas: %w", err)
	}
	out := make([]Quota, len(rows))
	for i, q := range rows {
		out[i] = q.quota()
	}
	return out, nil
}

func (r *Repository) quota(ctx context.Context, id uuid.UUID) (*Quota, error) {
	var rows []quotaRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+quotaColumns+` FROM survey_quotas WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load quota: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	q := rows[0].quota()
	return &q, nil
}

// errQuotaNameTaken is a quota name the survey already uses.
var errQuotaNameTaken = apperr.InvalidInput(apperr.CodeBadRequest, "Quota with this name already exists")

func (r *Repository) insertQuota(ctx context.Context, surveyID uuid.UUID, q quotaWrite) (*Quota, error) {
	var rows []quotaRow
	err := r.db.WithContext(ctx).Raw(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action, ending_card_id, count_partial_submissions)
		VALUES (?, ?, ?, ?::jsonb, ?, ?, ?) RETURNING `+quotaColumns, surveyID, q.Name, q.Limit, string(q.Logic), q.Action, q.EndingCardID, q.CountPartialSubmissions).Scan(&rows).Error
	if isUniqueViolation(err, "survey_quotas_survey_id_name_key") {
		return nil, errQuotaNameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create quota: %w", err)
	}
	out := rows[0].quota()
	return &out, nil
}

func (r *Repository) updateQuota(ctx context.Context, id uuid.UUID, q quotaWrite) (*Quota, error) {
	var rows []quotaRow
	err := r.db.WithContext(ctx).Raw(`UPDATE survey_quotas SET name = ?, "limit" = ?, logic = ?::jsonb, action = ?, ending_card_id = ?, count_partial_submissions = ?, updated_at = now()
		WHERE id = ? RETURNING `+quotaColumns, q.Name, q.Limit, string(q.Logic), q.Action, q.EndingCardID, q.CountPartialSubmissions, id).Scan(&rows).Error
	if isUniqueViolation(err, "survey_quotas_survey_id_name_key") {
		return nil, errQuotaNameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("update quota: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := rows[0].quota()
	return &out, nil
}

// QuotaInput is a quota definition as the editor sends it, forma2's ZSurveyQuotaInput without its surveyId.
type QuotaInput = surveydoc.Quota

func (s *Service) quotaWrite(in QuotaInput) (quotaWrite, error) {
	issues, err := s.docs.QuotaIssues(in)
	if err != nil {
		return quotaWrite{}, err
	}
	if len(issues) > 0 {
		return quotaWrite{}, ValidationError(issues)
	}
	ending := in.EndingCardID
	if ending != nil && *ending == "" {
		ending = nil
	}
	return quotaWrite{Name: in.Name, Limit: int(in.Limit), Logic: in.Logic, Action: in.Action, EndingCardID: ending, CountPartialSubmissions: in.CountPartialSubmissions}, nil
}

// Quotas lists a survey's quota definitions behind workspace.write, as forma2's quota actions gated every read and write.
func (s *Service) Quotas(ctx context.Context, caller Caller, surveyID uuid.UUID) ([]Quota, error) {
	if _, err := s.authorized(ctx, caller, surveyID, authz.SurveyWrite); err != nil {
		return nil, err
	}
	return s.repo.quotas(ctx, surveyID)
}

// CreateQuota is forma2's createQuotaAction: names are unique per survey.
func (s *Service) CreateQuota(ctx context.Context, caller Caller, surveyID uuid.UUID, in QuotaInput) (*Quota, error) {
	sv, err := s.authorized(ctx, caller, surveyID, authz.SurveyWrite)
	if err != nil {
		return nil, err
	}
	w, err := s.quotaWrite(in)
	if err != nil {
		return nil, err
	}
	q, err := s.repo.insertQuota(ctx, surveyID, w)
	id := ""
	if q != nil {
		id = q.ID.String()
	}
	s.record(ctx, caller, audit.ActionCreated, audit.TargetQuota, id, s.organizationOf(ctx, sv.WorkspaceID), err, nil, q)
	return q, err
}

// authorizedQuota loads a quota and checks the caller may write its survey's workspace.
func (s *Service) authorizedQuota(ctx context.Context, caller Caller, id uuid.UUID) (*Quota, *Survey, error) {
	q, err := s.repo.quota(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if q == nil {
		return nil, nil, apperr.NotFound("Quota", "")
	}
	sv, err := s.authorized(ctx, caller, q.SurveyID, authz.SurveyWrite)
	if err != nil {
		return nil, nil, err
	}
	return q, sv, nil
}

// UpdateQuota is forma2's updateQuotaAction: the quota stays on its survey whatever the input says.
func (s *Service) UpdateQuota(ctx context.Context, caller Caller, id uuid.UUID, in QuotaInput) (*Quota, error) {
	old, sv, err := s.authorizedQuota(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	w, err := s.quotaWrite(in)
	if err != nil {
		return nil, err
	}
	q, err := s.repo.updateQuota(ctx, id, w)
	if err == nil && q == nil {
		err = apperr.NotFound("Quota", "")
	}
	s.record(ctx, caller, audit.ActionUpdated, audit.TargetQuota, id.String(), s.organizationOf(ctx, sv.WorkspaceID), err, old, q)
	return q, err
}

// DeleteQuota is forma2's deleteQuotaAction.
func (s *Service) DeleteQuota(ctx context.Context, caller Caller, id uuid.UUID) error {
	old, sv, err := s.authorizedQuota(ctx, caller, id)
	if err != nil {
		return err
	}
	n, err := s.repo.exec(ctx, `DELETE FROM survey_quotas WHERE id = ?`, id)
	if err == nil && n == 0 {
		err = apperr.NotFound("Quota", "")
	}
	if err != nil && !isAppErr(err) {
		err = fmt.Errorf("delete quota: %w", err)
	}
	s.record(ctx, caller, audit.ActionDeleted, audit.TargetQuota, id.String(), s.organizationOf(ctx, sv.WorkspaceID), err, old, nil)
	return err
}

// QuotaResponseCount is forma2's getQuotaResponseCountAction: the responses screened into the quota.
func (s *Service) QuotaResponseCount(ctx context.Context, caller Caller, id uuid.UUID) (int, error) {
	if _, _, err := s.authorizedQuota(ctx, caller, id); err != nil {
		return 0, err
	}
	var n int
	if err := s.repo.db.WithContext(ctx).Raw(`SELECT count(*) FROM response_quota_links WHERE quota_id = ? AND status = 'screenedIn'`, id).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count quota responses: %w", err)
	}
	return n, nil
}

func isAppErr(err error) bool {
	_, ok := apperr.As(err)
	return ok
}
