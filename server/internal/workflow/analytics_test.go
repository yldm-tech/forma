package workflow

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
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
