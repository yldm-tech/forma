package workflow

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/v3api"
)

var marker = regexp.MustCompile(`^\[redacted:[0-9a-f]{12}\]$`)

func redactionInput() map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"nodes": []any{
			map[string]any{"id": "condition", "type": "if_else", "config": map[string]any{"condition": map[string]any{"id": "g"}}},
			map[string]any{"id": "send", "type": "action", "actionType": "send_email", "config": map[string]any{
				"to": "recipient@example.com", "from": "noreply@example.com", "replyTo": []any{"support@example.com"},
				"subject": "Hi", "body": "<p>Body</p>", "attachResponseData": true, "includeVariables": false,
			}},
		},
	}
}

func redactedConfig(t *testing.T, v any) map[string]any {
	t.Helper()
	return v.(map[string]any)["nodes"].([]any)[1].(map[string]any)["config"].(map[string]any)
}

func TestRedactDefinition(t *testing.T) {
	in := redactionInput()
	out := redactDefinition(in, "key")
	cfg := redactedConfig(t, out)
	for _, field := range []string{"to", "from", "subject", "body"} {
		if !marker.MatchString(cfg[field].(string)) {
			t.Errorf("%s = %v", field, cfg[field])
		}
	}
	if reply := cfg["replyTo"].([]any); len(reply) != 1 || !marker.MatchString(reply[0].(string)) {
		t.Errorf("replyTo = %v", reply)
	}
	if cfg["attachResponseData"] != true || cfg["includeVariables"] != false {
		t.Errorf("non-personal fields changed: %v", cfg)
	}
	if strings.Contains(string(mustJSON(out)), "@example.com") {
		t.Errorf("an address survived: %s", mustJSON(out))
	}
	if redactedConfig(t, in)["to"] != "recipient@example.com" {
		t.Error("the input was mutated")
	}
	if !jsonEqual(out.(map[string]any)["nodes"].([]any)[0], in["nodes"].([]any)[0]) {
		t.Error("an if_else node was redacted")
	}
	again := redactedConfig(t, redactDefinition(redactionInput(), "key"))
	if again["to"] != cfg["to"] {
		t.Error("the same value gave different markers")
	}
	changed := redactionInput()
	redactedConfig(t, changed)["to"] = "other@example.com"
	redactedConfig(t, changed)["replyTo"] = []any{"other@example.com"}
	if c := redactedConfig(t, redactDefinition(changed, "key")); c["to"] == cfg["to"] || c["replyTo"].([]any)[0] == cfg["replyTo"].([]any)[0] {
		t.Error("a changed value kept its marker")
	}
	unkeyed := redactedConfig(t, redactDefinition(redactionInput(), ""))
	otherKey := redactedConfig(t, redactDefinition(redactionInput(), "other"))
	if unkeyed["to"] == cfg["to"] || otherKey["to"] == cfg["to"] || !marker.MatchString(unkeyed["to"].(string)) {
		t.Error("the marker does not depend on the key")
	}
	if got := redactDefinition(map[string]any{"schemaVersion": 1}, "key"); !jsonEqual(got, map[string]any{"schemaVersion": 1}) {
		t.Errorf("no nodes: %v", got)
	}
	if redactDefinition(nil, "key") != nil || redactDefinition("nope", "key") != "nope" {
		t.Error("non-objects changed")
	}
}

func jsonEqual(a, b any) bool {
	return string(mustJSON(a)) == string(mustJSON(b))
}

