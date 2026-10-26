package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// responsesFixture is a survey of the fixture's first workspace with an open-text and an NPS question and a hidden field, and three responses to it: r1 finished, with a contact, a tag, a hidden field value and an answer to a question since removed; r2 partial; r3 finished.
type responsesFixture struct {
	fixture
	survey, contact, r1, r2, r3 uuid.UUID
}

const responsesBlocks = `[{"id":"b1","name":"Block","elements":[
	{"id":"q1","type":"openText","headline":{"default":"<p>What could we <b>improve</b>?</p>"},"required":false,"inputType":"text"},
	{"id":"q2","type":"nps","headline":{"default":"How likely are you to recommend us?"},"required":false,"lowerLabel":{"default":"Not likely"},"upperLabel":{"default":"Very likely"}}]}]`

func (e *env) responsesFixture() responsesFixture {
	e.t.Helper()
	f := responsesFixture{fixture: e.fixture()}
	f.survey = e.insert(`INSERT INTO surveys (workspace_id, name, type, status, blocks, hidden_fields) VALUES (?, 'Customer Satisfaction 2026', 'link', 'inProgress', ?::jsonb, '{"enabled":true,"fieldIds":["source"]}'::jsonb) RETURNING id`, f.ws, responsesBlocks)
	f.contact = e.insert(`INSERT INTO contacts (workspace_id) VALUES (?) RETURNING id`, f.ws)
	f.r1 = e.insert(`INSERT INTO responses (survey_id, contact_id, created_at, updated_at, finished, data, contact_attributes, meta) VALUES (?, ?, '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', true,
		'{"q1":"A faster pricing page","q2":9,"source":"newsletter","removed":"old answer"}'::jsonb, '{"email":"ann@example.com"}'::jsonb,
		'{"url":"https://example.com/feedback","country":"JP","ipAddress":"203.0.113.7"}'::jsonb) RETURNING id`, f.survey, f.contact)
	f.r2 = e.insert(`INSERT INTO responses (survey_id, created_at, updated_at, finished, data) VALUES (?, '2026-10-02T10:00:00Z', '2026-10-02T10:00:00Z', false, '{"q1":"More integrations","q2":3}'::jsonb) RETURNING id`, f.survey)
	f.r3 = e.insert(`INSERT INTO responses (survey_id, created_at, updated_at, finished, data) VALUES (?, '2026-10-03T10:00:00Z', '2026-10-03T10:00:00Z', true, '{"q2":10}'::jsonb) RETURNING id`, f.survey)
	tag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'vip') RETURNING id`, f.ws)
	e.exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?)`, f.r1, tag)
	return f
}

// ids are the ids of a list_responses page, in order.
func ids(t *testing.T, payload map[string]any) []string {
	t.Helper()
	data, ok := payload["data"].([]any)
	if !ok {
		t.Fatalf("no data: %v", payload)
	}
	out := make([]string, len(data))
	for i, item := range data {
		out[i] = item.(map[string]any)["id"].(string)
	}
	return out
}

func sameIDs(got []string, want ...uuid.UUID) bool {
	if len(got) != len(want) {
		return false
	}
	for i, id := range want {
		if got[i] != id.String() {
			return false
		}
	}
	return true
}

