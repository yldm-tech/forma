package ai

import (
	"encoding/json"
	"strings"
)

// parsePartialJSON is the AI SDK's parsePartialJson: the text as JSON if it already is, else the text completed by fixJSON, else nothing. It is how a structured output streamed as text becomes a readable snapshot before the model has finished writing it.
func parsePartialJSON(text string) (any, bool) {
	var v any
	if err := json.Unmarshal([]byte(text), &v); err == nil {
		return v, true
	}
	if err := json.Unmarshal([]byte(fixJSON(text)), &v); err == nil {
		return v, true
	}
	return nil, false
}

// jsonLiterals are the literals a truncated text may end inside, in the order fixJSON completes them.
var jsonLiterals = []string{"true", "false", "null"}

type jsonState int

const (
	stateRoot jsonState = iota
	stateFinish
	stateString
	stateStringEscape
	stateLiteral
	stateNumber
	stateObjectStart
	stateObjectKey
	stateObjectAfterKey
	stateObjectBeforeValue
	stateObjectAfterValue
	stateObjectAfterComma
	stateArrayStart
	stateArrayAfterValue
	stateArrayAfterComma
)

// fixJSON is the AI SDK's fixJson, a single linear scan that cuts a truncated JSON text back to its last complete token and closes every string, object, array and literal still open. Invalid JSON is not repaired: whatever comes out still goes through a real JSON parser.
func fixJSON(input string) string {
	stack := []jsonState{stateRoot}
	lastValid := -1
	literalStart := -1
	top := func() jsonState { return stack[len(stack)-1] }
	pop := func() { stack = stack[:len(stack)-1] }
	push := func(s jsonState) { stack = append(stack, s) }

	valueStart := func(c byte, i int, swap jsonState) {
		switch {
		case c == '"':
			lastValid = i
			pop()
			push(swap)
			push(stateString)
		case c == 'f' || c == 't' || c == 'n':
			lastValid = i
			literalStart = i
			pop()
			push(swap)
			push(stateLiteral)
		case c == '-':
			pop()
			push(swap)
			push(stateNumber)
		case c >= '0' && c <= '9':
			lastValid = i
			pop()
			push(swap)
			push(stateNumber)
		case c == '{':
			lastValid = i
			pop()
			push(swap)
			push(stateObjectStart)
		case c == '[':
			lastValid = i
			pop()
			push(swap)
			push(stateArrayStart)
		}
	}
	afterObjectValue := func(c byte, i int) {
		switch c {
		case ',':
			pop()
			push(stateObjectAfterComma)
		case '}':
			lastValid = i
			pop()
		}
	}
	afterArrayValue := func(c byte, i int) {
		switch c {
		case ',':
			pop()
			push(stateArrayAfterComma)
		case ']':
			lastValid = i
			pop()
		}
	}
	isLiteralPrefix := func(s string) bool {
		for _, literal := range jsonLiterals {
			if strings.HasPrefix(literal, s) {
				return true
			}
		}
		return false
	}

	for i := 0; i < len(input); i++ {
		c := input[i]
		switch top() {
		case stateRoot:
			valueStart(c, i, stateFinish)
		case stateObjectStart:
			switch c {
			case '"':
				pop()
				push(stateObjectKey)
			case '}':
				lastValid = i
				pop()
			}
		case stateObjectAfterComma:
			if c == '"' {
				pop()
				push(stateObjectKey)
			}
		case stateObjectKey:
			if c == '"' {
				pop()
				push(stateObjectAfterKey)
			}
		case stateObjectAfterKey:
			if c == ':' {
				pop()
				push(stateObjectBeforeValue)
			}
		case stateObjectBeforeValue:
			valueStart(c, i, stateObjectAfterValue)
		case stateObjectAfterValue:
			afterObjectValue(c, i)
		case stateString:
			switch c {
			case '"':
				pop()
				lastValid = i
			case '\\':
				push(stateStringEscape)
			default:
				lastValid = i
			}
		case stateArrayStart:
			if c == ']' {
				lastValid = i
				pop()
			} else {
				lastValid = i
				valueStart(c, i, stateArrayAfterValue)
			}
		case stateArrayAfterValue:
			switch c {
			case ',':
				pop()
				push(stateArrayAfterComma)
			case ']':
				lastValid = i
				pop()
			default:
				lastValid = i
			}
		case stateArrayAfterComma:
			valueStart(c, i, stateArrayAfterValue)
		case stateStringEscape:
			pop()
			lastValid = i
		case stateNumber:
			switch {
			case c >= '0' && c <= '9':
				lastValid = i
			case c == 'e' || c == 'E' || c == '-' || c == '.':
			case c == ',':
				pop()
				if top() == stateArrayAfterValue {
					afterArrayValue(c, i)
				}
				if top() == stateObjectAfterValue {
					afterObjectValue(c, i)
				}
			case c == '}':
				pop()
				if top() == stateObjectAfterValue {
					afterObjectValue(c, i)
				}
			case c == ']':
				pop()
				if top() == stateArrayAfterValue {
					afterArrayValue(c, i)
				}
			default:
				pop()
			}
		case stateLiteral:
			if !isLiteralPrefix(input[literalStart : i+1]) {
				pop()
				if top() == stateObjectAfterValue {
					afterObjectValue(c, i)
				} else if top() == stateArrayAfterValue {
					afterArrayValue(c, i)
				}
			} else {
				lastValid = i
			}
		}
	}

	result := []byte(input[:lastValid+1])
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i] {
		case stateString:
			result = append(result, '"')
		case stateObjectKey, stateObjectAfterKey, stateObjectAfterComma, stateObjectStart, stateObjectBeforeValue, stateObjectAfterValue:
			result = append(result, '}')
		case stateArrayStart, stateArrayAfterComma, stateArrayAfterValue:
			result = append(result, ']')
		case stateLiteral:
			partial := input[literalStart:]
			for _, lit := range jsonLiterals {
				if strings.HasPrefix(lit, partial) {
					result = append(result, lit[len(partial):]...)
					break
				}
			}
		}
	}
	return string(result)
}
