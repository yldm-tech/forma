package surveydoc

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

// operatorsByElementType is the allowlist of condition operators per element type that forma2's isInvalidOperatorsForElementType applied. An open-text element's list depends on its input type and lives in openTextOperators.
var operatorsByElementType = map[string][]string{
	ElementMultipleChoiceSingle: {"equals", "doesNotEqual", "equalsOneOf", "isSubmitted", "isSkipped"},
	ElementMultipleChoiceMulti:  {"equals", "doesNotEqual", "includesAllOf", "includesOneOf", "doesNotIncludeAllOf", "doesNotIncludeOneOf", "isSubmitted", "isSkipped"},
	ElementPictureSelection:     {"equals", "doesNotEqual", "includesAllOf", "includesOneOf", "doesNotIncludeAllOf", "doesNotIncludeOneOf", "isSubmitted", "isSkipped"},
	ElementRanking:              {"isSubmitted", "isSkipped"},
	ElementNPS:                  numericOperators,
	ElementRating:               numericOperators,
	ElementCSAT:                 numericOperators,
	ElementCES:                  numericOperators,
	ElementCTA:                  {"isClicked", "isNotClicked"},
	ElementConsent:              {"isAccepted", "isSkipped"},
	ElementCal:                  {"isBooked", "isSkipped"},
	ElementFileUpload:           {"isSubmitted", "isSkipped"},
	ElementDate:                 {"equals", "doesNotEqual", "isBefore", "isAfter", "isSubmitted", "isSkipped"},
	ElementMatrix:               {"isPartiallySubmitted", "isCompletelySubmitted", "isSkipped", "isEmpty", "isNotEmpty", "isAnyOf", "equals", "doesNotEqual"},
	ElementAddress:              {"isSubmitted", "isSkipped"},
	ElementContactInfo:          {"isSubmitted", "isSkipped"},
}

var numericOperators = []string{"equals", "doesNotEqual", "isLessThan", "isLessThanOrEqual", "isGreaterThan", "isGreaterThanOrEqual", "isSubmitted", "isSkipped"}

var textOperators = []string{"equals", "doesNotEqual", "contains", "doesNotContain", "startsWith", "doesNotStartWith", "endsWith", "doesNotEndWith", "isSubmitted", "isSkipped"}

// noRightOperandOperators are the operators whose condition is complete without a right operand, as the block logic validation lists them.
var noRightOperandOperators = []string{"isSubmitted", "isSkipped", "isClicked", "isNotClicked", "isAccepted", "isBooked", "isPartiallySubmitted", "isCompletelySubmitted", "isEmpty", "isNotEmpty"}

// isInvalidOperator is forma2's isInvalidOperatorsForElementType. A required element can never be skipped.
func isInvalidOperator(el *Element, op string) bool {
	if el.Required && op == "isSkipped" {
		return true
	}
	if el.Type == ElementOpenText {
		switch el.OpenTextInputType() {
		case "email", "phone", "text", "url":
			return !contains(textOperators, op)
		case "number":
			return !contains(numericOperators, op)
		}
		return false
	}
	allowed, ok := operatorsByElementType[el.Type]
	return ok && !contains(allowed, op)
}

// logicIssues reports the issues of one logic rule of one block, all at the same path, as forma2 did.
type logicIssues struct {
	issues
	b, l int
	at   []any
}

func (li *logicIssues) add(message string) {
	li.issues.add(li.at, message)
}

func (li *logicIssues) where() string {
	return fmt.Sprintf("in logic no: %d of block %d", li.l+1, li.b+1)
}

// blockLogicIssues is forma2's validateBlockLogic: every rule's conditions and actions, then the fallback.
func blockLogicIssues(s *Survey, b int, elements map[string]ElementRef) []Issue {
	block := &s.Blocks[b]
	var out issues
	for l := range block.Logic {
		conditions := &logicIssues{b: b, l: l, at: path("blocks", b, "logic", l, "conditions")}
		conditions.group(s, &block.Logic[l].Conditions, elements)
		out = append(out, conditions.issues...)
		out = append(out, actionIssues(s, b, l, block, elements)...)
	}
	out = append(out, fallbackIssues(s, b, block)...)
	return out
}

