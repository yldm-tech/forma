package surveydoc

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type fixture struct {
	WelcomeCard  WelcomeCard  `json:"welcomeCard"`
	Blocks       []Block      `json:"blocks"`
	Endings      []Ending     `json:"endings"`
	HiddenFields HiddenFields `json:"hiddenFields"`
	Variables    []Variable   `json:"variables"`
	Languages    []Language   `json:"languages"`
	FollowUps    []FollowUp   `json:"followUps"`
}

func load(t *testing.T, doc string) *Survey {
	t.Helper()
	var f fixture
	if err := json.Unmarshal([]byte(doc), &f); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return &Survey{WelcomeCard: f.WelcomeCard, Blocks: f.Blocks, Endings: f.Endings, HiddenFields: f.HiddenFields, Variables: f.Variables, Languages: f.Languages, FollowUps: f.FollowUps}
}

func messages(is []Issue) []string {
	out := []string{}
	for _, i := range is {
		out = append(out, i.Message)
	}
	return out
}

func hasMessage(is []Issue, want string) *Issue {
	for k := range is {
		if is[k].Message == want {
			return &is[k]
		}
	}
	return nil
}

func expectIssue(t *testing.T, is []Issue, message, name string) *Issue {
	t.Helper()
	for k := range is {
		if is[k].Message == message && is[k].Name() == name {
			return &is[k]
		}
	}
	if i := hasMessage(is, message); i != nil {
		t.Fatalf("issue %q at %q, want %q", message, i.Name(), name)
	}
	t.Fatalf("no issue %q among %q", message, messages(is))
	return nil
}

func expectNoIssues(t *testing.T, is []Issue) {
	t.Helper()
	if len(is) > 0 {
		t.Fatalf("unexpected issues %q", messages(is))
	}
}

var enDe = []Language{{Code: "en", Default: true, Enabled: true}, {Code: "de", Enabled: true}}
var enDeFrEs = []Language{{Code: "en", Default: true, Enabled: true}, {Code: "de", Enabled: true}, {Code: "fr", Enabled: true}, {Code: "es", Enabled: true}}

// The cases of forma2's survey schema multilingual label validation tests (modules/survey/editor/lib/validation.test.ts).
func TestLabelIssues(t *testing.T) {
	i := cardLabelIssue("cardHeadline", I18nString{"default": "", "en": "Welcome", "de": "Willkommen"}, enDe, -1)
	if i == nil || i.Message != "The note on the Welcome card is missing" || i.Name() != "welcomeCard.cardHeadline" || i.Params != nil {
		t.Errorf("missing default welcome headline: %+v", i)
	}
	i = cardLabelIssue("cardHeadline", I18nString{"default": "Welcome", "en": "Welcome", "de": ""}, enDe, -1)
	if i == nil || i.Message != "The note on the Welcome card is missing for the following languages:  -fLang- de" || !reflect.DeepEqual(i.Params["invalidLanguageCodes"], []string{"de"}) {
		t.Errorf("missing translation: %+v", i)
	}
	i = cardLabelIssue("cardHeadline", I18nString{"default": "", "en": "", "de": ""}, enDe, 2)
	if i == nil || i.Message != "The note on the Ending card 3 is missing" || i.Name() != "endings.2.cardHeadline" {
		t.Errorf("ending headline: %+v", i)
	}
	i = cardLabelIssue("endingCardButtonLabel", I18nString{"default": "Done", "en": "Done", "de": "", "fr": " ", "es": "Listo"}, enDeFrEs, 1)
	if i == nil || i.Message != "The button label on the Ending card 2 is missing for the following languages:  -fLang- de, fr" || !reflect.DeepEqual(i.Params["invalidLanguageCodes"], []string{"de", "fr"}) {
		t.Errorf("ending button label: %+v", i)
	}
	i = elementLabelIssue("headline", I18nString{"default": "", "en": "", "de": ""}, enDe, 1, 0, false)
	if i == nil || i.Message != "The question in question 1 of block 2 is missing" || i.Name() != "blocks.1.elements.0.headline" || i.Params != nil {
		t.Errorf("element headline: %+v", i)
	}
	i = elementLabelIssue("headline", I18nString{"default": "Question", "en": "Question", "de": ""}, enDe, 1, 0, false)
	if i == nil || i.Message != "The question in question 1 of block 2 is missing for the following languages:  -fLang- de" {
		t.Errorf("element translation: %+v", i)
	}
	i = elementLabelIssue("headline", I18nString{"default": "Question"}, enDe, 0, 0, false)
	if i == nil || i.Message != "The headline in question 1 of block 1 is not present for the following languages: de" {
		t.Errorf("absent translation key: %+v", i)
	}
	i = elementLabelIssue("headline", I18nString{"default": "<p><br></p>"}, nil, 0, 0, false)
	if i == nil || i.Message != "The question in question 1 of block 1 is missing" {
		t.Errorf("empty markup counts as missing: %+v", i)
	}
	if i := elementLabelIssue("headline", I18nString{"default": "Q", "de": ""}, []Language{{Code: "en", Default: true, Enabled: true}, {Code: "de", Enabled: false}}, 0, 0, false); i != nil {
		t.Errorf("a disabled language is not checked: %+v", i)
	}
}

