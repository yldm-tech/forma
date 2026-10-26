package response

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// whereOf compiles a filter given as JSON.
func whereOf(t *testing.T, sv *Survey, filter string) Clause {
	t.Helper()
	var f Criteria
	if err := json.Unmarshal([]byte(filter), &f); err != nil {
		t.Fatal(err)
	}
	clause, err := Where(t.Context(), nil, sv, &f)
	if err != nil {
		t.Fatal(err)
	}
	return clause
}

// expectDataIndex checks that, with sequential scans off, the planner reaches the rows clause matches through the GIN index on responses.data.
func expectDataIndex(t *testing.T, e *env, filter string, clause Clause) {
	t.Helper()
	var plan []string
	err := e.db.Transaction(func(tx *gorm.DB) error {
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
		expectDataIndex(t, e, filter, whereOf(t, sv, filter))
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
