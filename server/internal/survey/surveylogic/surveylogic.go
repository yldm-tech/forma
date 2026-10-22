// Package surveylogic evaluates survey logic conditions against a persisted response, forma2's server engine (apps/web/lib/surveyLogic/utils.ts evaluateLogic): the engine quotas screen responses with. It reproduces the JavaScript the engine was written in, over JSON values as encoding/json decodes them, so a response is screened exactly as forma2 screened it; jsvalue.Undefined stands for an absent key.
package surveylogic

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/yldm-tech/forma/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// Survey is what the engine reads of a survey: its elements in document order and its computed fields.
type Survey struct {
	Elements []surveydoc.Element
	Computed []embeddeddata.Linked
}

// NewSurvey flattens blocks into their elements, forma2's getElementsFromBlocks, and keeps the computed fields of the survey's embedded data.
func NewSurvey(blocks []surveydoc.Block, fields []embeddeddata.Linked) Survey {
	var elements []surveydoc.Element
	for _, b := range blocks {
		elements = append(elements, b.Elements...)
	}
	return Survey{Elements: elements, Computed: embeddeddata.OfSource(fields, embeddeddata.SourceComputed)}
}

func (s Survey) element(id string) *surveydoc.Element {
	for i := range s.Elements {
		if s.Elements[i].ID == id {
			return &s.Elements[i]
		}
	}
	return nil
}

// Input is the response a condition reads: its data and variables, the language its labels are matched in, and the reserved values (embeddeddata.ServerValues) a reserved operand reads.
type Input struct {
	Data      map[string]any
	Variables map[string]any
	Language  string
	Reserved  map[string]any
}

// Evaluate is evaluateLogic: a condition group, its nested groups evaluated recursively and its single conditions by the engine, combined with "or" as any and anything else as all.
func Evaluate(s Survey, in Input, group surveydoc.Condition) bool {
	results := make([]bool, len(group.Conditions))
	for i, c := range group.Conditions {
		if c.IsGroup {
			results[i] = Evaluate(s, in, c)
		} else {
			results[i] = evaluateSingle(s, in, c)
		}
	}
	if group.Connector == "or" {
		for _, r := range results {
			if r {
				return true
			}
		}
		return false
	}
	for _, r := range results {
		if !r {
			return false
		}
	}
	return true
}

func member(m map[string]any, key string) any {
	if v, ok := m[key]; ok {
		return v
	}
	return jsvalue.Undefined
}

// operandValue is an operand's value as decoded JSON, Undefined when it has none.
func operandValue(o *surveydoc.Operand) any {
	if o == nil || len(o.Value) == 0 {
		return jsvalue.Undefined
	}
	var v any
	if json.Unmarshal(o.Value, &v) != nil {
		return jsvalue.Undefined
	}
	return v
}

// operandKey is an operand's value used as a property name, as JavaScript would use it.
func operandKey(o *surveydoc.Operand) string { return jsvalue.String(operandValue(o)) }

func localized(s surveydoc.I18nString, language string) string { return s[language] }

func isArray(v any) bool {
	_, ok := v.([]any)
	return ok
}

func isString(v any) bool {
	_, ok := v.(string)
	return ok
}

// isObject is typeof v === "object": arrays, objects and null.
func isObject(v any) bool {
	switch v.(type) {
	case nil, []any, map[string]any:
		return true
	}
	return false
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return v != jsvalue.Undefined
}

// sameValueZero is the comparison Array.prototype.includes makes.
func sameValueZero(a, b any) bool {
	if x, ok := a.(float64); ok {
		if y, ok := b.(float64); ok && math.IsNaN(x) && math.IsNaN(y) {
			return true
		}
	}
	return jsvalue.StrictEqual(a, b)
}

func includes(list any, v any) bool {
	for _, e := range list.([]any) {
		if sameValueZero(e, v) {
			return true
		}
	}
	return false
}