const twoBlocks = `{
  "blocks": [
    {"id": "blocka", "name": "Block 1", "elements": [
      {"id": "q1", "type": "openText", "headline": {"default": "Name?"}, "required": false},
      {"id": "q2", "type": "multipleChoiceSingle", "headline": {"default": "Pick"}, "required": true,
       "choices": [{"id": "c1", "label": {"default": "One"}}, {"id": "c2", "label": {"default": "Two"}}]}
    ]},
    {"id": "blockb", "name": "Block 2", "elements": [
      {"id": "q3", "type": "rating", "headline": {"default": "Rate"}, "required": false, "scale": "number", "range": 5}
    ]}
  ],
  "endings": [{"id": "endone", "type": "endScreen", "headline": {"default": "Thanks"}}],
  "hiddenFields": {"enabled": true, "fieldIds": ["plan"]},
  "variables": [{"id": "varscore", "name": "score", "type": "number", "value": 0}, {"id": "vartext", "name": "label", "type": "text", "value": ""}]
}`

func TestRefineAcceptsAValidSurvey(t *testing.T) {
	s := load(t, twoBlocks)
	expectNoIssues(t, FieldIssues(s))
	expectNoIssues(t, Refine(s))
}

func TestRefineRequiresBlocksWithElements(t *testing.T) {
	s := load(t, `{"blocks": []}`)
	expectIssue(t, Refine(s), "Survey must have either questions or blocks with elements", "questions")
}

