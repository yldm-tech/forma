package surveydoc

import (
	"encoding/json"
	"testing"

	"github.com/yldm-tech/forma/server/internal/jsonschema"
)

func newValidator(t *testing.T) *Validator {
	t.Helper()
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	return NewValidator(schemas)
}

func validDocument() Document {
	return Document{
		WelcomeCard:  json.RawMessage(`{"enabled": false}`),
		Blocks:       json.RawMessage(`[{"id": "blocka", "name": "Block 1", "elements": [{"id": "q1", "type": "openText", "headline": {"default": "Q"}, "required": true}]}]`),
		Endings:      json.RawMessage(`[]`),
		HiddenFields: json.RawMessage(`{"enabled": false}`),
		Variables:    json.RawMessage(`[]`),
		Metadata:     json.RawMessage(`{}`),
	}
}

func TestCheckAcceptsAValidDocument(t *testing.T) {
	v := newValidator(t)
	is, s, err := v.Check(validDocument(), nil, nil)
	if err != nil || len(is) > 0 || s == nil || len(s.Blocks) != 1 {
		t.Fatalf("Check = %v, %v, %v", is, s, err)
	}
}

func TestCheckReportsStructureBeforeRefinements(t *testing.T) {
	v := newValidator(t)
	d := validDocument()
	d.Blocks = json.RawMessage(`[{"id": "Block One", "name": "Block 1", "elements": []}]`)
	d.Recaptcha = json.RawMessage(`{"enabled": true, "threshold": 2}`)
	is, s, err := v.Check(d, nil, nil)
	if err != nil || s != nil || len(is) == 0 {
		t.Fatalf("Check = %v, %v, %v", is, s, err)
	}
	names := map[string]bool{}
	for _, i := range is {
		names[i.Name()] = true
	}
	for _, want := range []string{"blocks.0.id", "blocks.0.elements", "recaptcha.threshold"} {
		if !names[want] {
			t.Errorf("no structure issue at %s among %v", want, is)
		}
	}
}

func TestCheckRunsFieldAndCrossFieldRulesTogether(t *testing.T) {
	v := newValidator(t)
	d := validDocument()
	d.Blocks = json.RawMessage(`[{"id": "blocka", "name": "Block 1", "elements": [{"id": "q1", "type": "openText", "headline": {"en": "Q"}, "required": true}]}]`)
	is, _, err := v.Check(d, []Language{{Code: "en", Default: true, Enabled: true}, {Code: "de", Enabled: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectIssue(t, is, i18nDefaultMessage, "blocks.0.elements.0.headline")
	expectIssue(t, is, "The question in question 1 of block 1 is missing", "blocks.0.elements.0.headline")
}

func TestCheckValidatesFollowUpStructure(t *testing.T) {
	v := newValidator(t)
	followUps := []FollowUpInput{{ID: "f", Name: "f", Trigger: json.RawMessage(`{"type": "response", "properties": null}`), Action: json.RawMessage(`{"type": "send-email", "properties": {"to": "x@y.co", "from": "not-an-email", "replyTo": [], "subject": "s", "body": "b", "attachResponseData": false}}`)}}
	is, _, err := v.Check(validDocument(), nil, followUps)
	if err != nil || len(is) == 0 || is[0].Name() != "followUps.0.action.properties.from" {
		t.Fatalf("Check = %v, %v", is, err)
	}
}

func TestQuotaIssues(t *testing.T) {
	v := newValidator(t)
	ending := "endone"
	empty := ""
	valid := Quota{Name: "Q", Limit: 10, Logic: json.RawMessage(`{"connector": "and", "conditions": []}`), Action: "endSurvey", EndingCardID: &ending}
	is, err := v.QuotaIssues(valid)
	if err != nil || len(is) > 0 {
		t.Fatalf("valid quota: %v, %v", is, err)
	}
	cases := []struct {
		name    string
		mutate  func(q *Quota)
		message string
		path    string
	}{
		{"no name", func(q *Quota) { q.Name = "" }, "Quota name is required", "name"},
		{"a zero limit", func(q *Quota) { q.Limit = 0 }, "Limit must be greater than 0", "limit"},
		{"no ending for endSurvey", func(q *Quota) { q.EndingCardID = &empty }, "endingCardId is required when action is endSurvey", "action"},
		{"a condition without its right operand", func(q *Quota) {
			q.Logic = json.RawMessage(`{"connector": "or", "conditions": [{"id": "c", "leftOperand": {"type": "element", "value": "q1"}, "operator": "equals"}]}`)
		}, `Conditional Logic: right operand is required for operator "equals"`, "logic.conditions.0.rightOperand"},
	}
	for _, tc := range cases {
		q := valid
		tc.mutate(&q)
		is, err := v.QuotaIssues(q)
		if err != nil {
			t.Fatal(err)
		}
		expectIssue(t, is, tc.message, tc.path)
	}
	q := valid
	q.Action, q.EndingCardID = "continueSurvey", nil
	if is, _ := v.QuotaIssues(q); len(is) > 0 {
		t.Errorf("continueSurvey without an ending: %v", is)
	}
}

func TestMediaIssue(t *testing.T) {
	block := func(el string) []Block {
		var b []Block
		if err := json.Unmarshal([]byte(`[{"id": "b", "name": "Intro", "elements": [`+el+`]}]`), &b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	if m := MediaIssue(block(`{"id": "q", "type": "openText", "headline": {"default": "Q"}, "required": false, "imageUrl": "https://cdn.example.com/photo?auto=format"}`)); m == "" || m[:42] != `Invalid image URL in question 1 of block "` {
		t.Errorf("image: %q", m)
	}
	if m := MediaIssue(block(`{"id": "q", "type": "openText", "headline": {"default": "Q"}, "required": false, "videoUrl": "https://example.com/v.mp4"}`)); m != `Invalid video URL in question 1 of block "Intro" (block 1). Only YouTube, Vimeo, and Loom URLs are supported.` {
		t.Errorf("video: %q", m)
	}
	if m := MediaIssue(block(`{"id": "q", "type": "pictureSelection", "headline": {"default": "Q"}, "required": false, "choices": [{"id": "a", "imageUrl": "/storage/a.png"}, {"id": "b", "imageUrl": "/storage/b.gif"}]}`)); m == "" || m[:52] != `Invalid image URL in choice 2 of question 1 of block` {
		t.Errorf("picture: %q", m)
	}
	if m := MediaIssue(block(`{"id": "q", "type": "openText", "headline": {"default": "Q"}, "required": false, "imageUrl": "/storage/ws/x.png", "videoUrl": "https://youtu.be/abc"}`)); m != "" {
		t.Errorf("valid media: %q", m)
	}
}
