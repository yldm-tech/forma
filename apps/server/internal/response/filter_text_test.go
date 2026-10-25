package response

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// textBlocks asks an open text question, an address, a contact info, a rating and a choice, the answers the keyword search, the text operators and the ranges read.
const textBlocks = `[{"id":"b1","name":"Block 1","elements":[
	{"id":"improve","type":"openText","headline":{"default":"Improve"},"required":false,"inputType":"text"},
	{"id":"addr","type":"address","headline":{"default":"Address"},"required":false},
	{"id":"ci","type":"contactInfo","headline":{"default":"Contact"},"required":false},
	{"id":"rate","type":"rating","headline":{"default":"Rate"},"required":false,"range":10,"scale":"number"},
	{"id":"pick","type":"multipleChoiceSingle","headline":{"default":"Pick"},"required":false,"choices":[
		{"id":"c1","label":{"default":"Onboarding"}},{"id":"c2","label":{"default":"Pricing"}}]}
]}]`

// textSurvey is a survey asking textBlocks, with responses keyed by a name for each. The contact attribute snapshots hold text, as responses written today do, and a JSON number, as some imported from forma2 do.
func textSurvey(t *testing.T, e *env, ws uuid.UUID) (uuid.UUID, map[string]uuid.UUID) {
	t.Helper()
	sv := e.survey(ws, surveyOpts{})
	e.exec(`UPDATE surveys SET blocks = ?::jsonb WHERE id = ?`, textBlocks, sv)
	responses := map[string]uuid.UUID{}
	for _, r := range []struct {
		name, data string
		attributes *string
	}{
		{"onboarding", `{"improve":"The Onboarding was slow","rate":2,"pick":"Pricing"}`, ptr(`{"plan":"pro","seats":"25","renewal":"2026-03-01T00:00:00.000Z"}`)},
		{"pricing", `{"improve":"pricing is too high","rate":7}`, ptr(`{"plan":"enterprise","seats":"100","renewal":"2026-03-02T10:30:00.000Z"}`)},
		{"address", `{"addr":["1 Onboarding Way","","Tokyo","","",""],"rate":9}`, ptr(`{"plan":"Professional","seats":3}`)},
		{"contact", `{"ci":["Ann","Lee","ann@example.com","","Onboarding Inc"],"rate":5}`, ptr(`{"plan":"free","seats":"many"}`)},
		{"choice", `{"pick":"Onboarding","rate":3}`, nil},
		{"empty", `{}`, nil},
	} {
		responses[r.name] = e.insert(`INSERT INTO responses (survey_id, finished, data, contact_attributes) VALUES (?, true, ?::jsonb, ?::jsonb) RETURNING id`, sv, r.data, r.attributes)
	}
	return sv, responses
}

