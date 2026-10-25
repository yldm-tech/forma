package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	testSurveyID = "0192a7a6-3f6e-7c4d-9b1a-5e2f8c3d4a10"
	testEndingID = "cm9zr4q7i000108l84gozfggr"
)

func sendEmailConfig() map[string]any {
	return map[string]any{
		"from": "noreply@example.com", "to": "jane@example.com", "replyTo": []any{"support@example.com"},
		"subject": "Thanks for your response", "body": "We received your response.",
		"attachResponseData": true, "includeVariables": false, "includeHiddenFields": false,
	}
}

func emailNode(id string) map[string]any {
	return map[string]any{"id": id, "type": "action", "actionType": "send_email", "config": sendEmailConfig()}
}

// testDefinition is forma2's createDefinition: a trigger and one send_email action after it.
func testDefinition() map[string]any {
	node := emailNode("send-email")
	node["label"] = "Send thank you email"
	node["ui"] = map[string]any{"position": map[string]any{"x": 100, "y": 100}, "collapsed": false}
	return map[string]any{
		"schemaVersion": 1,
		"trigger": map[string]any{"id": "trigger", "type": "trigger", "triggerType": "response.completed",
			"config": map[string]any{"surveyId": testSurveyID, "endingCardIds": []any{testEndingID}}},
		"nodes":       []any{node},
		"edges":       []any{map[string]any{"id": "trigger-send-email", "source": "trigger", "target": "send-email"}},
		"entryNodeId": "trigger",
	}
}

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// failsWith asserts the issues include one whose message matches.
func failsWith(t *testing.T, issues []Issue, message string) {
	t.Helper()
	for _, is := range issues {
		if strings.Contains(is.Message, message) {
			return
		}
	}
	t.Fatalf("issues %+v do not mention %q", issues, message)
}

func ifElseNode(id string, conditions ...any) map[string]any {
	return map[string]any{"id": id, "type": "if_else", "config": map[string]any{"condition": map[string]any{"id": id + "-group", "connector": "and", "conditions": conditions}}}
}

func TestFixtures(t *testing.T) {
	full, err := os.ReadFile("testdata/workflow-definition.full.json")
	if err != nil {
		t.Fatal(err)
	}
	d, issues := ParseDefinition(full)
	if len(issues) > 0 || !slices.ContainsFunc(d.Nodes, func(n Node) bool { return n.Type == NodeIfElse }) {
		t.Fatalf("full definition: %+v", issues)
	}
	executable, err := os.ReadFile("testdata/workflow-executable-definition.full.json")
	if err != nil {
		t.Fatal(err)
	}
	if d, issues := ParseExecutable(executable); len(issues) > 0 || len(d.Nodes) != 1 {
		t.Fatalf("executable definition: %+v", issues)
	}
	// A parsed definition stores what zod returned: every condition kept, the builder's extra ui keys kept, and it parses to itself.
	again, issues := ParseDefinition(mustJSON(d))
	if len(issues) > 0 || string(mustJSON(again)) != string(mustJSON(d)) {
		t.Fatalf("a stored definition does not parse to itself: %+v", issues)
	}
	if stored := string(mustJSON(d)); !strings.Contains(stored, `"color":"blue"`) || !strings.Contains(stored, `"right":{"path":"contact.plan","fallback":"free"}`) {
		t.Fatalf("full fixture lost data: %s", stored)
	}
}

