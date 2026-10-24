package surveydoc

import (
	"strings"
	"testing"
)

// logicSurvey is a survey whose second block carries one logic rule with the given conditions and actions; the first block holds one element of every kind the rules look at.
func logicSurvey(t *testing.T, conditions, actions string) *Survey {
	t.Helper()
	if actions == "" {
		actions = `{"id": "act", "objective": "jumpToBlock", "target": "endone"}`
	}
	doc := `{
	  "blocks": [
	    {"id": "blocka", "name": "First", "elements": [
	      {"id": "text", "type": "openText", "headline": {"default": "T"}, "required": false},
	      {"id": "num", "type": "openText", "inputType": "number", "headline": {"default": "N"}, "required": false},
	      {"id": "single", "type": "multipleChoiceSingle", "headline": {"default": "S"}, "required": true, "choices": [{"id": "c1", "label": {"default": "One"}}, {"id": "c2", "label": {"default": "Two"}}]},
	      {"id": "multi", "type": "multipleChoiceMulti", "headline": {"default": "M"}, "required": false, "choices": [{"id": "m1", "label": {"default": "One"}}, {"id": "m2", "label": {"default": "Two"}}]},
	      {"id": "nps", "type": "nps", "headline": {"default": "NPS"}, "required": false},
	      {"id": "rating", "type": "rating", "headline": {"default": "R"}, "required": false, "scale": "star", "range": 5},
	      {"id": "date", "type": "date", "headline": {"default": "D"}, "required": false, "format": "y-M-d"},
	      {"id": "matrix", "type": "matrix", "headline": {"default": "X"}, "required": false, "rows": [{"id": "r1", "label": {"default": "R1"}}], "columns": [{"id": "k1", "label": {"default": "K1"}}]},
	      {"id": "cta", "type": "cta", "headline": {"default": "C"}, "required": false},
	      {"id": "consent", "type": "consent", "headline": {"default": "C"}, "required": false, "label": {"default": "Yes"}}
	    ]},
	    {"id": "blockb", "name": "Second", "elements": [{"id": "here", "type": "openText", "headline": {"default": "H"}, "required": false}],
	     "logic": [{"id": "logica", "conditions": {"id": "group", "connector": "and", "conditions": [` + conditions + `]}, "actions": [` + actions + `]}]},
	    {"id": "blockc", "name": "Third", "elements": [
	      {"id": "later", "type": "openText", "headline": {"default": "L"}, "required": false},
	      {"id": "laterreq", "type": "openText", "headline": {"default": "L"}, "required": true}
	    ]}
	  ],
	  "endings": [{"id": "endone", "type": "endScreen", "headline": {"default": "Thanks"}}],
	  "hiddenFields": {"enabled": true, "fieldIds": ["plan"]},
	  "variables": [{"id": "varnum", "name": "score", "type": "number", "value": 0}, {"id": "vartext", "name": "label", "type": "text", "value": ""}]
	}`
	return load(t, doc)
}

func cond(left, op, right string) string {
	c := `{"id": "cond", "leftOperand": ` + left + `, "operator": "` + op + `"`
	if right != "" {
		c += `, "rightOperand": ` + right
	}
	return c + "}"
}

func el(id string) string       { return `{"type": "element", "value": "` + id + `"}` }
func static(v string) string    { return `{"type": "static", "value": ` + v + `}` }
func variable(id string) string { return `{"type": "variable", "value": "` + id + `"}` }

const where = " in logic no: 1 of block 2"

