package response

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// languageBlocks asks for the same choices and matrix cells in English (the default), German, Japanese and Simplified Chinese. German writes "Support" as English does.
const languageBlocks = `[{"id":"b1","name":"Block 1","elements":[
	{"id":"ml_best","type":"multipleChoiceSingle","headline":{"default":"Best"},"required":false,"choices":[
		{"id":"speed","label":{"default":"Speed","de":"Geschwindigkeit","ja":"速さ","zh-Hans":"速度"}},
		{"id":"price","label":{"default":"Price","de":"Preis","ja":"価格","zh-Hans":"价格"}},
		{"id":"other","label":{"default":"Other","de":"Sonstiges","ja":"その他","zh-Hans":"其他"}}]},
	{"id":"ml_multi","type":"multipleChoiceMulti","headline":{"default":"All"},"required":false,"choices":[
		{"id":"speed","label":{"default":"Speed","de":"Geschwindigkeit","ja":"速さ","zh-Hans":"速度"}},
		{"id":"price","label":{"default":"Price","de":"Preis","ja":"価格","zh-Hans":"价格"}},
		{"id":"support","label":{"default":"Support","de":"Support","ja":"サポート","zh-Hans":"支持"}},
		{"id":"other","label":{"default":"Other","de":"Sonstiges","ja":"その他","zh-Hans":"其他"}}]},
	{"id":"ml_mx","type":"matrix","headline":{"default":"Rate"},"required":false,
		"rows":[{"id":"r1","label":{"default":"Speed","de":"Tempo","ja":"速さ","zh-Hans":"速度"}},{"id":"r2","label":{"default":"Price","de":"Preis","ja":"価格","zh-Hans":"价格"}}],
		"columns":[{"id":"c1","label":{"default":"Good","de":"Gut","ja":"良い","zh-Hans":"好"}},{"id":"c2","label":{"default":"Bad","de":"Schlecht","ja":"悪い","zh-Hans":"差"}}]}
]}]`

// languageSurvey is a survey asking languageBlocks, with responses answered in each of its languages, keyed by a name for each.
func languageSurvey(t *testing.T, e *env, ws uuid.UUID) (uuid.UUID, map[string]uuid.UUID) {
	t.Helper()
	sv := e.survey(ws, surveyOpts{})
	e.exec(`UPDATE surveys SET blocks = ?::jsonb WHERE id = ?`, languageBlocks, sv)
	responses := map[string]uuid.UUID{}
	for _, r := range []struct {
		name     string
		language *string
		data     string
	}{
		{"en", nil, `{"ml_best":"Speed","ml_multi":["Speed","Support"],"ml_mx":{"Speed":"Good","Price":"Bad"}}`},
		{"de", ptr("de"), `{"ml_best":"Geschwindigkeit","ml_multi":["Preis","Geschwindigkeit"],"ml_mx":{"Tempo":"Gut"}}`},
		{"ja", ptr("ja"), `{"ml_best":"速さ","ml_multi":["サポート"],"ml_mx":{"速さ":"悪い"}}`},
		{"zh", ptr("zh-Hans"), `{"ml_best":"价格","ml_multi":["速度","价格"],"ml_mx":{"速度":"好"}}`},
		{"deOther", ptr("de"), `{"ml_best":"Etwas anderes","ml_multi":["Support","Mein Text"]}`},
		{"en2", nil, `{"ml_best":"Price","ml_mx":{"Speed":"Bad"}}`},
		{"mixed", ptr("de"), `{"ml_mx":{"Tempo":"Good"}}`},
	} {
		responses[r.name] = e.insert(`INSERT INTO responses (survey_id, finished, data, language) VALUES (?, true, ?::jsonb, ?) RETURNING id`, sv, r.data, r.language)
	}
	return sv, responses
}