// objectValues is Object.values for an array or an object.
func objectValues(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, e)
		}
		return out
	}
	return nil
}

// dateTime is new Date(v).getTime(), NaN for an Invalid Date.
func dateTime(v string) float64 {
	t, ok := jsvalue.ParseDate(v)
	if !ok {
		return math.NaN()
	}
	return float64(t.UnixMilli())
}

func evaluateSingle(s Survey, in Input, c surveydoc.Condition) bool {
	if c.LeftOperand == nil {
		// forma2 read leftOperand.type off undefined and its try/catch made the condition false.
		return false
	}
	left, ok := leftValue(s, in, c.LeftOperand)
	if !ok {
		return false
	}
	right := jsvalue.Undefined
	if c.RightOperand != nil {
		right = rightValue(s, in, c.RightOperand)
	}

	var leftField, rightField *surveydoc.Element
	if c.LeftOperand.Type == "element" {
		leftField = s.element(operandKey(c.LeftOperand))
	}
	if c.RightOperand != nil && c.RightOperand.Type == "element" {
		rightField = s.element(operandKey(c.RightOperand))
	}
	leftType, rightType := "", ""
	if leftField != nil {
		leftType = leftField.Type
	}
	if rightField != nil {
		rightType = rightField.Type
	}

	if c.LeftOperand.Type == "variable" && c.RightOperand != nil && (c.RightOperand.Type == "hiddenField" || c.RightOperand.Type == "reserved") {
		if f := embeddeddata.FindComputed(s.Computed, operandKey(c.LeftOperand)); f != nil && f.DataType == embeddeddata.TypeNumber {
			right = jsvalue.Number(right)
		}
	}

	// A missing operand is not text: it matches no substring operator, and the three negations are true for it.
	substrings := !jsvalue.IsNullish(left) && !jsvalue.IsNullish(right)
	ls, rs := jsvalue.String(left), jsvalue.String(right)
	leftString, leftIsString := left.(string)
	rightString, rightIsString := right.(string)
	bothStrings := leftIsString && rightIsString

	switch c.Operator {
	case "equals":
		if c.LeftOperand.Type == "element" && leftType == surveydoc.ElementDate && bothStrings {
			return dateTime(leftString) == dateTime(rightString)
		}
		if c.RightOperand != nil && c.RightOperand.Type == "element" {
			if rightType == surveydoc.ElementMultipleChoiceMulti {
				if list, ok := right.([]any); ok && leftIsString && len(list) == 1 {
					return includes(right, left)
				}
				return false
			}
			if rightType == surveydoc.ElementDate && bothStrings {
				return dateTime(leftString) == dateTime(rightString)
			}
		}
		if list, ok := left.([]any); ok && len(list) == 1 && rightIsString && includes(left, right) {
			return true
		}
		return jsvalue.StrictEqual(left, right)
	case "doesNotEqual":
		if c.LeftOperand.Type == "element" && leftType == surveydoc.ElementPictureSelection {
			if list, ok := left.([]any); ok && len(list) > 0 && rightIsString {
				return !includes(left, right)
			}
		}
		if c.LeftOperand.Type == "element" && leftType == surveydoc.ElementDate && bothStrings {
			return dateTime(leftString) != dateTime(rightString)
		}
		if c.RightOperand != nil && c.RightOperand.Type == "element" {
			if rightType == surveydoc.ElementMultipleChoiceMulti {
				if list, ok := right.([]any); ok && leftIsString && len(list) == 1 {
					return !includes(right, left)
				}
				return false
			}
			if rightType == surveydoc.ElementDate && bothStrings {
				return dateTime(leftString) != dateTime(rightString)
			}
		}
		if list, ok := left.([]any); ok && len(list) == 1 && rightIsString {
			return !includes(left, right)
		}
		return !jsvalue.StrictEqual(left, right)
	case "contains":
		return substrings && strings.Contains(ls, rs)
	case "doesNotContain":
		return !substrings || !strings.Contains(ls, rs)
	case "startsWith":
		return substrings && strings.HasPrefix(ls, rs)
	case "doesNotStartWith":
		return !substrings || !strings.HasPrefix(ls, rs)
	case "endsWith":
		return substrings && strings.HasSuffix(ls, rs)
	case "doesNotEndWith":
		return !substrings || !strings.HasSuffix(ls, rs)
	case "isSubmitted":
		switch x := left.(type) {
		case string:
			if c.LeftOperand.Type == "element" && leftType == surveydoc.ElementFileUpload && x != "" {
				return x != "skipped"
			}
			return x != ""
		case []any:
			return len(x) > 0
		case float64:
			return true
		}
		return false
	case "isSkipped":
		if list, ok := left.([]any); ok && len(list) == 0 {
			return true
		}
		if left == "" || jsvalue.IsNullish(left) {
			return true
		}
		if m, ok := left.(map[string]any); ok {
			return len(m) == 0
		}
		return false
	case "isGreaterThan":
		return jsvalue.Number(left) > jsvalue.Number(right)
	case "isLessThan":
		return jsvalue.Number(left) < jsvalue.Number(right)
	case "isGreaterThanOrEqual":
		return jsvalue.Number(left) >= jsvalue.Number(right)
	case "isLessThanOrEqual":
		return jsvalue.Number(left) <= jsvalue.Number(right)
	case "equalsOneOf", "isAnyOf":
		return isArray(right) && leftIsString && includes(right, left)
	case "includesAllOf":
		if !isArray(left) || !isArray(right) {
			return false
		}
		for _, v := range right.([]any) {
			if !includes(left, v) {
				return false
			}
		}
		return true
	case "includesOneOf":
		if !isArray(left) || !isArray(right) {
			return false
		}
		for _, v := range right.([]any) {
			if includes(left, v) {
				return true
			}
		}
		return false
	case "doesNotIncludeAllOf":
		if !isArray(left) || !isArray(right) {
			return false
		}
		for _, v := range right.([]any) {
			if !includes(left, v) {
				return true
			}
		}
		return false
	case "doesNotIncludeOneOf":
		if !isArray(left) || !isArray(right) {
			return false
		}
		for _, v := range right.([]any) {
			if includes(left, v) {
				return false
			}
		}
		return true
	case "isAccepted":
		return left == "accepted"
	case "isClicked":
		return left == "clicked"
	case "isNotClicked":
		return left != "clicked"
	case "isAfter":
		return dateTime(ls) > dateTime(rs)
	case "isBefore":
		return dateTime(ls) < dateTime(rs)
	case "isBooked":
		return left == "booked" || truthy(left)
	case "isPartiallySubmitted":
		// Object.values(null) threw, and forma2's try/catch made the condition false.
		if !isObject(left) || left == nil {
			return false
		}
		return includes(objectValues(left), "")
	case "isCompletelySubmitted":
		if !isObject(left) || left == nil {
			return false
		}
		values := objectValues(left)
		return len(values) > 0 && !includes(values, "")
	case "isSet", "isNotEmpty":
		return !jsvalue.IsNullish(left) && left != ""
	case "isNotSet":
		return jsvalue.IsNullish(left) || left == ""
	case "isEmpty":
		return left == ""
	}
	return false
}