// expectFiltered checks that the list and the count of the survey's responses under each filter are the named responses.
func expectFiltered(t *testing.T, e *env, owner, sv uuid.UUID, responses map[string]uuid.UUID, cases []struct {
	name, filter string
	want         []string
},
) {
	t.Helper()
	for _, c := range cases {
		var filter any
		if err := json.Unmarshal([]byte(c.filter), &filter); err != nil {
			t.Fatal(err)
		}
		rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/search", map[string]any{"filterCriteria": filter})
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
			continue
		}
		var got []string
		for _, id := range listedIDs(decodeList(t, rec.Body.Bytes())) {
			for name, rid := range responses {
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

// Open text answers filter by the text operators, ignoring case, and the keyword search finds a word in any free-text answer: an open text answer, or a field of an address or contact info answer, never a choice.
func TestTextOperatorsAndKeywordSearchReadTheRespondentsWords(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	sv, r := textSurvey(t, e, ws)

	expectFiltered(t, e, owner, sv, r, []struct {
		name, filter string
		want         []string
	}{
		{"contains ignores case", `{"data":{"improve":{"op":"contains","value":"onboarding"}}}`, []string{"onboarding"}},
		{"contains is literal, not a LIKE pattern", `{"data":{"improve":{"op":"contains","value":"pric%"}}}`, nil},
		{"doesNotContain is an answer without the text", `{"data":{"improve":{"op":"doesNotContain","value":"ONBOARDING"}}}`, []string{"pricing"}},
		{"contains and doesNotContain on one question", `{"data":{"improve":[{"op":"contains","value":"I"},{"op":"doesNotContain","value":"slow"}]}}`, []string{"pricing"}},
		{"startsWith ignores case", `{"data":{"improve":{"op":"startsWith","value":"the onb"}}}`, []string{"onboarding"}},
		{"search", `{"search":"Onboarding"}`, []string{"onboarding", "address", "contact"}},
		{"search ignores case and the spaces around it", `{"search":"  ONBOARDING "}`, []string{"onboarding", "address", "contact"}},
		{"search of a contact info field", `{"search":"ann@example"}`, []string{"contact"}},
		{"a blank search filters nothing", `{"search":"  "}`, []string{"onboarding", "pricing", "address", "contact", "choice", "empty"}},
		{"search with another filter", `{"search":"onboarding","data":{"rate":{"op":"greaterThan","value":4}}}`, []string{"address", "contact"}},
	})
}

// A key takes several conditions, all of which hold: a range on a rating, the same operator twice on a language.
func TestSeveralConditionsOnOneKeyAllHold(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	sv, r := textSurvey(t, e, ws)

	expectFiltered(t, e, owner, sv, r, []struct {
		name, filter string
		want         []string
	}{
		{"one condition in an array", `{"data":{"rate":[{"op":"greaterThan","value":4}]}}`, []string{"pricing", "address", "contact"}},
		{"a range", `{"data":{"rate":[{"op":"greaterThan","value":2},{"op":"lessThan","value":8}]}}`, []string{"pricing", "contact", "choice"}},
		{"an empty range", `{"data":{"rate":[{"op":"greaterThan","value":8},{"op":"lessThan","value":3}]}}`, nil},
		{"a contact attribute range", `{"contactAttributes":{"seats":[{"op":"greaterThan","value":3},{"op":"lessEqual","value":25}]}}`, []string{"onboarding"}},
	})

	for name, filter := range map[string]any{
		"an empty array":                     map[string]any{"data": map[string]any{"rate": []any{}}},
		"a malformed condition in the array": map[string]any{"data": map[string]any{"rate": []any{map[string]any{"op": "greaterThan", "value": 2}, map[string]any{"op": "nope"}}}},
	} {
		if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/count", map[string]any{"filterCriteria": filter}); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

// Contact attributes filter by their type: text by the text operators, a number (stored as text, or as a JSON number) by comparisons, a date by its day. notEquals and isNotSet count a response without the attribute, or without a contact.
func TestContactAttributeOperatorsFollowTheAttributesType(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	sv, r := textSurvey(t, e, ws)

	expectFiltered(t, e, owner, sv, r, []struct {
		name, filter string
		want         []string
	}{
		{"text equals", `{"contactAttributes":{"plan":{"op":"equals","value":"pro"}}}`, []string{"onboarding"}},
		{"text notEquals counts responses without a contact", `{"contactAttributes":{"plan":{"op":"notEquals","value":"pro"}}}`, []string{"pricing", "address", "contact", "choice", "empty"}},
		{"text contains ignores case", `{"contactAttributes":{"plan":{"op":"contains","value":"PRO"}}}`, []string{"onboarding", "address"}},
		{"text startsWith", `{"contactAttributes":{"plan":{"op":"startsWith","value":"ent"}}}`, []string{"pricing"}},
		{"isSet", `{"contactAttributes":{"renewal":{"op":"isSet"}}}`, []string{"onboarding", "pricing"}},
		{"isNotSet", `{"contactAttributes":{"renewal":{"op":"isNotSet"}}}`, []string{"address", "contact", "choice", "empty"}},
		{"number greaterThan reads text and JSON numbers", `{"contactAttributes":{"seats":{"op":"greaterThan","value":2}}}`, []string{"onboarding", "pricing", "address"}},
		{"number comparison is numeric, not textual", `{"contactAttributes":{"seats":{"op":"greaterThan","value":30}}}`, []string{"pricing"}},
		{"number lessThan", `{"contactAttributes":{"seats":{"op":"lessThan","value":25}}}`, []string{"address"}},
		{"number equals reads 25 and 25.0 alike", `{"contactAttributes":{"seats":{"op":"equals","value":25.0}}}`, []string{"onboarding"}},
		{"number notEquals counts text that is no number and no attribute", `{"contactAttributes":{"seats":{"op":"notEquals","value":25}}}`, []string{"pricing", "address", "contact", "choice", "empty"}},
		{"date isSameDay", `{"contactAttributes":{"renewal":{"op":"isSameDay","value":"2026-03-02"}}}`, []string{"pricing"}},
		{"date isBefore", `{"contactAttributes":{"renewal":{"op":"isBefore","value":"2026-03-02"}}}`, []string{"onboarding"}},
		{"date isAfter excludes the day itself", `{"contactAttributes":{"renewal":{"op":"isAfter","value":"2026-03-01"}}}`, []string{"pricing"}},
		{"a date range", `{"contactAttributes":{"renewal":[{"op":"isAfter","value":"2026-02-28"},{"op":"isBefore","value":"2026-03-02"}]}}`, []string{"onboarding"}},
		{"a date operator on text that is no date", `{"contactAttributes":{"plan":{"op":"isAfter","value":"2000-01-01"}}}`, nil},
	})

	for name, condition := range map[string]map[string]any{
		"a comparison with text":        {"op": "greaterThan", "value": "10"},
		"a date operator with no date":  {"op": "isAfter", "value": "March"},
		"a date operator with a number": {"op": "isSameDay", "value": 20260301},
		"a text operator with a number": {"op": "contains", "value": 2},
		"an unknown operator":           {"op": "isAnyOf", "value": []string{"pro"}},
	} {
		filter := map[string]any{"contactAttributes": map[string]any{"seats": condition}}
		if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/count", map[string]any{"filterCriteria": filter}); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

// One condition keeps the object shape every client sent before ranges; several read and write as an array.
func TestConditionsReadAndWriteOneConditionAsAnObject(t *testing.T) {
	for _, c := range []struct{ in, out string }{
		{`{"op":"equals","value":"a"}`, `{"op":"equals","value":"a"}`},
		{`[{"op":"equals","value":"a"}]`, `{"op":"equals","value":"a"}`},
		{`[{"op":"greaterThan","value":2},{"op":"lessThan","value":8}]`, `[{"op":"greaterThan","value":2},{"op":"lessThan","value":8}]`},
	} {
		var cs Conditions
		if err := json.Unmarshal([]byte(c.in), &cs); err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		out, err := json.Marshal(cs)
		if err != nil || string(out) != c.out {
			t.Errorf("%s: %s %v, want %s", c.in, out, err, c.out)
		}
	}
	var cs Conditions
	if err := json.Unmarshal([]byte(`"equals"`), &cs); err == nil {
		t.Errorf("a string read as conditions %v", cs)
	}
}