func (li *logicIssues) group(s *Survey, c *Condition, elements map[string]ElementRef) {
	if !c.IsGroup {
		li.single(s, c, elements)
		return
	}
	for i := range c.Conditions {
		li.group(s, &c.Conditions[i], elements)
	}
}

func (li *logicIssues) single(s *Survey, c *Condition, elements map[string]ElementRef) {
	left, right := c.LeftOperand, c.RightOperand
	if left == nil {
		return
	}
	leftValue, _ := left.String()
	switch left.Type {
	case "element":
		ref, ok := elements[leftValue]
		if !ok {
			li.add(fmt.Sprintf("Conditional Logic: Element Id %s does not exist %s", leftValue, li.where()))
			return
		}
		if li.b < ref.Block {
			li.add(fmt.Sprintf("Conditional Logic: Block %d cannot refer to an element in block %d that appears later in the survey", li.b+1, ref.Block+1))
			return
		}
		li.elementCondition(s, c, ref, elements)
	case "variable":
		if s.variable(leftValue) == nil {
			li.add(fmt.Sprintf("Conditional Logic: Variable ID %s does not exist %s", leftValue, li.where()))
			return
		}
		if right != nil && right.Type == "variable" {
			id, _ := right.String()
			if s.variable(id) == nil {
				li.add(fmt.Sprintf("Conditional Logic: Variable ID %s does not exist %s", id, li.where()))
			}
		}
	case "reserved":
		// A reserved operand names an entry of the reserved-field catalog, which grows; it is deliberately not checked, so a survey saved against a newer catalog still saves.
	case "hiddenField":
		if !s.hasHiddenField(leftValue) {
			li.add(fmt.Sprintf("Conditional Logic: Hidden field ID %s does not exist %s", leftValue, li.where()))
		}
	}
}

func (li *logicIssues) elementCondition(s *Survey, c *Condition, ref ElementRef, elements map[string]ElementRef) {
	el, op, right := ref.Data, c.Operator, c.RightOperand
	if isInvalidOperator(el, op) {
		li.add(fmt.Sprintf(`Conditional Logic: Invalid operator "%s" for element type "%s" %s`, op, el.Type, li.where()))
	}
	if el.Type == ElementCTA && !el.IsExternalButton() {
		li.add(fmt.Sprintf(`Conditional Logic: CTA element "%s" does not have an external button and cannot be used in logic conditions %s`, el.ID, li.where()))
		return
	}
	if contains(noRightOperandOperators, op) {
		if right != nil {
			li.add(fmt.Sprintf(`Conditional Logic: Right operand should not be defined for operator "%s" %s`, op, li.where()))
		}
		return
	}
	switch el.Type {
	case ElementOpenText:
		li.openTextRight(s, el, op, right, elements)
	case ElementMultipleChoiceSingle, ElementMultipleChoiceMulti, ElementPictureSelection, ElementRanking:
		li.choiceRight(el, ref, op, right)
	case ElementNPS, ElementRating:
		li.scoreRight(s, el, op, right)
	case ElementDate:
		li.dateRight(s, right, elements)
	case ElementMatrix:
		li.matrixRight(el, c.LeftOperand, op, right)
	}
}

func (li *logicIssues) rightVariable(s *Survey, right *Operand) *Variable {
	id, _ := right.String()
	v := s.variable(id)
	if v == nil {
		li.add(fmt.Sprintf("Conditional Logic: Variable ID %s does not exist %s", id, li.where()))
	}
	return v
}

func (li *logicIssues) rightHiddenField(s *Survey, right *Operand) {
	id, _ := right.String()
	if !s.hasHiddenField(id) {
		li.add(fmt.Sprintf("Conditional Logic: Hidden field ID %s does not exist %s", id, li.where()))
	}
}