// list_responses pages through a survey's responses newest first and filters them; each answer carries its question's headline as text, the rest of the data comes under otherData, and the respondent is named only on request.
func TestListResponses(t *testing.T) {
	e := newEnv(t)
	f := e.responsesFixture()
	token := bearer(e.accessToken(f.owner, "responses:read"))

	failed, page, text := e.tool(token, "list_responses", map[string]any{"surveyId": f.survey.String(), "limit": 2})
	if failed || !sameIDs(ids(t, page), f.r3, f.r2) || page["requestId"] == "" {
		t.Fatalf("first page: %s", text)
	}
	meta := page["meta"].(map[string]any)
	next, _ := meta["nextCursor"].(string)
	if meta["limit"] != float64(2) || next == "" {
		t.Fatalf("first page meta: %v", meta)
	}
	failed, page, text = e.tool(token, "list_responses", map[string]any{"surveyId": f.survey.String(), "limit": 2, "cursor": next})
	if failed || !sameIDs(ids(t, page), f.r1) || page["meta"].(map[string]any)["nextCursor"] != nil {
		t.Fatalf("second page: %s", text)
	}

	r1 := page["data"].([]any)[0].(map[string]any)
	if r1["finished"] != true || mustString(r1["tags"]) != `["vip"]` || mustString(r1["otherData"]) != `{"removed":"old answer","source":"newsletter"}` ||
		mustString(r1["answers"]) != `[{"headline":"What could we improve?","questionId":"q1","type":"openText","value":"A faster pricing page"},{"headline":"How likely are you to recommend us?","questionId":"q2","type":"nps","value":9}]` {
		t.Fatalf("response: %v", r1)
	}
	if _, ok := r1["contact"]; ok || r1["contactAttributes"] != nil || strings.Contains(text, "ann@example.com") || strings.Contains(text, "203.0.113.7") ||
		r1["meta"].(map[string]any)["country"] != "JP" {
		t.Fatalf("the respondent is named without includeContact: %s", text)
	}

	failed, page, text = e.tool(token, "list_responses", map[string]any{"surveyId": f.survey.String(), "includeContact": true, "search": "PRICING"})
	if failed || !sameIDs(ids(t, page), f.r1) {
		t.Fatalf("search: %s", text)
	}
	r1 = page["data"].([]any)[0].(map[string]any)
	if r1["contact"].(map[string]any)["id"] != f.contact.String() || r1["contactAttributes"].(map[string]any)["email"] != "ann@example.com" || r1["meta"].(map[string]any)["ipAddress"] != "203.0.113.7" {
		t.Fatalf("includeContact: %v", r1)
	}

	cases := []struct {
		name string
		args map[string]any
		want []uuid.UUID
	}{
		{"finished", map[string]any{"finished": true}, []uuid.UUID{f.r3, f.r1}},
		{"partial", map[string]any{"finished": false}, []uuid.UUID{f.r2}},
		{"created range", map[string]any{"createdFrom": "2026-10-02T00:00:00Z", "createdTo": "2026-10-02T23:59:59+00:00"}, []uuid.UUID{f.r2}},
		{"created from, inclusive", map[string]any{"createdFrom": "2026-10-02T10:00:00Z"}, []uuid.UUID{f.r3, f.r2}},
	}
	for _, tc := range cases {
		tc.args["surveyId"] = f.survey.String()
		if failed, page, text := e.tool(token, "list_responses", tc.args); failed || !sameIDs(ids(t, page), tc.want...) {
			t.Errorf("%s: %s", tc.name, text)
		}
	}

	failed, payload, text := e.tool(token, "list_responses", map[string]any{"surveyId": f.survey.String(), "cursor": "not-a-cursor"})
	if errObj, _ := payload["error"].(map[string]any); !failed || errObj["status"] != float64(400) || !strings.Contains(mustString(errObj["invalid_params"]), `"name":"cursor"`) {
		t.Fatalf("invalid cursor: %s", text)
	}
	failed, payload, text = e.tool(token, "list_responses", map[string]any{"surveyId": f.survey.String(), "createdFrom": "yesterday"})
	if errObj, _ := payload["error"].(map[string]any); !failed || errObj["status"] != float64(400) || !strings.Contains(mustString(errObj["invalid_params"]), `"name":"createdFrom"`) {
		t.Fatalf("invalid createdFrom: %s", text)
	}
}

