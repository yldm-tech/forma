package clientapi

import (
	"errors"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
)

// The messages V8's JSON.parse (Node.js 24) gave for each input, which forma2 returned as details.error.
func TestJSONSyntaxErrorsAreV8s(t *testing.T) {
	cases := map[string]string{
		``:                              "Unexpected end of JSON input",
		` `:                             "Unexpected end of JSON input",
		`{`:                             "Expected property name or '}' in JSON at position 1 (line 1 column 2)",
		`{not json`:                     "Expected property name or '}' in JSON at position 1 (line 1 column 2)",
		`{"a":1,}`:                      "Expected double-quoted property name in JSON at position 7 (line 1 column 8)",
		`abc`:                           `Unexpected token 'a', "abc" is not valid JSON`,
		`{"a" 1}`:                       "Expected ':' after property name in JSON at position 5 (line 1 column 6)",
		`[1,2`:                          "Expected ',' or ']' after array element in JSON at position 4 (line 1 column 5)",
		`{"a":1`:                        "Expected ',' or '}' after property value in JSON at position 6 (line 1 column 7)",
		`{"a":}`:                        `Unexpected token '}', "{"a":}" is not valid JSON`,
		`1 2`:                           "Unexpected non-whitespace character after JSON at position 2 (line 1 column 3)",
		`"abc`:                          "Unterminated string in JSON at position 4 (line 1 column 5)",
		"\"a\nb\"":                      "Bad control character in string literal in JSON at position 2 (line 1 column 3)",
		`"\q"`:                          "Bad escaped character in JSON at position 2 (line 1 column 3)",
		`[`:                             "Unexpected end of JSON input",
		`tru`:                           "Unexpected end of JSON input",
		`-`:                             "No number after minus sign in JSON at position 1 (line 1 column 2)",
		`01`:                            "Unexpected number in JSON at position 1 (line 1 column 2)",
		`1.`:                            "Unterminated fractional number in JSON at position 2 (line 1 column 3)",
		`1e+`:                           "Exponent part is missing a number in JSON at position 3 (line 1 column 4)",
		`"\u12"`:                        "Bad Unicode escape in JSON at position 5 (line 1 column 6)",
		`"\`:                            "Unexpected end of JSON input",
		strings.Repeat("a", 100):        `Unexpected token 'a', "aaaaaaaaaa"... is not valid JSON`,
		strings.Repeat("x", 20):         `Unexpected token 'x', "xxxxxxxxxxxxxxxxxxxx" is not valid JSON`,
		`{"a":1,"b":x` + "111111111111": `Unexpected token 'x', ...""a":1,"b":x111111111"... is not valid JSON`,
		"[1,1,1,1,1,1,x111111111":       `Unexpected token 'x', ..."1,1,1,1,1,x111111111" is not valid JSON`,
		"\n\n  {":                       "Expected property name or '}' in JSON at position 5 (line 3 column 4)",
		"\r\n{":                         "Expected property name or '}' in JSON at position 3 (line 2 column 2)",
		`[1,]`:                          `Unexpected token ']', "[1,]" is not valid JSON`,
		`{"é":x}`:                       `Unexpected token 'x', "{"é":x}" is not valid JSON`,
		`{"a":falsy}`:                   `Unexpected token 'y', "{"a":falsy}" is not valid JSON`,
		`{  "a"  :  1  ,  }`:            "Expected double-quoted property name in JSON at position 17 (line 1 column 18)",
		" {":                            "Unexpected token ' ', \" {\" is not valid JSON",
		`{"a":"`:                        "Unterminated string in JSON at position 6 (line 1 column 7)",
		`123abc`:                        "Unexpected non-whitespace character after JSON at position 3 (line 1 column 4)",
	}
	for input, want := range cases {
		_, err := parseJSON(input)
		var se *syntaxError
		if !errors.As(err, &se) || se.message != want {
			t.Errorf("JSON.parse(%q): %v, want %q", input, err, want)
		}
	}
}

func TestParseJSONIsJSONParse(t *testing.T) {
	v, err := parseJSON(`{"a":1,"a":2,"big":1e400,"list":[true,null,"x"]}`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["a"] != 2.0 {
		t.Fatalf("a repeated key keeps its last value: %v", m["a"])
	}
	if f, _ := m["big"].(float64); !math.IsInf(f, 1) {
		t.Fatalf("an out of range number is Infinity: %v", m["big"])
	}
	if l := m["list"].([]any); l[0] != true || l[1] != nil || l[2] != "x" {
		t.Fatalf("list %v", l)
	}
}

// forma2's parseJsonBodyWithLimit: a declared or actual body over 2 MiB is refused.
func TestBodyLimit(t *testing.T) {
	big := `{"a":"` + strings.Repeat("x", maxBodyBytes) + `"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(big))
	if _, _, err := parseBody(req); !errors.Is(err, errBodyTooLarge) {
		t.Fatalf("an oversized body: %v", err)
	}
	req = httptest.NewRequest("POST", "/", strings.NewReader(big))
	req.ContentLength = -1
	if _, _, err := parseBody(req); !errors.Is(err, errBodyTooLarge) {
		t.Fatalf("an oversized body without a length: %v", err)
	}
	req = httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
	req.ContentLength = maxBodyBytes + 1
	if _, _, err := parseBody(req); !errors.Is(err, errBodyTooLarge) {
		t.Fatalf("an oversized declared length: %v", err)
	}
	if errBodyTooLarge.Error() != "Request body must not exceed 2097152 bytes" {
		t.Fatalf("message %q", errBodyTooLarge)
	}
	exact := `"` + strings.Repeat("x", maxBodyBytes-2) + `"`
	if _, _, err := parseBody(httptest.NewRequest("POST", "/", strings.NewReader(exact))); err != nil {
		t.Fatalf("a body of exactly the limit: %v", err)
	}
}

func TestRequestBodyErrors(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	path := "/api/v2/client/" + ws.String() + "/responses"
	details := e.do("POST", path, `{"surveyId":"`+strings.Repeat("x", maxBodyBytes)+`"}`).expectError(t, 413, "payload_too_large", "Payload Too Large")
	if details["error"] != "Request body must not exceed 2097152 bytes" {
		t.Fatalf("details %v", details)
	}
	e.do("POST", path, "").expectError(t, 400, "bad_request", "Invalid JSON in request body")
	// A body that is not an object reads as an empty one, as forma2 spread it.
	got := e.do("POST", path, `"just a string"`).expectError(t, 400, "bad_request", msgInvalidFields)
	if got["surveyId"] != "Invalid input: expected string, received undefined" || got["finished"] != "Invalid input: expected boolean, received undefined" || got["data"] != "Invalid input: expected record, received undefined" {
		t.Fatalf("details %v", got)
	}
}