func (li *logicIssues) openTextRight(s *Survey, el *Element, op string, right *Operand, elements map[string]ElementRef) {
	if right == nil {
		return
	}
	switch right.Type {
	case "element":
		id, _ := right.String()
		other, ok := elements[id]
		if !ok {
			li.add(fmt.Sprintf("Conditional Logic: Element ID %s does not exist %s", id, li.where()))
			return
		}
		valid := []string{ElementOpenText}
		if el.OpenTextInputType() == "number" {
			valid = append(valid, ElementRating, ElementNPS)
		}
		if (op == "equals" || op == "doesNotEqual") && el.OpenTextInputType() != "number" {
			valid = append(valid, ElementDate, ElementMultipleChoiceSingle, ElementMultipleChoiceMulti)
		}
		if !contains(valid, other.Data.Type) {
			li.add(fmt.Sprintf(`Conditional Logic: Invalid element type "%s" for right operand %s`, other.Data.Type, li.where()))
		}
	case "variable":
		li.rightVariable(s, right)
	case "hiddenField":
		li.rightHiddenField(s, right)
	case "static":
		if !right.Truthy() {
			li.add(fmt.Sprintf("Conditional Logic: Static value is required %s", li.where()))
		}
	}
}

func (li *logicIssues) choiceRight(el *Element, ref ElementRef, op string, right *Operand) {
	if right == nil || right.Type != "static" {
		li.add(fmt.Sprintf(`Conditional Logic: Right operand should be a static value for "%s" %s`, op, li.where()))
		return
	}
	choiceIDs := make([]string, len(el.Choices))
	for i, ch := range el.Choices {
		choiceIDs[i] = ch.ID
	}
	switch {
	case op == "equals" || op == "doesNotEqual":
		value, ok := right.String()
		if !ok {
			li.add(fmt.Sprintf(`Conditional Logic: Right operand should be a string for "%s" %s`, op, li.where()))
			return
		}
		if !contains(choiceIDs, value) {
			li.add(fmt.Sprintf(`Conditional Logic: Choice "%s" does not exist in element %d of block %d`, value, ref.Element+1, ref.Block+1))
		}
	case el.Type != ElementMultipleChoiceSingle && contains([]string{"includesAllOf", "includesOneOf", "doesNotIncludeAllOf", "doesNotIncludeOneOf"}, op):
		var values []json.RawMessage
		if len(right.Value) == 0 || right.Value[0] != '[' || json.Unmarshal(right.Value, &values) != nil {
			li.add(fmt.Sprintf(`Conditional Logic: Right operand should be an array for "%s" %s`, op, li.where()))
			return
		}
		missing := false
		for _, raw := range values {
			var s string
			if json.Unmarshal(raw, &s) != nil {
				li.add(fmt.Sprintf(`Conditional Logic: Each value in the right operand should be a string for "%s" %s`, op, li.where()))
				missing = true
				continue
			}
			if !contains(choiceIDs, s) {
				missing = true
			}
		}
		if missing {
			li.add(fmt.Sprintf("Conditional Logic: One or more choices selected in right operand do not exist in the element %s", li.where()))
		}
	}
}

func (li *logicIssues) scoreRight(s *Survey, el *Element, op string, right *Operand) {
	switch {
	case right != nil && right.Type == "variable":
		if v := li.rightVariable(s, right); v != nil && v.Type != "number" {
			li.add(fmt.Sprintf("Conditional Logic: Variable type should be number %s", li.where()))
		}
	case right != nil && right.Type == "static":
		n, ok := right.Number()
		switch {
		case !ok:
			li.add(fmt.Sprintf(`Conditional Logic: Right operand should be a number for "%s" %s`, op, li.where()))
		case el.Type == ElementNPS:
			if n < 0 || n > 10 {
				li.add(fmt.Sprintf(`Conditional Logic: NPS score should be between 0 and 10 for "%s" %s`, op, li.where()))
			}
		default:
			r := math.NaN()
			if el.Range != nil {
				r = *el.Range
			}
			if n < 1 || n > r {
				li.add(fmt.Sprintf(`Conditional Logic: Rating value should be between 1 and %s for "%s" %s`, formatNumber(r), op, li.where()))
			}
		}
	default:
		li.add(fmt.Sprintf(`Conditional Logic: Right operand should be a variable or a static value for "%s" %s`, op, li.where()))
	}
}