// get_survey_summary answers the summary page's statistics with each card's question reduced to its id, type and headline, its listed answers cut to samplesPerQuestion, and no contact anywhere.
func TestGetSurveySummary(t *testing.T) {
	e := newEnv(t)
	f := e.responsesFixture()
	token := bearer(e.accessToken(f.owner, "responses:read"))

	failed, payload, text := e.tool(token, "get_survey_summary", map[string]any{"surveyId": f.survey.String()})
	if failed {
		t.Fatalf("summary: %s", text)
	}
	data := payload["data"].(map[string]any)
	meta := data["meta"].(map[string]any)
	if data["surveyId"] != f.survey.String() || meta["totalResponses"] != float64(3) || meta["completedResponses"] != float64(2) {
		t.Fatalf("summary meta: %s", text)
	}
	if strings.Contains(text, "ann@example.com") || strings.Contains(text, `"contact"`) || strings.Contains(text, "contactAttributes") || strings.Contains(text, "lowerLabel") {
		t.Fatalf("the summary names a respondent or carries whole elements: %s", text)
	}
	cards := map[string]map[string]any{}
	for _, c := range data["summary"].([]any) {
		card := c.(map[string]any)
		if el, ok := card["element"].(map[string]any); ok {
			cards[el["id"].(string)] = card
		}
	}
	q1, q2 := cards["q1"], cards["q2"]
	if q1 == nil || q2 == nil {
		t.Fatalf("cards: %s", text)
	}
	if mustString(q1["element"]) != `{"headline":"What could we improve?","id":"q1","type":"openText"}` || len(q1["samples"].([]any)) != 2 {
		t.Fatalf("open text card: %v", q1)
	}
	if q2["responseCount"] != float64(3) || q2["promoters"].(map[string]any)["count"] != float64(2) || q2["detractors"].(map[string]any)["count"] != float64(1) {
		t.Fatalf("NPS card: %v", q2)
	}

	failed, payload, text = e.tool(token, "get_survey_summary", map[string]any{"surveyId": f.survey.String(), "samplesPerQuestion": 1, "finished": true})
	if failed {
		t.Fatalf("filtered summary: %s", text)
	}
	data = payload["data"].(map[string]any)
	if data["meta"].(map[string]any)["totalResponses"] != float64(2) {
		t.Fatalf("finished filter: %s", text)
	}
	for _, c := range data["summary"].([]any) {
		card := c.(map[string]any)
		if samples, ok := card["samples"].([]any); ok && len(samples) > 1 {
			t.Fatalf("samplesPerQuestion 1: %v", card)
		}
	}
}

// The response tools need responses:read, which surveys:read does not imply, and read access to the survey's workspace; an unknown survey, a survey of another organization or workspace, and a cuid2 id all get the 403 that hides whether the survey exists. Every API key may read responses, as /api/v2/management/responses lets it.
func TestResponseToolsAuthorization(t *testing.T) {
	e := newEnv(t)
	f := e.responsesFixture()

	surveysOnly := bearer(e.accessToken(f.owner, "surveys:read surveys:write workflows:read workflows:write"))
	for _, tool := range []string{"list_responses", "get_survey_summary"} {
		failed, payload, text := e.tool(surveysOnly, tool, map[string]any{"surveyId": f.survey.String()})
		if errObj, _ := payload["error"].(map[string]any); !failed || errObj["status"] != float64(403) || errObj["detail"] != "OAuth token does not include the required MCP scope: responses:read" {
			t.Fatalf("%s without responses:read: %s", tool, text)
		}
	}
	responsesOnly := bearer(e.accessToken(f.owner, "responses:read"))
	if failed, payload, text := e.tool(responsesOnly, "list_workspaces", map[string]any{}); failed || len(payload["data"].([]any)) != 2 {
		t.Fatalf("list_workspaces with responses:read: %s", text)
	}

	outsider := bearer(e.accessToken(f.outsider, "responses:read"))
	readKey := map[string]string{"x-api-key": e.apiKey(f.org, f.ws, "read")}
	otherWorkspaceKey := map[string]string{"x-api-key": e.apiKey(f.org, f.ws2, "manage")}
	refused := []struct {
		name     string
		headers  map[string]string
		surveyID string
	}{
		{"another organization", outsider, f.survey.String()},
		{"a key of another workspace", otherWorkspaceKey, f.survey.String()},
		{"an unknown survey", responsesOnly, uuid.NewString()},
		{"a cuid2 id", responsesOnly, "clunknownsurveyaaaaaaaaa1"},
	}
	for _, tc := range refused {
		for _, tool := range []string{"list_responses", "get_survey_summary"} {
			failed, payload, text := e.tool(tc.headers, tool, map[string]any{"surveyId": tc.surveyID})
			if errObj, _ := payload["error"].(map[string]any); !failed || errObj["status"] != float64(403) || errObj["code"] != "forbidden" || errObj["detail"] != "You are not authorized to access this resource" {
				t.Errorf("%s, %s: %s", tc.name, tool, text)
			}
		}
	}

	if failed, payload, text := e.tool(readKey, "list_responses", map[string]any{"surveyId": f.survey.String()}); failed || len(payload["data"].([]any)) != 3 {
		t.Fatalf("read key: %s", text)
	}
	failed, payload, text := e.tool(readKey, "get_survey_summary", map[string]any{"surveyId": f.survey.String()})
	if failed || payload["data"].(map[string]any)["meta"].(map[string]any)["totalResponses"] != float64(3) {
		t.Fatalf("read key summary: %s", text)
	}
	var out map[string]any
	if json.Unmarshal([]byte(text), &out) != nil {
		t.Fatalf("summary text is not JSON: %s", text)
	}
}