func TestRefineElementRules(t *testing.T) {
	cases := []struct {
		name, element, message, path string
	}{
		{"duplicate choice labels", `{"id": "q", "type": "multipleChoiceMulti", "headline": {"default": "Q"}, "required": false, "choices": [{"id": "a", "label": {"default": " Same "}}, {"id": "b", "label": {"default": "Same"}}]}`,
			"Element 1 in block 1 has duplicate choice labels ", "blocks.0.elements.0.choices"},
		{"an empty choice label", `{"id": "q", "type": "ranking", "headline": {"default": "Q"}, "required": false, "choices": [{"id": "a", "label": {"default": ""}}, {"id": "b", "label": {"default": "B"}}]}`,
			"Choice 1 in question 1 of block 1 is missing", "blocks.0.elements.0.choices.0"},
		{"an empty consent label", `{"id": "q", "type": "consent", "headline": {"default": "Q"}, "required": false, "label": {"default": " "}}`,
			"The checkbox label in question 1 of block 1 is missing", "blocks.0.elements.0.label"},
		{"an external CTA without URL", `{"id": "q", "type": "cta", "headline": {"default": "Q"}, "required": false, "buttonExternal": true, "ctaButtonLabel": {"default": "Go"}}`,
			"Element 1 in block 1: Button URL is required when external button is enabled", "blocks.0.elements.0.buttonUrl"},
		{"an external CTA to plain http", `{"id": "q", "type": "cta", "headline": {"default": "Q"}, "required": false, "buttonExternal": true, "buttonUrl": "http://example.com", "ctaButtonLabel": {"default": "Go"}}`,
			"Element 1 in block 1: URL must start with https:// or mailto:", "blocks.0.elements.0.buttonUrl"},
		{"an external CTA without label", `{"id": "q", "type": "cta", "headline": {"default": "Q"}, "required": false, "buttonExternal": true, "buttonUrl": "https://example.com"}`,
			"The ctaButtonLabel in question 1 of block 1 is missing", "blocks.0.elements.0.ctaButtonLabel"},
		{"duplicate matrix rows", `{"id": "q", "type": "matrix", "headline": {"default": "Q"}, "required": false, "rows": [{"id": "r1", "label": {"default": "R"}}, {"id": "r2", "label": {"default": "R"}}], "columns": [{"id": "c1", "label": {"default": "C"}}]}`,
			"Question 1 in block 1 has duplicate row labels ", "blocks.0.elements.0.rows"},
		{"no allowed file extension", `{"id": "q", "type": "fileUpload", "headline": {"default": "Q"}, "required": false, "allowMultipleFiles": false, "allowedFileExtensions": []}`,
			"Question 1 in block 1 must have atleast one allowed file extension", "blocks.0.elements.0.allowedFileExtensions"},
		{"a bad cal host", `{"id": "q", "type": "cal", "headline": {"default": "Q"}, "required": false, "calUserName": "x", "calHost": "-bad.example.com"}`,
			"Question 1 in block 1 must have a valid host name", "blocks.0.elements.0.calHost"},
		{"a contact info with nothing shown", `{"id": "q", "type": "contactInfo", "headline": {"default": "Q"}, "required": false,
			"firstName": {"show": false, "required": false, "placeholder": {"default": "F"}}, "lastName": {"show": false, "required": false, "placeholder": {"default": "L"}},
			"email": {"show": false, "required": false, "placeholder": {"default": "E"}}, "phone": {"show": false, "required": false, "placeholder": {"default": "P"}},
			"company": {"show": false, "required": false, "placeholder": {"default": "C"}}}`,
			"At least one field must be shown in the Contact Info question 1 in block 1", "blocks.0.elements.0"},
		{"an address with a blank shown placeholder", `{"id": "q", "type": "address", "headline": {"default": "Q"}, "required": false,
			"addressLine1": {"show": true, "required": false, "placeholder": {"default": ""}}, "addressLine2": {"show": false, "required": false, "placeholder": {"default": "x"}},
			"city": {"show": false, "required": false, "placeholder": {"default": "x"}}, "state": {"show": false, "required": false, "placeholder": {"default": "x"}},
			"zip": {"show": false, "required": false, "placeholder": {"default": "x"}}, "country": {"show": false, "required": false, "placeholder": {"default": "x"}}}`,
			"Label for field Address Line 1 in question 1 of block 1 is missing", "blocks.0.elements.0.addressline1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := load(t, `{"blocks": [{"id": "blocka", "name": "B", "elements": [`+tc.element+`]}]}`)
			expectIssue(t, Refine(s), tc.message, tc.path)
		})
	}
}

func TestRefineDuplicateLabelsNameTheLanguages(t *testing.T) {
	s := load(t, `{"blocks": [{"id": "blocka", "name": "B", "elements": [{"id": "q", "type": "multipleChoiceSingle", "headline": {"default": "Q", "de": "Q"}, "required": false,
		"choices": [{"id": "a", "label": {"default": "A", "de": "X"}}, {"id": "b", "label": {"default": "A", "de": "X"}}]}]}],
		"languages": [{"code": "en", "default": true, "enabled": true}, {"code": "de", "default": false, "enabled": true}]}`)
	i := expectIssue(t, Refine(s), "Element 1 in block 1 has duplicate choice labels for the following languages:", "blocks.0.elements.0.choices")
	if !reflect.DeepEqual(i.Params["invalidLanguageCodes"], []string{"en", "de"}) {
		t.Errorf("params %v", i.Params)
	}
}

