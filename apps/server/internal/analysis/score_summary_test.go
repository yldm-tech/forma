package analysis

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

func scoreSummary(variables ...map[string]any) *ScoreSummary {
	sv := &surveyDoc{Survey: &response.Survey{}, ScoreVariable: &surveydoc.Variable{ID: "v", Name: "score", Type: "number", IsScore: true}}
	agg := newSummarizer(sv)
	for _, v := range variables {
		agg.add(&summaryResponse{Data: map[string]any{}, TTC: map[string]any{}, Variables: v})
	}
	return agg.result(0, []QuotaProgress{}).Score
}

func TestScoreSummaryHasABinPerScoreWhileThereAreFew(t *testing.T) {
	t.Parallel()
	got := scoreSummary(map[string]any{"v": 3.0}, map[string]any{"v": 1.0}, map[string]any{"v": 3.0}, map[string]any{"v": 2.5},
		// No score: the variable did not exist yet, or the value is not a number.
		map[string]any{}, map[string]any{"v": "3"}, nil)
	want := &ScoreSummary{VariableID: "v", Name: "score", ResponseCount: 4, Average: 2.38, Median: 2.75, Min: 1, Max: 3, Bins: []ScoreBin{
		{From: 1, To: 1, Count: 1, Percentage: 25},
		{From: 2.5, To: 2.5, Count: 1, Percentage: 25},
		{From: 3, To: 3, Count: 2, Percentage: 50},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("score summary %+v, want %+v", got, want)
	}
}

func TestScoreSummaryGroupsManyScoresIntoTenBins(t *testing.T) {
	t.Parallel()
	var responses []map[string]any
	for score := range 21 {
		responses = append(responses, map[string]any{"v": float64(score * 5)})
	}
	got := scoreSummary(responses...)
	if len(got.Bins) != 10 || got.Min != 0 || got.Max != 100 || got.Average != 50 || got.Median != 50 {
		t.Fatalf("score summary %+v", got)
	}
	if b := got.Bins[0]; b.From != 0 || b.To != 10 || b.Count != 2 {
		t.Fatalf("first bin %+v", b)
	}
	// The last bin holds the highest score.
	if b := got.Bins[9]; b.From != 90 || b.To != 100 || b.Count != 3 {
		t.Fatalf("last bin %+v", b)
	}
	total := 0
	for _, b := range got.Bins {
		total += b.Count
	}
	if total != 21 {
		t.Fatalf("bins count %d responses, want 21", total)
	}
}

func TestSummaryScoreIsNullWithoutAScoreVariable(t *testing.T) {
	t.Parallel()
	agg := newSummarizer(&surveyDoc{Survey: &response.Survey{}})
	agg.add(&summaryResponse{Data: map[string]any{}, TTC: map[string]any{}, Variables: map[string]any{"v": 1.0}})
	out, err := json.Marshal(agg.result(0, []QuotaProgress{}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if score, ok := got["score"]; !ok || score != nil {
		t.Fatalf("score %v (present %v), want null", score, ok)
	}
	empty := scoreSummary()
	if empty.ResponseCount != 0 || empty.Bins == nil || len(empty.Bins) != 0 {
		t.Fatalf("score summary without scored responses %+v", empty)
	}
}

func TestSummaryReportsTheTotalScoreOfTheResponses(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{blocks: summaryBlocks})
	e.exec(`UPDATE surveys SET variables = ?::jsonb WHERE id = ?`, `[{"id": "bonus", "name": "bonus", "type": "number", "value": 0}, {"id": "total", "name": "score", "type": "number", "value": 0, "isScore": true}]`, sv)
	// The survey save links each variable as a computed Embedded Data field, which the response filter reads.
	e.field(ws, sv, "bonus", "bonus", "computed", "number")
	e.field(ws, sv, "total", "score", "computed", "number")
	e.response(sv, row{at: at(1), finished: true, variables: `{"total": 4, "bonus": 100}`})
	e.response(sv, row{at: at(2), finished: true, variables: `{"total": 2}`})
	e.response(sv, row{at: at(3), variables: `{"bonus": 1}`})

	var s struct {
		Score *ScoreSummary `json:"score"`
	}
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", map[string]any{}), &s)
	want := &ScoreSummary{VariableID: "total", Name: "score", ResponseCount: 2, Average: 3, Median: 3, Min: 2, Max: 4, Bins: []ScoreBin{
		{From: 2, To: 2, Count: 1, Percentage: 50},
		{From: 4, To: 4, Count: 1, Percentage: 50},
	}}
	if !reflect.DeepEqual(s.Score, want) {
		t.Fatalf("score %+v, want %+v", s.Score, want)
	}

	// The response filter applies to the score as to every card.
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", map[string]any{"filterCriteria": map[string]any{"variables": map[string]any{"total": map[string]any{"op": "greaterThan", "value": 3}}}}), &s)
	if s.Score == nil || s.Score.ResponseCount != 1 || s.Score.Average != 4 {
		t.Fatalf("filtered score %+v", s.Score)
	}
}