func TestDefinitionRules(t *testing.T) {
	t.Run("parses a valid executable definition", func(t *testing.T) {
		d, issues := ParseExecutable(raw(t, testDefinition()))
		if len(issues) > 0 || d.Nodes[0].Type != NodeAction {
			t.Fatalf("%+v", issues)
		}
	})
	t.Run("rejects invalid graph references", func(t *testing.T) {
		d := testDefinition()
		d["edges"] = []any{map[string]any{"id": "broken-edge", "source": "trigger", "target": "missing-node"}}
		_, issues := ParseDefinition(raw(t, d))
		failsWith(t, issues, "Edge target does not reference a workflow node: missing-node")
		if issues[0].Name() != "edges.0.target" {
			t.Fatalf("path %q", issues[0].Name())
		}
	})
	t.Run("allows a trigger-only draft", func(t *testing.T) {
		d := testDefinition()
		d["nodes"], d["edges"] = []any{}, []any{}
		if _, issues := ParseDefinition(raw(t, d)); len(issues) > 0 {
			t.Fatalf("%+v", issues)
		}
	})
	t.Run("allows an empty draft without a trigger, with nodes and edges defaulted", func(t *testing.T) {
		d, issues := ParseDefinition(json.RawMessage(`{"trigger":null,"entryNodeId":null}`))
		if len(issues) > 0 || d.Trigger != nil || string(mustJSON(d)) != `{"schemaVersion":1,"trigger":null,"nodes":[],"edges":[],"entryNodeId":null}` {
			t.Fatalf("%+v %s", issues, mustJSON(d))
		}
	})
	t.Run("rejects a trigger-less definition whose entryNodeId is set", func(t *testing.T) {
		_, issues := ParseDefinition(json.RawMessage(`{"schemaVersion":1,"trigger":null,"nodes":[],"edges":[],"entryNodeId":"trigger"}`))
		failsWith(t, issues, "entryNodeId must be null")
	})
	t.Run("rejects trigger-less definitions for execution", func(t *testing.T) {
		_, issues := ParseExecutable(json.RawMessage(`{"schemaVersion":1,"trigger":null,"nodes":[],"edges":[],"entryNodeId":null}`))
		if len(issues) == 0 || issues[0].Name() != "trigger" {
			t.Fatalf("%+v", issues)
		}
	})
	for _, field := range []string{"to", "subject", "body"} {
		t.Run("rejects an executable send_email with an empty "+field, func(t *testing.T) {
			d := testDefinition()
			d["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)[field] = "  "
			_, issues := ParseExecutable(raw(t, d))
			failsWith(t, issues, "send_email node send-email is missing "+field)
			if issues[0].Name() != "nodes.0.config."+field {
				t.Fatalf("path %q", issues[0].Name())
			}
		})
	}
	t.Run("rejects a body that is only the editor's empty markup", func(t *testing.T) {
		d := testDefinition()
		d["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)["body"] = `<p class="fb-editor-paragraph"><br></p>`
		_, issues := ParseExecutable(raw(t, d))
		failsWith(t, issues, "missing body")
	})
	for field, limit := range map[string]int{"subject": 998, "body": 100_000} {
		t.Run("bounds the "+field+", drafts included", func(t *testing.T) {
			d := testDefinition()
			cfg := d["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)
			cfg[field] = strings.Repeat("a", limit)
			if _, issues := ParseDefinition(raw(t, d)); len(issues) > 0 {
				t.Fatalf("at the limit: %+v", issues)
			}
			cfg[field] = strings.Repeat("a", limit+1)
			if _, issues := ParseDefinition(raw(t, d)); len(issues) == 0 {
				t.Fatal("over the limit accepted")
			}
		})
	}
	t.Run("allows a draft whose send_email is incomplete", func(t *testing.T) {
		d := testDefinition()
		cfg := d["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)
		cfg["to"], cfg["subject"], cfg["body"] = "", "", ""
		if _, issues := ParseDefinition(raw(t, d)); len(issues) > 0 {
			t.Fatalf("%+v", issues)
		}
	})
	t.Run("rejects more than one edge out of the trigger", func(t *testing.T) {
		d := testDefinition()
		d["edges"] = append(d["edges"].([]any), map[string]any{"id": "trigger-send-email-2", "source": "trigger", "target": "send-email"})
		_, issues := ParseDefinition(raw(t, d))
		failsWith(t, issues, "at most one outgoing trigger edge")
	})
	t.Run("rejects an executable definition without an edge out of the trigger", func(t *testing.T) {
		d := testDefinition()
		d["nodes"], d["edges"] = []any{}, []any{}
		_, issues := ParseExecutable(raw(t, d))
		failsWith(t, issues, "exactly one outgoing trigger edge")
	})
	condition := ifElseNode("condition",
		map[string]any{"id": "email-exists", "left": map[string]any{"path": "response.email"}, "operator": "exists"},
		map[string]any{"id": "nested-group", "connector": "or", "conditions": []any{
			map[string]any{"id": "score-high", "left": map[string]any{"path": "response.score"}, "operator": "greaterThan", "right": 8},
			map[string]any{"id": "plan-match", "left": map[string]any{"path": "response.plan"}, "operator": "equals", "right": map[string]any{"path": "contact.plan"}},
		}})
	t.Run("allows if_else in stored definitions but not executable ones", func(t *testing.T) {
		d := testDefinition()
		d["nodes"] = append(d["nodes"].([]any), condition, emailNode("then-email"), emailNode("else-email"))
		d["edges"] = append(d["edges"].([]any),
			map[string]any{"id": "send-email-condition", "source": "send-email", "target": "condition"},
			map[string]any{"id": "condition-then", "source": "condition", "target": "then-email", "sourceHandle": "then"},
			map[string]any{"id": "condition-else", "source": "condition", "target": "else-email", "sourceHandle": "else"})
		if _, issues := ParseDefinition(raw(t, d)); len(issues) > 0 {
			t.Fatalf("%+v", issues)
		}
		_, issues := ParseExecutable(raw(t, d))
		failsWith(t, issues, "if_else nodes are not executable in this version of workflows")
	})
	t.Run("rejects unreachable nodes in executable definitions", func(t *testing.T) {
		d := testDefinition()
		d["nodes"] = append(d["nodes"].([]any), emailNode("orphan-email"))
		_, issues := ParseExecutable(raw(t, d))
		failsWith(t, issues, "unreachable node ids: orphan-email")
	})
	t.Run("rejects cycles in executable definitions", func(t *testing.T) {
		d := testDefinition()
		d["nodes"] = append(d["nodes"].([]any), emailNode("second-email"))
		d["edges"] = []any{
			map[string]any{"id": "e1", "source": "trigger", "target": "send-email"},
			map[string]any{"id": "e2", "source": "send-email", "target": "second-email"},
			map[string]any{"id": "e3", "source": "second-email", "target": "send-email"},
		}
		_, issues := ParseExecutable(raw(t, d))
		failsWith(t, issues, "acyclic")
	})
	t.Run("rejects if_else edges without branch handles", func(t *testing.T) {
		d := testDefinition()
		d["nodes"] = append(d["nodes"].([]any), ifElseNode("condition", map[string]any{"id": "c", "left": map[string]any{"path": "response.email"}, "operator": "exists"}), emailNode("then-email"))
		d["edges"] = append(d["edges"].([]any),
			map[string]any{"id": "send-email-condition", "source": "send-email", "target": "condition"},
			map[string]any{"id": "condition-then", "source": "condition", "target": "then-email"})
		_, issues := ParseDefinition(raw(t, d))
		failsWith(t, issues, "then or else sourceHandle")
		failsWith(t, issues, "must have exactly one then edge")
	})
	t.Run("rejects branch handles on other nodes", func(t *testing.T) {
		d := testDefinition()
		d["edges"] = []any{map[string]any{"id": "e", "source": "trigger", "target": "send-email", "sourceHandle": "then"}}
		_, issues := ParseDefinition(raw(t, d))
		failsWith(t, issues, "Only if_else nodes")
	})
	t.Run("rejects duplicate node ids", func(t *testing.T) {
		d := testDefinition()
		d["nodes"] = append(d["nodes"].([]any), emailNode("trigger"))
		_, issues := ParseDefinition(raw(t, d))
		failsWith(t, issues, "Duplicate workflow node id: trigger")
	})
	t.Run("rejects other action types", func(t *testing.T) {
		d := testDefinition()
		d["nodes"] = []any{map[string]any{"id": "webhook", "type": "action", "actionType": "sendWebhookPreview", "config": map[string]any{"url": "https://example.com"}}}
		_, issues := ParseDefinition(raw(t, d))
		if len(issues) != 1 || issues[0].Name() != "nodes.0.actionType" {
			t.Fatalf("%+v", issues)
		}
	})
	t.Run("validates condition right-hand values by operator", func(t *testing.T) {
		for _, tc := range []struct {
			condition map[string]any
			message   string
		}{
			{map[string]any{"id": "c", "left": map[string]any{"path": "response.email"}, "operator": "exists"}, ""},
			{map[string]any{"id": "c", "left": map[string]any{"path": "response.email"}, "operator": "exists", "right": ""}, "must not have a right-hand value"},
			{map[string]any{"id": "c", "left": map[string]any{"path": "response.email"}, "operator": "equals"}, "requires a right-hand value"},
			{map[string]any{"id": "c", "left": map[string]any{"path": "response.email"}, "operator": "equals", "right": nil}, ""},
		} {
			p := &parser{}
			p.condition(tc.condition, nil)
			if tc.message == "" && len(p.issues) > 0 {
				t.Fatalf("%v: %+v", tc.condition, p.issues)
			}
			if tc.message != "" {
				failsWith(t, p.issues, tc.message)
			}
		}
	})
	t.Run("defaults the ending ids to every ending and checks their shape", func(t *testing.T) {
		d := testDefinition()
		delete(d["trigger"].(map[string]any)["config"].(map[string]any), "endingCardIds")
		parsed, issues := ParseDefinition(raw(t, d))
		if len(issues) > 0 || parsed.Trigger.Config.EndingCardIDs == nil || len(parsed.Trigger.Config.EndingCardIDs) != 0 {
			t.Fatalf("%+v", issues)
		}
		d["trigger"].(map[string]any)["config"].(map[string]any)["endingCardIds"] = []any{"Not An Id"}
		_, issues = ParseDefinition(raw(t, d))
		failsWith(t, issues, "Invalid cuid2")
	})
	t.Run("refuses a cuid2 trigger survey id", func(t *testing.T) {
		d := testDefinition()
		d["trigger"].(map[string]any)["config"].(map[string]any)["surveyId"] = "cm9zr4mps000008l8btfy1vtz"
		_, issues := ParseExecutable(raw(t, d))
		failsWith(t, issues, "Invalid UUID")
	})
	t.Run("needs the trigger's survey to be a UUID and the sender an address", func(t *testing.T) {
		d := testDefinition()
		d["trigger"].(map[string]any)["config"].(map[string]any)["surveyId"] = "Not-A-Survey"
		d["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)["from"] = "nobody"
		_, issues := ParseDefinition(raw(t, d))
		if len(issues) != 2 || issues[0].Name() != "trigger.config.surveyId" || issues[0].Message != "Invalid UUID" || issues[1].Name() != "nodes.0.config.from" || issues[1].Message != "Invalid email address" {
			t.Fatalf("%+v", issues)
		}
	})
	t.Run("reports type errors with zod's messages and skips the graph rules", func(t *testing.T) {
		d := testDefinition()
		d["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)["attachResponseData"] = "yes"
		d["edges"] = []any{map[string]any{"id": "e", "source": "trigger", "target": "nowhere"}}
		_, issues := ParseDefinition(raw(t, d))
		if len(issues) != 1 || issues[0].Name() != "nodes.0.config.attachResponseData" || issues[0].Message != "Invalid input: expected boolean, received string" {
			t.Fatalf("%+v", issues)
		}
	})
	t.Run("caps the schema version", func(t *testing.T) {
		d := testDefinition()
		d["schemaVersion"] = 2
		_, issues := ParseDefinition(raw(t, d))
		failsWith(t, issues, "Too big: expected number to be <=1")
	})
}

func TestBlankRichText(t *testing.T) {
	for _, blank := range []string{"", "   \n ", "<p><br></p>", `<p class="fb-editor-paragraph" dir="ltr"><br></p>`, "<p></p><p></p>", "<p>&nbsp;</p>", "<p>&#160;&#xA0;</p>"} {
		if !IsBlankRichText(blank) {
			t.Errorf("%q is not blank", blank)
		}
	}
	for _, filled := range []string{"<p>Thanks!</p>", "Hi there", `<p><span style="font-weight: bold">x</span></p>`, "<p><3</p>", "<p>5 < 6</p>", "<p>a > b</p>", "<p><b</p>", "<p>a <b c</p>",
		`<p><span data-recall-id="abc" data-fallback-value="there">#recall:abc/fallback:there#</span></p>`} {
		if IsBlankRichText(filled) {
			t.Errorf("%q is blank", filled)
		}
	}
	start := time.Now()
	if IsBlankRichText(strings.Repeat("<A", 100_000)) {
		t.Error("unclosed tags read as blank")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("stripping took %v", elapsed)
	}
	filled := SendEmailConfig{To: "user@example.com", Subject: "Hello", Body: "<p>Hi</p>"}
	if got := BlankContentFields(filled); len(got) != 0 {
		t.Errorf("filled: %v", got)
	}
	if got := BlankContentFields(SendEmailConfig{Subject: " "}); !slices.Equal(got, []string{"to", "subject", "body"}) {
		t.Errorf("blank: %v", got)
	}
	withMarkupTo := filled
	withMarkupTo.To = "<p><br></p>"
	if got := BlankContentFields(withMarkupTo); len(got) != 0 {
		t.Errorf("a to is never markup: %v", got)
	}
}

func TestPlan(t *testing.T) {
	plan := func(d map[string]any) ([]Step, error) {
		t.Helper()
		def, _ := ParseDefinition(raw(t, d))
		if def == nil {
			t.Fatalf("definition does not parse: %v", d)
		}
		return Plan(def)
	}
	steps, err := plan(testDefinition())
	if err != nil || len(steps) != 1 || steps[0].ID != "send-email" || steps[0].Type != ActionSendEmail {
		t.Fatalf("%+v %v", steps, err)
	}
	chain := testDefinition()
	chain["nodes"] = []any{emailNode("a"), emailNode("b")}
	chain["edges"] = []any{map[string]any{"id": "e1", "source": "trigger", "target": "a"}, map[string]any{"id": "e2", "source": "a", "target": "b"}}
	if steps, err := plan(chain); err != nil || len(steps) != 2 || steps[0].ID != "a" || steps[1].ID != "b" {
		t.Fatalf("chain: %+v %v", steps, err)
	}
	empty := testDefinition()
	empty["nodes"], empty["edges"] = []any{}, []any{}
	if steps, err := plan(empty); err != nil || len(steps) != 0 {
		t.Fatalf("empty: %+v %v", steps, err)
	}
	branching := testDefinition()
	branching["nodes"] = []any{emailNode("a"), emailNode("b")}
	branching["edges"] = []any{map[string]any{"id": "e1", "source": "a", "target": "b"}, map[string]any{"id": "e2", "source": "a", "target": "trigger"}, map[string]any{"id": "e3", "source": "trigger", "target": "a"}}
	var planErr *PlanError
	if _, err := plan(branching); !errors.As(err, &planErr) || !strings.Contains(err.Error(), "more than one outgoing edge") {
		t.Fatalf("branching: %v", err)
	}
	missing := &Definition{Trigger: &TriggerNode{ID: "trigger"}, Edges: []Edge{{ID: "e1", Source: "trigger", Target: "ghost"}}}
	if _, err := Plan(missing); !errors.As(err, &planErr) || !strings.Contains(err.Error(), "missing node: ghost") {
		t.Fatalf("missing: %v", err)
	}
	branch := &Definition{Trigger: &TriggerNode{ID: "trigger"}, Nodes: []Node{{ID: "x", Type: NodeIfElse}}, Edges: []Edge{{ID: "e1", Source: "trigger", Target: "x"}}}
	if _, err := Plan(branch); err == nil || !strings.Contains(err.Error(), "not an executable send_email action") {
		t.Fatalf("if_else: %v", err)
	}
}

func TestLiteralRecipients(t *testing.T) {
	d := &Definition{Nodes: []Node{
		{Type: NodeAction, Email: &SendEmailConfig{To: "a@example.com"}},
		{Type: NodeAction, Email: &SendEmailConfig{To: "q1"}},
		{Type: NodeAction, Email: &SendEmailConfig{To: "a@example.com"}},
		{Type: NodeAction, Email: &SendEmailConfig{To: "B@example.com"}},
	}}
	if got := LiteralRecipients(d); !slices.Equal(got, []string{"a@example.com", "B@example.com"}) {
		t.Fatalf("%v", got)
	}
}