func TestRefineElementIDsAreUniqueAcrossBlocks(t *testing.T) {
	s := load(t, strings.Replace(twoBlocks, `"id": "q3"`, `"id": "q1"`, 1))
	expectIssue(t, Refine(s), `Element ID "q1" is used in multiple blocks. Element IDs must be unique across all blocks.`, "blocks.1.elements.0.id")
}

func TestRefineWelcomeCardAndEndings(t *testing.T) {
	s := load(t, `{"welcomeCard": {"enabled": true, "headline": {"default": "Hi"}, "subheader": {"default": "Sub"}},
		"blocks": [{"id": "blocka", "name": "B", "elements": [{"id": "q", "type": "openText", "headline": {"default": "Q", "de": "Q"}, "required": false}]}],
		"endings": [
			{"id": "enda", "type": "endScreen"},
			{"id": "endb", "type": "endScreen", "headline": {"default": "T", "de": "T"}, "buttonLink": "https://x.com"},
			{"id": "endc", "type": "endScreen", "headline": {"default": "T", "de": "T"}, "buttonLabel": {"default": "Go", "de": "Los"}, "buttonLink": "x.com"},
			{"id": "endd", "type": "redirectToUrl", "url": " "},
			{"id": "ende", "type": "redirectToUrl", "label": "L", "url": "ftp://x"}
		],
		"languages": [{"code": "en", "default": true, "enabled": true}, {"code": "de", "default": false, "enabled": true}]}`)
	is := Refine(s)
	expectIssue(t, is, "The cardHeadline in Welcome card is not present for the following languages: de", "welcomeCard.cardHeadline")
	expectIssue(t, is, "The welcomeCardSubheader in Welcome card is not present for the following languages: de", "welcomeCard.welcomeCardSubheader")
	expectIssue(t, is, "The note on the Ending card 1 is missing", "endings.0.cardHeadline")
	expectIssue(t, is, "Ending card 2: Button label cannot be empty", "endings.1.buttonLabel")
	expectIssue(t, is, "Ending card 3: URL must start with http:// or https://", "endings.2.buttonLink")
	expectIssue(t, is, "Redirect Url label cannot be empty for ending Card 4.", "endings.3.label")
	expectIssue(t, is, "Ending card 4: Redirect URL cannot be empty", "endings.3.url")
	expectIssue(t, is, "Ending card 5: URL must start with http:// or https://", "endings.4.url")
}

func TestRefineFollowUps(t *testing.T) {
	s := load(t, `{"blocks": [{"id": "blocka", "name": "B", "elements": [
		{"id": "mail", "type": "openText", "inputType": "email", "headline": {"default": "Mail"}, "required": false},
		{"id": "free", "type": "openText", "headline": {"default": "Text"}, "required": false}]}],
		"hiddenFields": {"enabled": true, "fieldIds": ["hiddenmail"]},
		"followUps": [
			{"id": "f0", "name": "deleted", "deleted": true, "trigger": {"type": "response", "properties": null}, "action": {"type": "send-email", "properties": {"to": "free"}}},
			{"id": "f1", "name": "a", "trigger": {"type": "response", "properties": null}, "action": {"type": "send-email", "properties": {"to": "mail"}}},
			{"id": "f2", "name": "b", "trigger": {"type": "response", "properties": null}, "action": {"type": "send-email", "properties": {"to": "free"}}},
			{"id": "f3", "name": "c", "trigger": {"type": "endings", "properties": {"endingIds": []}}, "action": {"type": "send-email", "properties": {"to": "someone@example.com"}}},
			{"id": "f4", "name": "d", "trigger": {"type": "response", "properties": null}, "action": {"type": "send-email", "properties": {"to": "hiddenmail"}}}
		]}`)
	is := Refine(s)
	expectIssue(t, is, "The action in follow up 2 has an invalid email field", "followUps")
	expectIssue(t, is, "The trigger in follow up 3 has no ending selected", "followUps")
	if len(is) != 2 {
		t.Errorf("issues %q", messages(is))
	}
}