func TestBlockLogicConditions(t *testing.T) {
	cases := []struct {
		name, condition, message string
	}{
		{"an unknown element", cond(el("ghost"), "isSubmitted", ""), "Conditional Logic: Element Id ghost does not exist" + where},
		{"a later element", cond(el("later"), "isSubmitted", ""), "Conditional Logic: Block 2 cannot refer to an element in block 3 that appears later in the survey"},
		{"an operator the element type does not take", cond(el("text"), "isGreaterThan", static("3")), `Conditional Logic: Invalid operator "isGreaterThan" for element type "openText"` + where},
		{"skipping a required element", cond(el("single"), "isSkipped", ""), `Conditional Logic: Invalid operator "isSkipped" for element type "multipleChoiceSingle"` + where},
		{"a CTA without external button", cond(el("cta"), "isClicked", ""), `Conditional Logic: CTA element "cta" does not have an external button and cannot be used in logic conditions` + where},
		{"an open text compared with a choice element on a number input", cond(el("num"), "equals", el("single")), `Conditional Logic: Invalid element type "multipleChoiceSingle" for right operand` + where},
		{"an open text compared with an unknown variable", cond(el("text"), "equals", variable("nope")), "Conditional Logic: Variable ID nope does not exist" + where},
		{"an open text compared with an unknown hidden field", cond(el("text"), "equals", `{"type": "hiddenField", "value": "nope"}`), "Conditional Logic: Hidden field ID nope does not exist" + where},
		{"an open text compared with a zero", cond(el("num"), "equals", static("0")), "Conditional Logic: Static value is required" + where},
		{"a choice compared with a variable", cond(el("single"), "equals", variable("vartext")), `Conditional Logic: Right operand should be a static value for "equals"` + where},
		{"a choice compared with a non-string", cond(el("single"), "equals", static("1")), `Conditional Logic: Right operand should be a string for "equals"` + where},
		{"an unknown choice", cond(el("single"), "equals", static(`"c9"`)), `Conditional Logic: Choice "c9" does not exist in element 3 of block 1`},
		{"an include operator with a string", cond(el("multi"), "includesOneOf", static(`"m1"`)), `Conditional Logic: Right operand should be an array for "includesOneOf"` + where},
		{"an include operator with an unknown choice", cond(el("multi"), "includesAllOf", static(`["m1", "m9"]`)), "Conditional Logic: One or more choices selected in right operand do not exist in the element" + where},
		{"an NPS score out of range", cond(el("nps"), "isGreaterThan", static("11")), `Conditional Logic: NPS score should be between 0 and 10 for "isGreaterThan"` + where},
		{"a rating out of range", cond(el("rating"), "equals", static("6")), `Conditional Logic: Rating value should be between 1 and 5 for "equals"` + where},
		{"a rating compared with a text variable", cond(el("rating"), "equals", variable("vartext")), "Conditional Logic: Variable type should be number" + where},
		{"a rating compared with an element", cond(el("rating"), "equals", el("num")), `Conditional Logic: Right operand should be a variable or a static value for "equals"` + where},
		{"a date compared with a number variable", cond(el("date"), "isBefore", variable("varnum")), "Conditional Logic: Variable type should be text" + where},
		{"a date compared with a bad date", cond(el("date"), "isAfter", static(`"tomorrow-ish"`)), "Conditional Logic: Invalid date format for right operand" + where},
		{"a date compared with a choice element", cond(el("date"), "equals", el("single")), `Conditional Logic: Invalid element type "multipleChoiceSingle" for right operand` + where},
		{"a whole matrix with a value operator", cond(el("matrix"), "equals", static(`"x"`)), `Conditional Logic: Operator "equals" is not allowed in matrix element` + where},
		{"a matrix row out of range", cond(`{"type": "element", "value": "matrix", "meta": {"row": "3"}}`, "equals", static(`"k1"`)), "Conditional Logic: Invalid row index in matrix element" + where},
		{"a matrix row compared with a variable", cond(`{"type": "element", "value": "matrix", "meta": {"row": "0"}}`, "equals", variable("vartext")), "Conditional Logic: Right operand should be a static value in matrix element" + where},
		{"an unknown variable", cond(variable("nope"), "equals", static("1")), "Conditional Logic: Variable ID nope does not exist" + where},
		{"an unknown hidden field", cond(`{"type": "hiddenField", "value": "nope"}`, "isSet", ""), "Conditional Logic: Hidden field ID nope does not exist" + where},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := logicSurvey(t, tc.condition, "")
			expectIssue(t, Refine(s), tc.message, "blocks.1.logic.0.conditions")
		})
	}
}

func TestBlockLogicAcceptsValidConditions(t *testing.T) {
	for _, c := range []string{
		cond(el("text"), "contains", static(`"a"`)),
		cond(el("text"), "equals", el("date")),
		cond(el("num"), "isGreaterThan", el("rating")),
		cond(el("single"), "equals", static(`"c2"`)),
		cond(el("multi"), "includesAllOf", static(`["m1", "m2"]`)),
		cond(el("nps"), "isLessThan", variable("varnum")),
		cond(el("rating"), "equals", static("5")),
		cond(el("date"), "isBefore", static(`"2025-01-31"`)),
		cond(el("matrix"), "isPartiallySubmitted", ""),
		cond(`{"type": "element", "value": "matrix", "meta": {"row": "0"}}`, "equals", static(`"k1"`)),
		cond(el("here"), "isSubmitted", ""),
		cond(`{"type": "reserved", "value": "durationSeconds"}`, "isGreaterThan", static("30")),
		cond(`{"type": "reserved", "value": "notInAnyCatalogYet"}`, "isSet", ""),
		cond(`{"type": "hiddenField", "value": "plan"}`, "equals", static(`"pro"`)),
		cond(el("consent"), "isAccepted", ""),
	} {
		s := logicSurvey(t, c, "")
		expectNoIssues(t, Refine(s))
	}
}

