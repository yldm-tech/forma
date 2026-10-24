package response

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// Answer filters are containments of responses.data, which the GIN index (jsonb_path_ops) answers: with sequential scans off, the planner reaches the matching rows through it.
func TestAnswerFiltersCanUseTheDataIndex(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	svID := e.survey(ws, surveyOpts{})
	sv, err := LoadSurvey(t.Context(), e.db, svID)
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{
		`{"data":{"q1":{"op":"equals","value":"b"}}}`,
		`{"data":{"f1":{"op":"includesAll","value":["https://files/1"]}}}`,
		`{"data":{"q2":{"op":"includesOne","value":["One","Two"]}}}`,
		`{"data":{"q1":{"op":"clicked"}}}`,
		`{"data":{"m":{"op":"matrix","value":{"Row":"Col"}}}}`,
	} {
		var f Criteria
		if err := json.Unmarshal([]byte(filter), &f); err != nil {
			t.Fatal(err)
		}
		clause, err := Where(sv, &f)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		err = e.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(`SET LOCAL enable_seqscan = off`).Error; err != nil {
				return err
			}
			return tx.Raw("EXPLAIN SELECT r.id FROM responses r WHERE "+clause.SQL, clause.Args...).Scan(&plan).Error
		})
		if err != nil {
			t.Fatal(err)
		}
		if text := strings.Join(plan, "\n"); !strings.Contains(text, "responses_data_idx") {
			t.Errorf("%s does not use the data index:\n%s", filter, text)
		}
	}
}

func TestListPagesAreBounded(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws, surveyOpts{})
	for limit, status := range map[int]int{500: http.StatusOK, 501: http.StatusBadRequest} {
		if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/search", map[string]any{"limit": limit}); rec.Code != status {
			t.Errorf("limit %d: %d %s", limit, rec.Code, rec.Body)
		}
	}
}
