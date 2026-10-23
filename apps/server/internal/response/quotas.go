package response

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveylogic"
)

// Quota actions and link statuses.
const (
	actionEndSurvey   = "endSurvey"
	statusScreenedIn  = "screenedIn"
	statusScreenedOut = "screenedOut"
)

type quota struct {
	ID                      uuid.UUID
	Limit                   int
	Logic                   []byte
	Action                  string
	EndingCardID            *string
	CountPartialSubmissions bool
}

// endedResponse reports whether the quota's endSurvey action finished the response it screened.
func (q *quota) endedResponse() bool { return q != nil && q.Action == actionEndSurvey }

func (q *quota) full() QuotaFull {
	if q == nil {
		return QuotaFull{}
	}
	f := &FullQuota{ID: q.ID, Action: q.Action}
	if q.EndingCardID != nil {
		f.EndingCardID = *q.EndingCardID
	}
	return QuotaFull{QuotaFull: true, Quota: f}
}

func ids(qs []quota) []uuid.UUID {
	out := make([]uuid.UUID, len(qs))
	for i, q := range qs {
		out[i] = q.ID
	}
	return out
}

// screening is what a quota evaluation reads of the response it screens.
type screening struct {
	ResponseID uuid.UUID
	Finished   bool
	Data       map[string]any
	Variables  map[string]any
	// Language is the response's language, "default" when it has none.
	Language string
	// Row is the persisted response, for the reserved operands (country, finished, ...).
	Row embeddeddata.ValueResponse
}

// evaluateQuotas is forma2's evaluateResponseQuotas inside the transaction that wrote the response: the survey's quotas screen it, and the first full quota is returned. A quota failure never fails the write: it runs in a savepoint, is rolled back to it and logged, and nothing is screened, as forma2 caught and logged every error.
func (s *Service) evaluateQuotas(ctx context.Context, tx *gorm.DB, sv *Survey, in screening) *quota {
	var full *quota
	err := tx.Transaction(func(sp *gorm.DB) error {
		var err error
		full, err = screen(ctx, sp, sv, in)
		return err
	})
	if err != nil {
		s.log.ErrorContext(ctx, "Error evaluating quotas for response", "response_id", in.ResponseID, "survey_id", sv.ID, "err", err)
		return nil
	}
	return full
}

// ScreenQuotas is forma2's createResponseWithQuotaEvaluation on a caller's transaction, for a response another module wrote in it (the AI example responses): the survey's quotas screen the response as it is stored, through evaluateQuotas and its savepoint, so a quota failure is logged and never fails the caller's write.
func (s *Service) ScreenQuotas(ctx context.Context, tx *gorm.DB, surveyID, responseID uuid.UUID) error {
	sv, err := LoadSurvey(ctx, tx, surveyID)
	if err != nil {
		return err
	}
	if sv == nil {
		return fmt.Errorf("screen quotas: survey %s not found", surveyID)
	}
	row, _, err := loadResponse(ctx, tx, responseID)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("screen quotas: response %s not found", responseID)
	}
	data, err := decodeObject(row.Data)
	if err != nil {
		return err
	}
	variables, err := decodeObject(row.Variables)
	if err != nil {
		return err
	}
	meta, err := decodeObject(row.Meta)
	if err != nil {
		return err
	}
	ttc, err := decodeTTC(row.TTC)
	if err != nil {
		return err
	}
	language := "default"
	if row.Language != nil && *row.Language != "" {
		language = *row.Language
	}
	s.evaluateQuotas(ctx, tx, sv, screening{
		ResponseID: row.ID, Finished: row.Finished, Data: data, Variables: variables, Language: language,
		Row: valueResponse(*row, data, variables, ttc, meta),
	})
	return nil
}

func screen(ctx context.Context, tx *gorm.DB, sv *Survey, in screening) (*quota, error) {
	var quotas []quota
	if err := tx.WithContext(ctx).Raw(`SELECT id, "limit", logic, action, ending_card_id, count_partial_submissions
		FROM survey_quotas WHERE survey_id = ? ORDER BY created_at DESC, id DESC`, sv.ID).Scan(&quotas).Error; err != nil {
		return nil, fmt.Errorf("load quotas: %w", err)
	}
	if len(quotas) == 0 {
		return nil, nil
	}
	language := in.Language
	if language == sv.DefaultLanguage {
		language = "default"
	}
	engine := surveylogic.NewSurvey(sv.blocks, sv.Fields)
	input := surveylogic.Input{
		Data: in.Data, Variables: in.Variables, Language: language,
		Reserved: embeddeddata.ServerValues(in.Row, sv.Fields, sv.ElementIDs()),
	}
	var passed, failed []quota
	for _, q := range quotas {
		var group surveydoc.Condition
		if err := json.Unmarshal(q.Logic, &group); err != nil {
			return nil, fmt.Errorf("decode logic of quota %s: %w", q.ID, err)
		}
		if surveylogic.Evaluate(engine, input, group) {
			passed = append(passed, q)
		} else {
			failed = append(failed, q)
		}
	}
	return handleQuotas(ctx, tx, in, passed, failed)
}