// Responses hold choice and matrix labels in the language they were answered in, while the views filter by the default-language labels the summary counts under: a choice or cell matches in every language.
func TestAnswerFiltersMatchChoicesInEveryLanguage(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	sv, r := languageSurvey(t, e, ws)

	cases := []struct {
		name   string
		filter string
		want   []string
	}{
		{"single choice includesOne", `{"data":{"ml_best":{"op":"includesOne","value":["Speed"]}}}`, []string{"en", "de", "ja"}},
		{"single choice includesOne of two", `{"data":{"ml_best":{"op":"includesOne","value":["Speed","Price"]}}}`, []string{"en", "de", "ja", "zh", "en2"}},
		{"Other is an answer no label in any language covers", `{"data":{"ml_best":{"op":"includesOne","value":["Other"]}}}`, []string{"deOther"}},
		{"Other beside a choice keeps the choice in every language", `{"data":{"ml_best":{"op":"includesOne","value":["Speed","Other"]}}}`, []string{"en", "de", "ja", "deOther"}},
		{"a translated label names only itself", `{"data":{"ml_best":{"op":"includesOne","value":["Geschwindigkeit"]}}}`, []string{"de"}},
		{"multi choice includesOne", `{"data":{"ml_multi":{"op":"includesOne","value":["Price"]}}}`, []string{"de", "zh"}},
		{"multi choice includesAll is every choice, each in any language", `{"data":{"ml_multi":{"op":"includesAll","value":["Speed","Price"]}}}`, []string{"de", "zh"}},
		{"multi choice includesAll of a label two languages share", `{"data":{"ml_multi":{"op":"includesAll","value":["Support"]}}}`, []string{"en", "ja", "deOther"}},
		{"multi choice Other beside a choice", `{"data":{"ml_multi":{"op":"includesOne","value":["Support","Other"]}}}`, []string{"en", "ja", "deOther"}},
		// A German row label with the English column label ("mixed") is no cell of either language.
		{"matrix cell", `{"data":{"ml_mx":{"op":"matrix","value":{"Speed":"Good"}}}}`, []string{"en", "de", "zh"}},
		{"matrix cell of another column", `{"data":{"ml_mx":{"op":"matrix","value":{"Speed":"Bad"}}}}`, []string{"ja", "en2"}},
		{"matrix cell of another row", `{"data":{"ml_mx":{"op":"matrix","value":{"Price":"Bad"}}}}`, []string{"en"}},
		{"matrix row the element does not have", `{"data":{"ml_mx":{"op":"matrix","value":{"Nope":"Good"}}}}`, nil},
	}
	for _, c := range cases {
		var filter any
		if err := json.Unmarshal([]byte(c.filter), &filter); err != nil {
			t.Fatal(err)
		}
		rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/search", map[string]any{"filterCriteria": filter})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
		var got []string
		for _, id := range listedIDs(decodeList(t, rec.Body.Bytes())) {
			for name, rid := range r {
				if rid == id {
					got = append(got, name)
				}
			}
		}
		want := slices.Clone(c.want)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", c.name, got, want)
		}
		rec = e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/count", map[string]any{"filterCriteria": filter})
		var count countResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &count); err != nil || count.Count != int64(len(c.want)) {
			t.Errorf("%s: count %s, want %d", c.name, rec.Body, len(c.want))
		}
	}
}

// Expanded to every language, the answer filters stay containments the data index answers.
func TestTranslatedAnswerFiltersCanUseTheDataIndex(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	svID, _ := languageSurvey(t, e, ws)
	sv, err := LoadSurvey(t.Context(), e.db, svID)
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{
		`{"data":{"ml_best":{"op":"includesOne","value":["Speed","Price"]}}}`,
		`{"data":{"ml_multi":{"op":"includesAll","value":["Speed","Price"]}}}`,
		`{"data":{"ml_mx":{"op":"matrix","value":{"Speed":"Good"}}}}`,
	} {
		clause := whereOf(t, sv, filter)
		if !strings.Contains(clause.SQL, " OR ") {
			t.Errorf("%s is not expanded to the other languages: %s", filter, clause.SQL)
		}
		expectDataIndex(t, e, filter, clause)
	}
}