func TestFieldIssues(t *testing.T) {
	s := load(t, `{
		"welcomeCard": {"enabled": true, "fileUrl": "javascript:alert(1)"},
		"blocks": [
			{"id": "blocka", "name": "B", "buttonLabel": {"en": "Next"}, "elements": [
				{"id": "end", "type": "openText", "headline": {"default": "Q"}, "required": false, "charLimit": {"enabled": true}},
				{"id": "my id", "type": "openText", "headline": {"en": "Q"}, "required": false, "charLimit": {"enabled": true, "min": 5, "max": 2}},
				{"id": "q", "type": "cta", "headline": {"default": "Q"}, "required": false, "buttonExternal": true, "buttonUrl": "javascript:alert(1)"},
				{"id": "q", "type": "openText", "headline": {"default": "Q"}, "required": false, "charLimit": {"min": -1}}
			], "logic": [{"id": "logica", "conditions": {"id": "groupa", "connector": "and", "conditions": [
				{"id": "c1", "leftOperand": {"type": "element", "value": "end"}, "operator": "equals"},
				{"id": "c2", "leftOperand": {"type": "element", "value": "end"}, "operator": "isSubmitted", "rightOperand": {"type": "static", "value": "x"}},
				{"id": "groupb", "connector": "or", "conditions": [{"id": "c3", "leftOperand": {"type": "element", "value": "end"}, "operator": "contains", "rightOperand": {"type": "static", "value": ""}}]}
			]}, "actions": [
				{"id": "a1", "objective": "calculate", "variableId": "varscore", "operator": "divide", "value": {"type": "static", "value": 0}},
				{"id": "a2", "objective": "requireAnswer", "target": "has space"},
				{"id": "a3", "objective": "jumpToBlock", "target": "Not-A-Cuid"}
			]}]},
			{"id": "blocka", "name": "B2", "elements": [{"id": "z", "type": "openText", "headline": {"default": "Q"}, "required": false}]}
		],
		"endings": [{"id": "enda", "type": "endScreen", "imageUrl": "data:x"}, {"id": "enda", "type": "redirectToUrl", "url": "ftp://x"}],
		"hiddenFields": {"enabled": true, "fieldIds": ["verify", "with space", "bad!"]},
		"variables": [{"id": "v1", "name": "Score", "type": "number"}, {"id": "v1", "name": "Score", "type": "number"}],
		"followUps": [{"id": "f", "name": "f", "trigger": {"type": "endings", "properties": null}, "action": {"type": "send-email", "properties": {"to": "x"}}}]
	}`)
	is := FieldIssues(s)
	for _, want := range []struct{ message, path string }{
		{StorageURLMessage, "welcomeCard.fileUrl"},
		{"Welcome card must have a headline", "welcomeCard"},
		{"Element id is not allowed", "blocks.0.elements.0.id"},
		{"Enter the values for either minimum or maximum field", "blocks.0.elements.0"},
		{"Element id not allowed, avoid using spaces.", "blocks.0.elements.1.id"},
		{"Element id not allowed, use only alphanumeric characters, hyphens, or underscores.", "blocks.0.elements.1.id"},
		{i18nDefaultMessage, "blocks.0.elements.1.headline"},
		{"Minimum value cannot be greater than the maximum value", "blocks.0.elements.1"},
		{"Please enter a valid http(s), mailto: or tel: URL", "blocks.0.elements.2.buttonUrl"},
		{"Button label is required when external button is enabled", "blocks.0.elements.2.ctaButtonLabel"},
		{"The character limit values should be positive", "blocks.0.elements.3"},
		{`Conditional Logic: right operand is required for operator "equals"`, "blocks.0.logic.0.conditions.conditions.0.rightOperand"},
		{`Conditional Logic: right operand should not be present for operator "isSubmitted"`, "blocks.0.logic.0.conditions.conditions.1.rightOperand"},
		{`Conditional Logic: right operand value cannot be empty for operator "contains"`, "blocks.0.logic.0.conditions.conditions.2.conditions.0"},
		{"Conditional Logic: Cannot divide by zero", "blocks.0.logic.0.actions.0.value.value"},
		{"Conditional Logic: Target element id is not a valid element id", "blocks.0.logic.0.actions.1.target.target"},
		{"Conditional Logic: Target block id is not a valid block id", "blocks.0.logic.0.actions.2.target"},
		{i18nDefaultMessage, "blocks.0.buttonLabel"},
		{"Element IDs must be unique within a block", "blocks.0.3.id"},
		{"Block IDs must be unique", "blocks.1.id"},
		{StorageURLMessage, "endings.0.imageUrl"},
		{"URL must start with http:// or https://", "endings.1.url"},
		{"Ending IDs must be unique", "endings.1.id"},
		{"Hidden field id is not allowed", "hiddenFields.fieldIds.0"},
		{"Hidden field id not allowed, avoid using spaces.", "hiddenFields.fieldIds.1"},
		{"Hidden field id not allowed, use only alphanumeric characters, hyphens, or underscores.", "hiddenFields.fieldIds.2"},
		{"Variable name can only contain lowercase letters, numbers, and underscores", "variables.0.variables"},
		{"Variable IDs must be unique", "variables"},
		{"Variable names must be unique", "variables"},
		{"Properties must be defined for endings type", "followUps.0.trigger"},
	} {
		expectIssue(t, is, want.message, want.path)
	}
}