// leftValue is getLeftOperandValue. ok is false where forma2's engine threw, which its try/catch turned into a false condition.
func leftValue(s Survey, in Input, o *surveydoc.Operand) (value any, ok bool) {
	key := operandKey(o)
	switch o.Type {
	case "element":
		el := s.element(key)
		if el == nil {
			return jsvalue.Undefined, true
		}
		answer := member(in.Data, key)
		switch {
		case el.Type == surveydoc.ElementOpenText && el.InputType == "number":
			if answer == jsvalue.Undefined {
				return jsvalue.Undefined, true
			}
			if str, isStr := answer.(string); isStr && jsvalue.Trim(str) == "" {
				return jsvalue.Undefined, true
			}
			n := jsvalue.Number(answer)
			if math.IsNaN(n) {
				return jsvalue.Undefined, true
			}
			return n, true
		case el.Type == surveydoc.ElementMultipleChoiceSingle || el.Type == surveydoc.ElementMultipleChoiceMulti:
			if v, read := choiceIDs(el, answer, in.Language); read {
				return v, true
			}
		case el.Type == surveydoc.ElementMatrix:
			if v, read, threw := matrixColumn(el, o, answer, in.Language); threw {
				return nil, false
			} else if read {
				return v, true
			}
		}
		return answer, true
	case "variable":
		return embeddeddata.LogicVariableValue(s.Computed, key, in.Variables), true
	case "hiddenField":
		return member(in.Data, key), true
	case "reserved":
		return member(in.Reserved, key), true
	}
	return jsvalue.Undefined, true
}

