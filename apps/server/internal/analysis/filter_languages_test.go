package analysis

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
)

// languageFilterBlocks asks for the same choices and matrix cells in English (the default), German, Japanese and Simplified Chinese.
const languageFilterBlocks = `[{"id":"b1","name":"B1","elements":[
	{"id":"ml_best","type":"multipleChoiceSingle","headline":{"default":"Best"},"required":false,"choices":[
		{"id":"speed","label":{"default":"Speed","de":"Geschwindigkeit","ja":"速さ","zh-Hans":"速度"}},
		{"id":"price","label":{"default":"Price","de":"Preis","ja":"価格","zh-Hans":"价格"}}]},
	{"id":"ml_multi","type":"multipleChoiceMulti","headline":{"default":"All"},"required":false,"choices":[
		{"id":"speed","label":{"default":"Speed","de":"Geschwindigkeit","ja":"速さ","zh-Hans":"速度"}},
		{"id":"price","label":{"default":"Price","de":"Preis","ja":"価格","zh-Hans":"价格"}},
		{"id":"support","label":{"default":"Support","de":"Support","ja":"サポート","zh-Hans":"支持"}}]},
	{"id":"ml_mx","type":"matrix","headline":{"default":"Rate"},"required":false,
		"rows":[{"id":"r1","label":{"default":"Speed","de":"Tempo","ja":"速さ","zh-Hans":"速度"}},{"id":"r2","label":{"default":"Price","de":"Preis","ja":"価格","zh-Hans":"价格"}}],
		"columns":[{"id":"c1","label":{"default":"Good","de":"Gut","ja":"良い","zh-Hans":"好"}},{"id":"c2","label":{"default":"Bad","de":"Schlecht","ja":"悪い","zh-Hans":"差"}}]}
]}]`

// A choice or matrix cell the summary counts, filtered on, keeps exactly the responses the summary counted under it, whatever language they were answered in.
func TestFilteredCountsMatchTheSummaryInEveryLanguage(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{blocks: languageFilterBlocks})
	for _, l := range []struct {
		code      string
		isDefault bool
	}{{"en", true}, {"de", false}, {"ja", false}, {"zh-Hans", false}} {
		id := e.insert(`INSERT INTO languages (workspace_id, code) VALUES (?, ?) RETURNING id`, ws, l.code)
		e.exec(`INSERT INTO survey_languages (survey_id, language_id, is_default) VALUES (?, ?, ?)`, sv, id, l.isDefault)
	}
	for i, r := range []struct {
		language *string
		data     string
	}{
		{ptr("en"), `{"ml_best":"Speed","ml_multi":["Speed","Support"],"ml_mx":{"Speed":"Good","Price":"Bad"}}`},
		{nil, `{"ml_best":"Price","ml_multi":["Price"],"ml_mx":{"Speed":"Bad"}}`},
		{ptr("de"), `{"ml_best":"Geschwindigkeit","ml_multi":["Preis","Geschwindigkeit"],"ml_mx":{"Tempo":"Gut","Preis":"Gut"}}`},
		{ptr("de"), `{"ml_best":"Geschwindigkeit","ml_multi":["Support"],"ml_mx":{"Tempo":"Schlecht"}}`},
		{ptr("ja"), `{"ml_best":"速さ","ml_multi":["サポート","速さ"],"ml_mx":{"速さ":"良い"}}`},
		{ptr("ja"), `{"ml_best":"価格","ml_multi":["価格"],"ml_mx":{"価格":"悪い"}}`},
		{ptr("zh-Hans"), `{"ml_best":"速度","ml_multi":["速度","价格","支持"],"ml_mx":{"速度":"好","价格":"差"}}`},
	} {
		e.response(sv, row{at: at(i), finished: true, language: r.language, data: r.data})
	}

	type summary struct {
		Meta    Meta              `json:"meta"`
		Summary []json.RawMessage `json:"summary"`
	}
	read := func(filter map[string]any) summary {
		t.Helper()
		var s summary
		decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", map[string]any{"filterCriteria": filter}), &s)
		if len(s.Summary) != 3 {
			t.Fatalf("summary has %d entries", len(s.Summary))
		}
		return s
	}
	filtered := func(element string, condition map[string]any) int {
		t.Helper()
		return read(map[string]any{"data": map[string]any{element: condition}}).Meta.TotalResponses
	}
	all := read(map[string]any{})

	for i, element := range []string{"ml_best", "ml_multi"} {
		var choices struct {
			Choices []struct {
				Value string `json:"value"`
				Count int    `json:"count"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(all.Summary[i], &choices); err != nil {
			t.Fatal(err)
		}
		for _, c := range choices.Choices {
			if c.Count == 0 {
				t.Errorf("%s %s: the fixture answers every choice", element, c.Value)
			}
			if got := filtered(element, map[string]any{"op": "includesOne", "value": []string{c.Value}}); got != c.Count {
				t.Errorf("%s includesOne %s: %d responses, the summary counts %d", element, c.Value, got, c.Count)
			}
		}
	}
	// Speed and Price together: the German and the Simplified Chinese answer.
	if got := filtered("ml_multi", map[string]any{"op": "includesAll", "value": []string{"Speed", "Price"}}); got != 2 {
		t.Errorf("ml_multi includesAll Speed, Price: %d responses, want 2", got)
	}

	var matrix MatrixSummary
	if err := json.Unmarshal(all.Summary[2], &matrix); err != nil {
		t.Fatal(err)
	}
	for _, r := range matrix.Data {
		for _, c := range r.ColumnPercentages {
			count := int(math.Round(c.Percentage * float64(r.TotalResponsesForRow) / 100))
			if count == 0 {
				t.Errorf("%s %s: the fixture answers every cell", r.RowLabel, c.Column)
			}
			if got := filtered("ml_mx", map[string]any{"op": "matrix", "value": map[string]string{r.RowLabel: c.Column}}); got != count {
				t.Errorf("matrix %s = %s: %d responses, the summary counts %d", r.RowLabel, c.Column, got, count)
			}
		}
	}
}