func TestCyclicLogic(t *testing.T) {
	jump := func(target string) string {
		return `"logic": [{"id": "l", "conditions": {"id": "g", "connector": "and", "conditions": []}, "actions": [{"id": "a", "objective": "jumpToBlock", "target": "` + target + `"}]}]`
	}
	block := func(id, extra string) string {
		if extra != "" {
			extra = ", " + extra
		}
		return `{"id": "` + id + `", "name": "Block ` + id + `", "elements": [{"id": "q` + id + `", "type": "openText", "headline": {"default": "Q"}, "required": false}]` + extra + `}`
	}
	cases := []struct {
		name   string
		blocks []string
		want   []string
	}{
		{"sequential flow", []string{block("a", ""), block("b", ""), block("c", "")}, nil},
		{"a forward jump", []string{block("a", jump("c")), block("b", ""), block("c", "")}, nil},
		{"a jump to an ending", []string{block("a", jump("endx")), block("b", "")}, nil},
		{"a backward jump", []string{block("a", ""), block("b", ""), block("c", jump("a"))}, []string{"c", "b", "a"}},
		{"a backward fallback", []string{block("a", ""), block("b", jump("endx")+`, "logicFallback": "a"`)}, []string{"b", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := load(t, `{"blocks": [`+strings.Join(tc.blocks, ",")+`], "endings": [{"id": "endx", "type": "endScreen", "headline": {"default": "T"}}]}`)
			if got := cyclicBlocks(s.Blocks); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("cyclic %v, want %v", got, tc.want)
			}
		})
	}
	s := load(t, `{"blocks": [`+block("a", "")+","+block("b", jump("a"))+`]}`)
	expectIssue(t, Refine(s), "Conditional Logic: Cyclic logic detected in block 2 (Block b).", "blocks.1.logic")
	expectIssue(t, Refine(s), "Conditional Logic: Cyclic logic detected in block 1 (Block a).", "blocks.0.logic")
}
