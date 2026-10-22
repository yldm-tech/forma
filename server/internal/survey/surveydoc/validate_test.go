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

// logicDocument is a valid document whose second block carries one logic rule with the given condition and action, next to a number variable "vara".
func logicDocument(condition, action string) Document {
	d := validDocument()
	d.Blocks = json.RawMessage(`[
	  {"id": "blocka", "name": "First", "elements": [{"id": "q1", "type": "openText", "headline": {"default": "Q"}, "required": false}]},
	  {"id": "blockb", "name": "Second", "elements": [{"id": "q2", "type": "openText", "headline": {"default": "Q"}, "required": false}],
	   "logic": [{"id": "logica", "conditions": {"id": "groupa", "connector": "and", "conditions": [` + condition + `]}, "actions": [` + action + `]}]},
	  {"id": "blockc", "name": "Third", "elements": [{"id": "q3", "type": "openText", "headline": {"default": "Q"}, "required": false}]}
	]`)
	d.Variables = json.RawMessage(`[{"id": "vara", "name": "score", "type": "number", "value": 0}]`)
	return d
}

// forma2 parses elements, endings, choices, operands and actions with non-strict zod objects, which strip every key the matching variant does not declare. Such a key is neither validated nor able to fail the save, whatever its value.
func TestCheckIgnoresKeysTheVariantDoesNotDeclare(t *testing.T) {
	v := newValidator(t)
	element := func(el string) Document {
		d := validDocument()
		d.Blocks = json.RawMessage(`[{"id": "blocka", "name": "Block 1", "elements": [` + el + `]}]`)
		return d
	}
	endings := func(endings string) Document {
		d := validDocument()
		d.Endings = json.RawMessage(endings)
		return d
	}
	jump := `{"id": "acta", "objective": "jumpToBlock", "target": "blockc"}`
	cases := map[string]Document{
		"an open text with a consent label of another type":     element(`{"id": "q1", "type": "openText", "headline": {"default": "Q"}, "required": true, "label": "x"}`),
		"an open text with choices that have no default label":  element(`{"id": "q1", "type": "openText", "headline": {"default": "Q"}, "required": true, "choices": [{"id": "a", "label": {"en": "x"}}]}`),
		"an open text with matrix rows and address fields":      element(`{"id": "q1", "type": "openText", "headline": {"default": "Q"}, "required": true, "rows": [{"id": "r", "label": {"en": "x"}}], "city": {"show": true, "required": false, "placeholder": {"en": "x"}}, "html": {"en": "x"}}`),
		"a picture choice with a label of another type":         element(`{"id": "q1", "type": "pictureSelection", "headline": {"default": "Q"}, "required": true, "choices": [{"id": "a", "imageUrl": "https://example.com/a.png", "label": "x"}, {"id": "b", "imageUrl": "https://example.com/b.png"}]}`),
		"a choice with an image URL of another type":            element(`{"id": "q1", "type": "multipleChoiceSingle", "headline": {"default": "Q"}, "required": true, "choices": [{"id": "a", "label": {"default": "A"}, "imageUrl": 5}, {"id": "b", "label": {"default": "B"}}]}`),
		"an end screen with a redirect label of another type":   endings(`[{"id": "endone", "type": "endScreen", "headline": {"default": "Thanks"}, "label": 5}]`),
		"a redirect with an end screen's button link":           endings(`[{"id": "endone", "type": "redirectToUrl", "url": "https://example.com", "label": "Go", "buttonLink": "ftp://x", "imageUrl": "javascript:x"}]`),
		"a variable operand with an element's meta":             logicDocument(`{"id": "conda", "leftOperand": {"type": "variable", "value": "vara", "meta": {"row": 1}}, "operator": "equals", "rightOperand": {"type": "static", "value": 1}}`, jump),
		"a requireAnswer action with a calculate value":         logicDocument(`{"id": "conda", "leftOperand": {"type": "element", "value": "q1"}, "operator": "isSubmitted"}`, `{"id": "acta", "objective": "requireAnswer", "target": "q3", "value": "x", "variableId": 5}`),
		"a jumpToBlock action with a calculate value of a type": logicDocument(`{"id": "conda", "leftOperand": {"type": "element", "value": "q1"}, "operator": "isSubmitted"}`, `{"id": "acta", "objective": "jumpToBlock", "target": "blockc", "value": "x"}`),
		"a group with a single condition's operands":            logicDocument(`{"id": "groupb", "connector": "or", "leftOperand": "x", "conditions": [{"id": "conda", "leftOperand": {"type": "element", "value": "q1"}, "operator": "isSubmitted"}]}`, jump),
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			is, s, err := v.Check(d, nil, nil)
			if err != nil || s == nil {
				t.Fatalf("Check = %v, %v", is, err)
			}
			expectNoIssues(t, is)
		})
	}
}

// ZSingleCondition is tried before ZConditionGroup and strips "conditions": an object without a connector is a single condition, whatever other keys it carries, and its logic is validated.
func TestCheckTreatsAConditionWithoutAConnectorAsSingle(t *testing.T) {
	v := newValidator(t)
	d := logicDocument(`{"id": "conda", "leftOperand": {"type": "element", "value": "nope"}, "operator": "equals", "rightOperand": {"type": "static", "value": "x"}, "conditions": []}`, `{"id": "acta", "objective": "jumpToBlock", "target": "blockc"}`)
	is, _, err := v.Check(d, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectIssue(t, is, "Conditional Logic: Element Id nope does not exist in logic no: 1 of block 2", "blocks.1.logic.0.conditions")
}

// ZSurveyBase's ZStorageUrl and background refinements on styling and metadata, which the exported schemas cannot express.
func TestCheckValidatesStylingAndMetadataURLs(t *testing.T) {
	v := newValidator(t)
	d := validDocument()
	d.Styling = json.RawMessage(`{"logo": {"url": "javascript:alert(1)"}, "background": {"bgType": "upload", "bg": ""}}`)
	d.Metadata = json.RawMessage(`{"ogImage": "javascript:alert(1)"}`)
	is, _, err := v.Check(d, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectIssue(t, is, StorageURLMessage, "styling.logo.url")
	expectIssue(t, is, "Invalid background", "styling.background")
	expectIssue(t, is, StorageURLMessage, "metadata.ogImage")

	d.Styling = json.RawMessage(`{"logo": {"url": "/storage/ws/logo.png"}, "background": {"bgType": "upload", "bg": "/storage/ws/bg.png"}}`)
	d.Metadata = json.RawMessage(`{"ogImage": "https://example.com/og.png"}`)
	is, _, err = v.Check(d, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectNoIssues(t, is)

	d.Styling = json.RawMessage(`{"logo": null, "background": {"bgType": "color", "bg": null}}`)
	is, _, err = v.Check(d, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectNoIssues(t, is)
}