func TestSerializers(t *testing.T) {
	created := time.Date(2026, 6, 9, 12, 0, 0, 123456000, time.UTC)
	name := "Ada"
	errMsg := "boom"
	run := Run{ID: uuid.New(), WorkflowID: uuid.New(), WorkspaceID: uuid.New(), Status: RunFailed, TriggerType: TriggerResponseCompleted, CreatedAt: created, UpdatedAt: created,
		Error: &errMsg, Attempt: 2, WorkflowName: "Notify", TriggerPayload: json.RawMessage(`{"type":"response.completed"}`),
		Data: json.RawMessage(`{"trigger":{"type":"response.completed"},"steps":[{"stepId":"s"}],"extra":1}`), LastErrorAt: &created,
		Logs: []RunLog{{ID: uuid.New(), Sequence: 1, StepID: "s", StepType: ActionSendEmail, Status: StepFailed}}}
	w := Workflow{ID: uuid.New(), WorkspaceID: run.WorkspaceID, Name: "Notify", Status: StatusDraft, CreatedAt: created, UpdatedAt: created, CreatorName: &name,
		Definition: raw(t, testDefinition()), LastRun: &run, RunCount: 3}

	item := toListItem(&w)
	if *item.TriggerType != TriggerResponseCompleted || *item.SurveyID != testSurveyID || item.CreatedAt != "2026-06-09T12:00:00.123Z" || item.Creator.Name != "Ada" || item.LastRun.ID != run.ID || item.RunCount != 3 {
		t.Fatalf("list item %+v", item)
	}
	w.CreatorName, w.LastRun = nil, nil
	w.Definition = json.RawMessage(`{"schemaVersion":1,"trigger":null,"nodes":[],"edges":[],"entryNodeId":null}`)
	body := string(mustJSON(toResource(&w)))
	for _, want := range []string{`"creator":null`, `"lastRun":null`, `"triggerType":null`, `"surveyId":null`, `"description":null`, `"definition":{"schemaVersion":1`} {
		if !strings.Contains(body, want) {
			t.Errorf("resource %s lacks %s", body, want)
		}
	}
	summary := string(mustJSON(toRunSummary(&run)))
	if !strings.Contains(summary, `"startedAt":null`) || !strings.Contains(summary, `"workflowVersionId":null`) {
		t.Errorf("summary %s", summary)
	}
	if li := toRunListItem(&run); li.WorkflowName != "Notify" || li.ID != run.ID {
		t.Errorf("run list item %+v", li)
	}
	detail := string(mustJSON(toRunResource(&run)))
	for _, want := range []string{`"data":{"extra":1,"steps":[{"stepId":"s"}]}`, `"triggerPayload":{"type":"response.completed"}`, `"lastErrorAt":"2026-06-09T12:00:00.123Z"`, `"nextAttemptAt":null`,
		`"input":{}`, `"output":{}`, `"error":null`, `"finishedAt":null`} {
		if !strings.Contains(detail, want) {
			t.Errorf("run detail %s lacks %s", detail, want)
		}
	}
	queued := run
	queued.Data = json.RawMessage(`{}`)
	if got := string(mustJSON(toRunResource(&queued).Data)); got != `{"steps":[]}` {
		t.Errorf("queued data %s", got)
	}
}

func TestCursors(t *testing.T) {
	at := time.Date(2026, 6, 9, 12, 0, 0, 123456000, time.UTC)
	w := Workflow{ID: uuid.New(), Name: "Name", CreatedAt: at, UpdatedAt: at}
	for _, sortBy := range []string{sortName, sortCreatedAt, sortUpdatedAt} {
		token := nextListCursor(w, sortBy)
		if strings.ContainsAny(token, "{}=") {
			t.Fatalf("cursor %q is not opaque base64url", token)
		}
		c, err := decodeListCursor(token, sortBy)
		if err != nil || c.id != w.ID || (sortBy != sortName && !c.at.Equal(at)) || (sortBy == sortName && c.Value != "Name") {
			t.Fatalf("%s: %+v %v", sortBy, c, err)
		}
	}
	problem := func(_ any, err error) *v3api.Problem {
		t.Helper()
		var p *v3api.Problem
		if !errors.As(err, &p) || p.Status != 400 || len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != "cursor" {
			t.Fatalf("not a cursor problem: %v", err)
		}
		return p
	}
	if p := problem(decodeListCursor(nextListCursor(w, sortName), sortCreatedAt)); p.Detail != mismatchedCursor {
		t.Fatalf("mismatch: %+v", p)
	}
	encode := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, token := range []string{"not-a-cursor", encode(`{"version":1,"sortBy":"updatedAt","value":"yesterday","id":"` + w.ID.String() + `"}`),
		encode(`{"version":2,"sortBy":"name","value":"x","id":"` + w.ID.String() + `"}`), encode(`{"version":1,"sortBy":"name","value":"x"}`)} {
		if p := problem(decodeListCursor(token, sortName)); p.Detail != invalidCursor {
			t.Fatalf("%q: %+v", token, p)
		}
	}
	r := Run{ID: uuid.New(), CreatedAt: at}
	c, err := decodeRunCursor(nextRunCursor(r))
	if err != nil || c.id != r.ID || !c.at.Equal(at) {
		t.Fatalf("run cursor %+v %v", c, err)
	}
	for _, token := range []string{"%%%", encode(`{"version":1,"value":"` + at.Format(time.RFC3339) + `"}`), encode(`{"version":1,"value":"soon","id":"` + r.ID.String() + `"}`), encode(`{"version":9,"value":"` + at.Format(time.RFC3339) + `","id":"` + r.ID.String() + `"}`)} {
		_ = problem(decodeRunCursor(token))
	}
}

func TestMessageID(t *testing.T) {
	run := uuid.MustParse("0192a7a6-3f6e-7c4d-9b1a-5e2f8c3d4a10")
	a := MessageID(run, "send-email", "team@acme.example")
	if !regexp.MustCompile(`^<[0-9a-f]{32}@acme\.example>$`).MatchString(a) || MessageID(run, "send-email", "x@acme.example") != a {
		t.Fatalf("message id %s", a)
	}
	if MessageID(run, "other", "team@acme.example") == a {
		t.Fatal("different steps share a Message-ID")
	}
	if got := MessageID(run, "send-email", "nobody"); !strings.HasSuffix(got, "@forma.ylam.ai>") {
		t.Fatalf("fallback domain: %s", got)
	}
}