func TestBlockLogicActions(t *testing.T) {
	const at = "blocks.1.logic.0"
	calc := func(variableID, operator, value string) string {
		return `{"id": "act", "objective": "calculate", "variableId": "` + variableID + `", "operator": "` + operator + `", "value": ` + value + `}`
	}
	cases := []struct {
		name, actions, message, path string
	}{
		{"an unknown variable", calc("ghost", "add", static("1")), "Conditional Logic: Variable ID ghost does not exist" + where, at},
		{"a variable of another type", calc("varnum", "add", variable("vartext")), "Conditional Logic: Invalid variable type for variable" + where, at},
		{"a text variable from a multi-choice element", calc("vartext", "assign", el("multi")), "Conditional Logic: Invalid element type for text variable" + where, at},
		{"a number variable from a text input", calc("varnum", "add", el("text")), "Conditional Logic: Invalid element type for number variable" + where, at},
		{"requiring an unknown element", `{"id": "act", "objective": "requireAnswer", "target": "ghost"}`, "Conditional Logic: Element ID ghost does not exist for requireAnswer action" + where, at},
		{"requiring an element of the same block", `{"id": "act", "objective": "requireAnswer", "target": "here"}`, "Conditional Logic: Element here cannot be in the current block for requireAnswer action" + where + ". RequireAnswer must target elements in other blocks.", at},
		{"requiring an earlier element", `{"id": "act", "objective": "requireAnswer", "target": "text"}`, "Conditional Logic: Element text is in a previous block (block 1). RequireAnswer should target elements in future blocks after block 2.", at},
		{"requiring a required element", `{"id": "act", "objective": "requireAnswer", "target": "laterreq"}`, "Conditional Logic: Element laterreq in block 3 is already required" + where, at},
		{"jumping nowhere", `{"id": "act", "objective": "jumpToBlock", "target": "ghostblock"}`, "Conditional Logic: Block ID ghostblock does not exist" + where, at},
		{"jumping to itself", `{"id": "act", "objective": "jumpToBlock", "target": "blockb"}`, "Conditional Logic: Cannot jump to the current block" + where, at},
		{"two jumps", `{"id": "a1", "objective": "jumpToBlock", "target": "blockc"}, {"id": "a2", "objective": "jumpToBlock", "target": "endone"}`, "Conditional Logic: Multiple jump actions are not allowed" + where, "blocks.1.logic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := logicSurvey(t, cond(el("text"), "isSubmitted", ""), tc.actions)
			expectIssue(t, Refine(s), tc.message, tc.path)
		})
	}
	for _, ok := range []string{
		calc("varnum", "add", el("rating")),
		calc("varnum", "add", el("num")),
		calc("varnum", "add", el("single")),
		calc("vartext", "concat", el("date")),
		calc("vartext", "assign", static(`"x"`)),
		`{"id": "act", "objective": "requireAnswer", "target": "later"}`,
	} {
		s := logicSurvey(t, cond(el("text"), "isSubmitted", ""), ok)
		expectNoIssues(t, Refine(s))
	}
}

func TestBlockLogicFallback(t *testing.T) {
	base := `{"blocks": [
		{"id": "blocka", "name": "A", "elements": [{"id": "q", "type": "openText", "headline": {"default": "Q"}, "required": false}]%s},
		{"id": "blockb", "name": "B", "elements": [{"id": "r", "type": "openText", "headline": {"default": "R"}, "required": false}]}],
		"endings": [{"id": "endone", "type": "endScreen", "headline": {"default": "T"}}]}`
	logic := `, "logic": [{"id": "l", "conditions": {"id": "g", "connector": "and", "conditions": []}, "actions": [{"id": "a", "objective": "jumpToBlock", "target": "endone"}]}]`
	cases := []struct{ extra, message string }{
		{`, "logicFallback": "blockb"`, "Conditional Logic: Fallback logic is defined without any logic in block 1"},
		{logic + `, "logicFallback": "blocka"`, "Conditional Logic: Fallback logic is defined with the same block in block 1"},
		{logic + `, "logicFallback": "ghost"`, "Conditional Logic: Fallback block ID ghost does not exist in block 1"},
	}
	for _, tc := range cases {
		s := load(t, strings.Replace(base, "%s", tc.extra, 1))
		expectIssue(t, Refine(s), tc.message, "blocks.0")
	}
	s := load(t, strings.Replace(base, "%s", logic+`, "logicFallback": "blockb"`, 1))
	expectNoIssues(t, Refine(s))
}