// handleQuotas is forma2's handleQuotas: count what each matched quota already holds and screen the response in or out of it.
func handleQuotas(ctx context.Context, tx *gorm.DB, in screening, passed, failed []quota) (*quota, error) {
	db := tx.WithContext(ctx)
	counts := map[uuid.UUID]int{}
	if len(passed) > 0 {
		// Admission is read-count-insert: locking the candidate quotas makes a concurrent response matching the same quota at limit-1 wait for this one, and ordering the locks by id keeps two of them from deadlocking.
		var locked []uuid.UUID
		if err := db.Raw(`SELECT id FROM survey_quotas WHERE id IN ? ORDER BY id FOR UPDATE`, ids(passed)).Scan(&locked).Error; err != nil {
			return nil, fmt.Errorf("lock quotas: %w", err)
		}
		var rows []struct {
			QuotaID uuid.UUID
			Count   int
		}
		if err := db.Raw(`SELECT l.quota_id, count(*) AS count FROM response_quota_links l
			JOIN survey_quotas q ON q.id = l.quota_id JOIN responses r ON r.id = l.response_id
			WHERE l.quota_id IN ? AND l.status = ? AND l.response_id <> ? AND (q.count_partial_submissions OR r.finished)
			GROUP BY l.quota_id`, ids(passed), statusScreenedIn, in.ResponseID).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("count quota links: %w", err)
		}
		for _, r := range rows {
			counts[r.QuotaID] = r.Count
		}
	}

	var first *quota
	var fullQuotas, other []quota
	for i := range passed {
		if counts[passed[i].ID] >= passed[i].Limit {
			if first == nil {
				first = &passed[i]
			}
			fullQuotas = append(fullQuotas, passed[i])
		} else {
			other = append(other, passed[i])
		}
	}
	// A partial response only counts towards quotas that count partial submissions, unless a full quota ends it here.
	if !in.Finished && (first == nil || first.Action != actionEndSurvey) {
		fullQuotas, other = countingPartial(fullQuotas), countingPartial(other)
	}

	if len(failed) > 0 {
		if err := db.Exec(`DELETE FROM response_quota_links WHERE response_id = ? AND quota_id IN ?`, in.ResponseID, ids(failed)).Error; err != nil {
			return nil, fmt.Errorf("unlink failed quotas: %w", err)
		}
	}
	if len(fullQuotas) > 0 {
		if err := db.Exec(`INSERT INTO response_quota_links (response_id, quota_id, status) SELECT ?, unnest(?::uuid[]), ? ON CONFLICT DO NOTHING`,
			in.ResponseID, uuidArray(ids(fullQuotas)), statusScreenedOut).Error; err != nil {
			return nil, fmt.Errorf("screen out: %w", err)
		}
		if err := db.Exec(`UPDATE response_quota_links SET status = ? WHERE response_id = ? AND quota_id IN ? AND status <> ?`,
			statusScreenedOut, in.ResponseID, ids(fullQuotas), statusScreenedOut).Error; err != nil {
			return nil, fmt.Errorf("screen out: %w", err)
		}
	}
	if len(other) > 0 {
		// An existing screenedOut link is not flipped back: the response stays out of a quota it was once turned away from.
		if err := db.Exec(`INSERT INTO response_quota_links (response_id, quota_id, status) SELECT ?, unnest(?::uuid[]), ? ON CONFLICT DO NOTHING`,
			in.ResponseID, uuidArray(ids(other)), statusScreenedIn).Error; err != nil {
			return nil, fmt.Errorf("screen in: %w", err)
		}
	}

	if first != nil && first.Action == actionEndSurvey {
		if err := endResponse(ctx, tx, in.ResponseID, first.EndingCardID); err != nil {
			return nil, err
		}
	}
	return first, nil
}

func countingPartial(qs []quota) []quota {
	var out []quota
	for _, q := range qs {
		if q.CountPartialSubmissions {
			out = append(out, q)
		}
	}
	return out
}

func uuidArray(ids []uuid.UUID) string {
	b := []byte{'{'}
	for i, id := range ids {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, id.String()...)
	}
	return string(append(b, '}'))
}

// endResponse is the update a quota's endSurvey action makes, forma2's updateResponse(id, {finished: true, endingId}): the response is finished on the quota's ending and its time to complete gains its total.
func endResponse(ctx context.Context, tx *gorm.DB, responseID uuid.UUID, endingID *string) error {
	var ttcRaw []byte
	if err := tx.WithContext(ctx).Raw(`SELECT ttc FROM responses WHERE id = ? FOR UPDATE`, responseID).Row().Scan(&ttcRaw); err != nil {
		return fmt.Errorf("load response ttc: %w", err)
	}
	ttc, err := decodeTTC(ttcRaw)
	if err != nil {
		return err
	}
	ttcJSON, err := jsonParam(ttcTotal(ttc))
	if err != nil {
		return err
	}
	return tx.WithContext(ctx).Exec(`UPDATE responses SET finished = true, ending_id = ?, ttc = ?::jsonb, updated_at = ? WHERE id = ?`,
		endingID, ttcJSON, time.Now().UTC(), responseID).Error
}

// decodeTTC reads a stored ttc object. Only numbers are kept: every writer stores milliseconds, and a value that is not one cannot take part in a total.
func decodeTTC(raw []byte) (map[string]float64, error) {
	out := map[string]float64{}
	if len(raw) == 0 {
		return out, nil
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode response ttc: %w", err)
	}
	for k, v := range values {
		if n, ok := v.(float64); ok {
			out[k] = n
		}
	}
	return out, nil
}
