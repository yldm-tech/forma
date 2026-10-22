package clientapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// maxBodyBytes is forma2's DEFAULT_REQUEST_BODY_LIMIT_BYTES.
const maxBodyBytes = 2 * 1024 * 1024

// errBodyTooLarge is forma2's RequestBodyTooLargeError.
var errBodyTooLarge = fmt.Errorf("Request body must not exceed %d bytes", maxBodyBytes) //nolint:staticcheck // forma2's message, which clients see

// readBody is forma2's readRequestBodyWithLimit: a declared Content-Length over the limit is refused before reading, and so is a body that turns out longer. The bytes are decoded as UTF-8 the way TextDecoder does, invalid sequences becoming U+FFFD.
func readBody(r *http.Request) (string, error) {
	if r.ContentLength > maxBodyBytes {
		return "", errBodyTooLarge
	}
	if r.Body == nil {
		return "", nil
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("read request body: %w", err)
	}
	if len(b) > maxBodyBytes {
		return "", errBodyTooLarge
	}
	return strings.ToValidUTF8(string(b), "�"), nil
}

// syntaxError is JSON.parse's SyntaxError, with V8's message.
type syntaxError struct{ message string }

func (e *syntaxError) Error() string { return e.message }

// parseBody is forma2's parseJsonBodyWithLimit: the body read with its limit and parsed by JSON.parse, returned with its text. Its errors are errBodyTooLarge, a *syntaxError carrying V8's message, or a read failure.
func parseBody(r *http.Request) (any, string, error) {
	s, err := readBody(r)
	if err != nil {
		return nil, "", err
	}
	v, err := parseJSON(s)
	return v, s, err
}

// parseJSON is JSON.parse: objects become map[string]any (a repeated key keeps its last value), numbers float64 (out of range as ±Infinity), and a syntax error is reported as V8 reports it.
func parseJSON(s string) (any, error) {
	if err := checkJSONSyntax(s); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, &syntaxError{message: err.Error()}
	}
	return jsNumbers(v), nil
}

func jsNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return math.NaN()
		}
		return f
	case map[string]any:
		for k, e := range x {
			x[k] = jsNumbers(e)
		}
	case []any:
		for i, e := range x {
			x[i] = jsNumbers(e)
		}
	}
	return v
}

// rawMember is one top-level member of a JSON object body as sent, for the decoders that need the order its keys were sent in (the ingest contract); nil when the body is not an object or lacks it. A repeated key yields its last value, as JSON.parse does.
func rawMember(body string, key string) json.RawMessage {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &obj) != nil {
		return nil
	}
	return obj[key]
}

// jsonScanner finds the first syntax error of a JSON text the way V8's JsonParser reports it. Positions are UTF-16 code units, as JavaScript counts them.
type jsonScanner struct {
	src   []uint16
	text  string
	pos   int
	depth int
}

func checkJSONSyntax(s string) error {
	sc := &jsonScanner{src: utf16.Encode([]rune(s)), text: s}
	sc.skipSpace()
	if err := sc.value(); err != nil {
		return err
	}
	sc.skipSpace()
	if sc.pos < len(sc.src) {
		return sc.at("Unexpected non-whitespace character after JSON")
	}
	return nil
}

func (sc *jsonScanner) eof() bool { return sc.pos >= len(sc.src) }

func (sc *jsonScanner) peek() uint16 { return sc.src[sc.pos] }

func (sc *jsonScanner) skipSpace() {
	for !sc.eof() {
		switch sc.peek() {
		case ' ', '\t', '\n', '\r':
			sc.pos++
		default:
			return
		}
	}
}

// at is a message with V8's "in JSON at position N (line L column C)" suffix for the current position.
func (sc *jsonScanner) at(message string) error {
	line, lineStart := 1, 0
	for i := 0; i < sc.pos && i < len(sc.src); i++ {
		switch sc.src[i] {
		case '\n':
			line++
			lineStart = i + 1
		case '\r':
			if i+1 < len(sc.src) && sc.src[i+1] == '\n' {
				continue
			}
			line++
			lineStart = i + 1
		}
	}
	// V8 does not repeat "JSON" after a message that already ends with it.
	where := " in JSON"
	if strings.HasSuffix(message, "after JSON") {
		where = ""
	}
	return &syntaxError{message: fmt.Sprintf("%s%s at position %d (line %d column %d)", message, where, sc.pos, line, sc.pos-lineStart+1)}
}

func (sc *jsonScanner) unexpectedEnd() error {
	return &syntaxError{message: "Unexpected end of JSON input"}
}

// unexpectedToken is V8's message for a character no JSON value can start with, quoting the source around it: all of it when it is shorter than 21 characters, otherwise ten characters either side.
func (sc *jsonScanner) unexpectedToken() error {
	if sc.eof() {
		return sc.unexpectedEnd()
	}
	token := string(utf16.Decode(sc.src[sc.pos : sc.pos+1]))
	if r := sc.src[sc.pos]; utf16.IsSurrogate(rune(r)) {
		token = string(utf8.RuneError)
	}
	const context = 10
	n := len(sc.src)
	if n < 2*context+1 {
		return &syntaxError{message: fmt.Sprintf("Unexpected token '%s', \"%s\" is not valid JSON", token, sc.text)}
	}
	start, end := max(0, sc.pos-context), min(n, sc.pos+context)
	snippet := string(utf16.Decode(sc.src[start:end]))
	prefix, suffix := "", ""
	if start > 0 {
		prefix = "..."
	}
	if end < n {
		suffix = "..."
	}
	return &syntaxError{message: fmt.Sprintf("Unexpected token '%s', %s\"%s\"%s is not valid JSON", token, prefix, snippet, suffix)}
}