// choiceIDs reads a choice answer as the ids of the choices whose label it holds in the language; a label no choice has reads as "other" when the element offers an Other box. ok is false when the answer is neither a string nor an array, which leaves it as stored.
func choiceIDs(el *surveydoc.Element, answer any, language string) (any, bool) {
	othersEnabled := false
	for _, ch := range el.Choices {
		if ch.ID == "other" {
			othersEnabled = true
		}
	}
	find := func(label any) (string, bool) {
		for _, ch := range el.Choices {
			if sameValueZero(localized(ch.Label, language), label) {
				return ch.ID, true
			}
		}
		return "", false
	}
	switch x := answer.(type) {
	case string:
		if id, ok := find(x); ok {
			return id, true
		}
		if othersEnabled {
			return "other", true
		}
		return jsvalue.Undefined, true
	case []any:
		ids := []any{}
		seen := map[string]bool{}
		add := func(id string) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		for _, v := range x {
			if id, ok := find(v); ok {
				add(id)
			} else if othersEnabled {
				add("other")
			}
		}
		return ids, true
	}
	return nil, false
}

// matrixColumn reads a matrix answer for the row an operand's meta names as the index of the chosen column, as a string. read is false when the operand names no row or the answer is not an object, which leaves the answer as stored; threw is where forma2 read a row off a null answer (typeof null is "object") and threw.
func matrixColumn(el *surveydoc.Element, o *surveydoc.Operand, answer any, language string) (value any, read, threw bool) {
	rows, isObj := answer.(map[string]any)
	if !isObj && answer != nil {
		return nil, false, false
	}
	rowMeta, named := o.Meta["row"]
	if !named {
		return nil, false, false
	}
	rowIndex := jsvalue.ParseNumber(rowMeta)
	if math.IsNaN(rowIndex) || rowIndex < 0 || rowIndex >= float64(len(el.Rows)) {
		return jsvalue.Undefined, true, false
	}
	if answer == nil {
		return nil, false, true
	}
	row := localized(el.Rows[int(rowIndex)].Label, language)
	rowValue := member(rows, row)
	if rowValue == "" {
		return "", true, false
	}
	if !truthy(rowValue) {
		return jsvalue.Undefined, true, false
	}
	for i, col := range el.Columns {
		if sameValueZero(localized(col.Label, language), rowValue) {
			return jsvalue.FormatNumber(float64(i)), true, false
		}
	}
	return jsvalue.Undefined, true, false
}

func rightValue(s Survey, in Input, o *surveydoc.Operand) any {
	switch o.Type {
	case "element", "hiddenField":
		return member(in.Data, operandKey(o))
	case "variable":
		return embeddeddata.LogicVariableValue(s.Computed, operandKey(o), in.Variables)
	case "reserved":
		return member(in.Reserved, operandKey(o))
	case "static":
		return operandValue(o)
	}
	return jsvalue.Undefined
}