func (li *logicIssues) dateRight(s *Survey, right *Operand, elements map[string]ElementRef) {
	if right == nil {
		return
	}
	switch right.Type {
	case "element":
		id, _ := right.String()
		other, ok := elements[id]
		if !ok {
			li.add(fmt.Sprintf("Conditional Logic: Element ID %s does not exist %s", id, li.where()))
			return
		}
		if other.Data.Type != ElementOpenText && other.Data.Type != ElementDate {
			li.add(fmt.Sprintf(`Conditional Logic: Invalid element type "%s" for right operand %s`, other.Data.Type, li.where()))
		}
	case "variable":
		if v := li.rightVariable(s, right); v != nil && v.Type != "text" {
			li.add(fmt.Sprintf("Conditional Logic: Variable type should be text %s", li.where()))
		}
	case "hiddenField":
		li.rightHiddenField(s, right)
	case "static":
		if !right.Truthy() {
			li.add(fmt.Sprintf("Conditional Logic: Please select a date value %s", li.where()))
		} else if !isJSDate(right) {
			li.add(fmt.Sprintf("Conditional Logic: Invalid date format for right operand %s", li.where()))
		}
	}
}

// dateLayouts are the date and date-time forms a static date operand may take; the editor writes yyyy-MM-dd.
var dateLayouts = []string{"2006-01-02", time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01", "2006"}

// isJSDate is forma2's !isNaN(new Date(value).getTime()) for the date forms authoring produces: a number of milliseconds, or an ISO 8601 date or date-time.
func isJSDate(o *Operand) bool {
	if _, ok := o.Number(); ok {
		return true
	}
	s, ok := o.String()
	if !ok {
		return false
	}
	for _, layout := range dateLayouts {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

func (li *logicIssues) matrixRight(el *Element, left *Operand, op string, right *Operand) {
	row, hasRow := left.Meta["row"]
	if !hasRow {
		if right != nil && len(right.Value) > 0 {
			li.add(fmt.Sprintf("Conditional Logic: Right operand is not allowed in matrix element %s", li.where()))
		}
		if op != "isPartiallySubmitted" && op != "isCompletelySubmitted" {
			li.add(fmt.Sprintf(`Conditional Logic: Operator "%s" is not allowed in matrix element %s`, op, li.where()))
		}
		return
	}
	if right == nil {
		li.add(fmt.Sprintf("Conditional Logic: Right operand is required in matrix element %s", li.where()))
		return
	}
	if right.Type != "static" {
		li.add(fmt.Sprintf("Conditional Logic: Right operand should be a static value in matrix element %s", li.where()))
	}
	// Number(row): NaN fails neither bound, as in forma2.
	if index, err := strconv.ParseFloat(row, 64); err == nil && (index < 0 || index >= float64(len(el.Rows))) {
		li.add(fmt.Sprintf("Conditional Logic: Invalid row index in matrix element %s", li.where()))
	}
}

// actionIssues is forma2's validateBlockActions: at most one issue per action, then the multiple-jump rule.
func actionIssues(s *Survey, b, l int, block *Block, elements map[string]ElementRef) []Issue {
	var out issues
	at := path("blocks", b, "logic", l)
	where := fmt.Sprintf("in logic no: %d of block %d", l+1, b+1)
	jumps := 0
	for _, a := range block.Logic[l].Actions {
		switch a.Objective {
		case "calculate":
			if m := calculateIssue(s, a, where, elements); m != "" {
				out.add(at, m)
			}
		case "requireAnswer":
			target, ok := elements[a.Target]
			switch {
			case !ok:
				out.add(at, fmt.Sprintf("Conditional Logic: Element ID %s does not exist for requireAnswer action %s", a.Target, where))
			case target.Block == b:
				out.add(at, fmt.Sprintf("Conditional Logic: Element %s cannot be in the current block for requireAnswer action %s. RequireAnswer must target elements in other blocks.", a.Target, where))
			case target.Block < b:
				out.add(at, fmt.Sprintf("Conditional Logic: Element %s is in a previous block (block %d). RequireAnswer should target elements in future blocks after block %d.", a.Target, target.Block+1, b+1))
			case target.Data.Required:
				out.add(at, fmt.Sprintf("Conditional Logic: Element %s in block %d is already required %s", a.Target, target.Block+1, where))
			}
		case "jumpToBlock":
			jumps++
			if !isNavigationTarget(s, a.Target) {
				out.add(at, fmt.Sprintf("Conditional Logic: Block ID %s does not exist %s", a.Target, where))
			} else if a.Target == block.ID {
				out.add(at, fmt.Sprintf("Conditional Logic: Cannot jump to the current block %s", where))
			}
		}
	}
	if jumps > 1 {
		out.add(path("blocks", b, "logic"), fmt.Sprintf("Conditional Logic: Multiple jump actions are not allowed %s", where))
	}
	return out
}

func calculateIssue(s *Survey, a Action, where string, elements map[string]ElementRef) string {
	variable := s.variable(a.VariableID)
	if variable == nil {
		return fmt.Sprintf("Conditional Logic: Variable ID %s does not exist %s", a.VariableID, where)
	}
	if a.Value == nil {
		return ""
	}
	value, _ := a.Value.String()
	if a.Value.Type == "variable" {
		if selected := s.variable(value); selected == nil || selected.Type != variable.Type {
			return fmt.Sprintf("Conditional Logic: Invalid variable type for variable %s", where)
		}
	}
	if a.Value.Type != "element" {
		return ""
	}
	selected, ok := elements[value]
	if variable.Type == "text" {
		if !ok || !contains([]string{ElementOpenText, ElementMultipleChoiceSingle, ElementRating, ElementNPS, ElementDate}, selected.Data.Type) {
			return fmt.Sprintf("Conditional Logic: Invalid element type for text variable %s", where)
		}
		return ""
	}
	// forma2's condition rejects only a missing element or a non-number open-text element; every other type passes.
	if !ok || (selected.Data.Type == ElementOpenText && selected.Data.OpenTextInputType() != "number") {
		return fmt.Sprintf("Conditional Logic: Invalid element type for number variable %s", where)
	}
	return ""
}

func isNavigationTarget(s *Survey, id string) bool {
	for _, b := range s.Blocks {
		if b.ID == id {
			return true
		}
	}
	for _, e := range s.Endings {
		if e.ID == id {
			return true
		}
	}
	return false
}

// fallbackIssues is forma2's validateBlockLogicFallback.
func fallbackIssues(s *Survey, b int, block *Block) []Issue {
	if block.LogicFallback == "" {
		return nil
	}
	at := path("blocks", b)
	switch {
	case len(block.Logic) == 0:
		return []Issue{{Path: at, Message: fmt.Sprintf("Conditional Logic: Fallback logic is defined without any logic in block %d", b+1)}}
	case block.ID == block.LogicFallback:
		return []Issue{{Path: at, Message: fmt.Sprintf("Conditional Logic: Fallback logic is defined with the same block in block %d", b+1)}}
	}
	for i, other := range s.Blocks {
		if i != b && other.ID == block.LogicFallback {
			return nil
		}
	}
	for _, e := range s.Endings {
		if e.ID == block.LogicFallback {
			return nil
		}
	}
	return []Issue{{Path: at, Message: fmt.Sprintf("Conditional Logic: Fallback block ID %s does not exist in block %d", block.LogicFallback, b+1)}}
}

// formatNumber prints a number as JavaScript's String(n) does for the values these messages carry.
func formatNumber(n float64) string {
	if math.IsNaN(n) {
		return "undefined"
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}