// maxDepth bounds nesting at encoding/json's own limit, so a body the decoder would refuse is refused here first.
const maxDepth = 10000

func (sc *jsonScanner) value() error {
	if sc.eof() {
		return sc.unexpectedEnd()
	}
	switch c := sc.peek(); {
	case c == '"':
		return sc.string()
	case c == '{':
		return sc.object()
	case c == '[':
		return sc.array()
	case c == '-' || (c >= '0' && c <= '9'):
		return sc.number()
	case c == 't':
		return sc.literal("true")
	case c == 'f':
		return sc.literal("false")
	case c == 'n':
		return sc.literal("null")
	}
	return sc.unexpectedToken()
}

func (sc *jsonScanner) literal(word string) error {
	for i := 0; i < len(word); i++ {
		if sc.eof() {
			return sc.unexpectedEnd()
		}
		if sc.peek() != uint16(word[i]) {
			return sc.unexpectedToken()
		}
		sc.pos++
	}
	return nil
}

func isDigit(c uint16) bool { return c >= '0' && c <= '9' }

func (sc *jsonScanner) digits() {
	for !sc.eof() && isDigit(sc.peek()) {
		sc.pos++
	}
}

func (sc *jsonScanner) number() error {
	if sc.peek() == '-' {
		sc.pos++
		if sc.eof() || !isDigit(sc.peek()) {
			return sc.at("No number after minus sign")
		}
	}
	if sc.peek() == '0' {
		sc.pos++
		if !sc.eof() && isDigit(sc.peek()) {
			return sc.at("Unexpected number")
		}
	} else {
		sc.digits()
	}
	if !sc.eof() && sc.peek() == '.' {
		sc.pos++
		if sc.eof() || !isDigit(sc.peek()) {
			return sc.at("Unterminated fractional number")
		}
		sc.digits()
	}
	if !sc.eof() && (sc.peek() == 'e' || sc.peek() == 'E') {
		sc.pos++
		if !sc.eof() && (sc.peek() == '+' || sc.peek() == '-') {
			sc.pos++
		}
		if sc.eof() || !isDigit(sc.peek()) {
			return sc.at("Exponent part is missing a number")
		}
		sc.digits()
	}
	return nil
}

func isHex(c uint16) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func (sc *jsonScanner) string() error {
	sc.pos++
	for {
		if sc.eof() {
			return sc.at("Unterminated string")
		}
		c := sc.peek()
		switch {
		case c == '"':
			sc.pos++
			return nil
		case c < 0x20:
			return sc.at("Bad control character in string literal")
		case c == '\\':
			sc.pos++
			if sc.eof() {
				return sc.unexpectedEnd()
			}
			switch sc.peek() {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				sc.pos++
			case 'u':
				sc.pos++
				for range 4 {
					if sc.eof() || !isHex(sc.peek()) {
						return sc.at("Bad Unicode escape")
					}
					sc.pos++
				}
			default:
				return sc.at("Bad escaped character")
			}
		default:
			sc.pos++
		}
	}
}

func (sc *jsonScanner) nest() error {
	sc.depth++
	if sc.depth > maxDepth {
		return sc.at("Maximum nesting depth exceeded")
	}
	return nil
}

func (sc *jsonScanner) object() error {
	if err := sc.nest(); err != nil {
		return err
	}
	defer func() { sc.depth-- }()
	sc.pos++
	sc.skipSpace()
	if !sc.eof() && sc.peek() == '}' {
		sc.pos++
		return nil
	}
	first := true
	for {
		if sc.eof() || sc.peek() != '"' {
			if first {
				return sc.at("Expected property name or '}'")
			}
			return sc.at("Expected double-quoted property name")
		}
		first = false
		if err := sc.string(); err != nil {
			return err
		}
		sc.skipSpace()
		if sc.eof() || sc.peek() != ':' {
			return sc.at("Expected ':' after property name")
		}
		sc.pos++
		sc.skipSpace()
		if err := sc.value(); err != nil {
			return err
		}
		sc.skipSpace()
		if sc.eof() {
			return sc.at("Expected ',' or '}' after property value")
		}
		switch sc.peek() {
		case ',':
			sc.pos++
			sc.skipSpace()
		case '}':
			sc.pos++
			return nil
		default:
			return sc.at("Expected ',' or '}' after property value")
		}
	}
}

func (sc *jsonScanner) array() error {
	if err := sc.nest(); err != nil {
		return err
	}
	defer func() { sc.depth-- }()
	sc.pos++
	sc.skipSpace()
	if !sc.eof() && sc.peek() == ']' {
		sc.pos++
		return nil
	}
	for {
		if err := sc.value(); err != nil {
			return err
		}
		sc.skipSpace()
		if sc.eof() {
			return sc.at("Expected ',' or ']' after array element")
		}
		switch sc.peek() {
		case ',':
			sc.pos++
			sc.skipSpace()
		case ']':
			sc.pos++
			return nil
		default:
			return sc.at("Expected ',' or ']' after array element")
		}
	}
}
