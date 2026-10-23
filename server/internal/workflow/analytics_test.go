package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/server/internal/platform/analytics/analyticstest"
)

func TestSummarizeDefinition(t *testing.T) {
	full, err := os.ReadFile("testdata/workflow-definition.full.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		raw  string
		want DefinitionSummary
	}{
		{"the full fixture, by concrete node type", string(full), DefinitionSummary{TriggerType: "response.completed", ActionTypes: []string{"if_else", "send_email"}, ActionCount: 4, NodeCount: 5}},
		{"an empty draft", `{"schemaVersion":1,"trigger":null,"nodes":[],"edges":[],"entryNodeId":null}`, DefinitionSummary{ActionTypes: []string{}}},
		{"node types it has never seen", `{"trigger":{"type":"trigger","triggerType":"contact.attribute_changed"},"nodes":[{"type":"action","actionType":"send_email"},{"type":"action","actionType":"call_webhook"},{"type":"delay"},{"type":"action","actionType":"send_email"}]}`,
			DefinitionSummary{TriggerType: "contact.attribute_changed", ActionTypes: []string{"call_webhook", "delay", "send_email"}, ActionCount: 4, NodeCount: 5}},
		{"null", `null`, DefinitionSummary{ActionTypes: []string{}}},
		{"an empty object", `{}`, DefinitionSummary{ActionTypes: []string{}}},
		{"malformed trigger and nodes", `{"trigger":"not-a-node","nodes":"not-an-array"}`, DefinitionSummary{ActionTypes: []string{}}},
		{"untyped nodes", `{"nodes":[null,42,{"type":""},{"type":"action"}]}`, DefinitionSummary{ActionTypes: []string{"action"}, ActionCount: 1, NodeCount: 1}},
	} {
		if got := SummarizeDefinition(json.RawMessage(tc.raw)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestSummarizeOptions(t *testing.T) {
	full, err := os.ReadFile("testdata/workflow-definition.full.json")
	if err != nil {
		t.Fatal(err)
	}
	o := SummarizeOptions(full)
	if o.EndingScope == nil || *o.EndingScope != "specific" || o.EmailRecipientKind == nil || *o.EmailRecipientKind != "mixed" || !*o.AttachResponseData || !*o.IncludeVariables || !*o.IncludeHiddenFields {
		t.Errorf("full fixture options = %+v", o)
	}
	if b, _ := json.Marshal(o); strings.Contains(string(b), "@example.com") {
		t.Errorf("options carry a recipient: %s", b)
	}
	o = SummarizeOptions(json.RawMessage(`{"trigger":{"type":"trigger","triggerType":"response.completed","config":{"surveyId":"s","endingCardIds":[]}},"nodes":[{"type":"action","actionType":"send_email","config":{"to":"team@example.com","attachResponseData":false}}]}`))
	if *o.EndingScope != "all" || *o.EmailRecipientKind != "literal" || *o.AttachResponseData || *o.IncludeVariables || *o.IncludeHiddenFields {
		t.Errorf("literal options = %+v", o)
	}
	o = SummarizeOptions(json.RawMessage(`{"trigger":null,"nodes":[{"type":"action","actionType":"send_email","config":{"to":"cm9zr4mps000008l8email","includeVariables":true}}]}`))
	if o.EndingScope != nil || *o.EmailRecipientKind != "element" {
		t.Errorf("element options = %+v", o)
	}
	o = SummarizeOptions(json.RawMessage(`{"trigger":{"type":"trigger","triggerType":"contact.created"},"nodes":[{"type":"delay"}]}`))
	if o != (DefinitionOptions{}) {
		t.Errorf("options of a definition without a known trigger or email = %+v, want all nil", o)
	}
}

// TestLifecycleAndRunFailureEvents: the builder's mutations send forma2's lifecycle events with the definition's shape and never its content, and a run that fails on its last attempt sends one workflow_run_failed with a coarse failure class.
func TestLifecycleAndRunFailureEvents(t *testing.T) {
	// After newEnv, which makes the test parallel: a client installed earlier would stay installed while the package's sequential tests run.
	e := newEnv(t)
	rec := analyticstest.Start(t)
	f := e.fixture()
	w := e.create(f.owner, f.ws, "Notify", definition(f.survey, "q1"))
	e.ok(e.lifecycle(f.owner, w.ID, "enable"), http.StatusOK, nil)
	e.ok(e.lifecycle(f.owner, w.ID, "test"), http.StatusOK, nil)

	r := e.respond(f.survey, true, `{}`, nil)
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, r)); err != nil {
		t.Fatal(err)
	}
	run := e.runFor(w.ID, r.ID)
	if err := e.runner.Work(context.Background(), runJob(run, w.ID, f.ws, 3)); err != nil {
		t.Fatal(err)
	}
	// A second delivery of the same run finds it terminal and reports nothing.
	if err := e.runner.Work(context.Background(), runJob(run, w.ID, f.ws, 3)); err != nil {
		t.Fatal(err)
	}

	// Every event of this workflow was captured above; once flushed, all of them have arrived, and the events of tests running in parallel are told apart by the workflow.
	rec.Flush()
	mine := func(event string) []analyticstest.Message {
		var out []analyticstest.Message
		for _, m := range rec.All() {
			if m.Event == event && m.Properties["workflow_id"] == w.ID.String() {
				out = append(out, m)
			}
		}
		return out
	}
	created := mine("workflow_created")
	if len(created) != 1 {
		t.Fatalf("workflow_created = %v", created)
	}
	c := created[0]
	if c.DistinctID != f.owner.String() || c.Properties["via"] != "ui" || c.Properties["status"] != "draft" || c.Properties["organization_id"] != f.org.String() ||
		c.Properties["trigger_type"] != "response.completed" || c.Properties["action_types_joined"] != "send_email" || c.Properties["node_count"] != float64(2) ||
		c.Properties["email_recipient_kind"] != "element" || c.Properties["ending_scope"] != "all" {
		t.Errorf("workflow_created = %v", c.Properties)
	}
	if _, ok := c.Properties["previous_status"]; ok {
		t.Error("workflow_created carries a previous status")
	}
	if enabled := mine("workflow_enabled"); len(enabled) != 1 || enabled[0].Properties["previous_status"] != "draft" || enabled[0].Properties["status"] != "enabled" {
		t.Errorf("workflow_enabled = %v", enabled)
	}
	if tested := mine("workflow_tested"); len(tested) != 1 || tested[0].Properties["test_ok"] != true {
		t.Errorf("workflow_tested = %v", tested)
	}
	failed := mine("workflow_run_failed")
	if len(failed) != 1 {
		t.Fatalf("workflow_run_failed = %v", failed)
	}
	p := failed[0].Properties
	if failed[0].DistinctID != f.org.String() || p["run_id"] != run.String() || p["error_kind"] != "step_failed" || p["failed_step_type"] != "send_email" || p["attempt"] != float64(3) || p["trigger_type"] != "response.completed" {
		t.Errorf("workflow_run_failed = %v", p)
	}
	for _, m := range rec.All() {
		if b, _ := json.Marshal(m.Properties); strings.Contains(string(b), "Recipient value not found") || strings.Contains(string(b), "owner@example.com") {
			t.Errorf("%s carries an error message or an address: %s", m.Event, b)
		}
	}
}
