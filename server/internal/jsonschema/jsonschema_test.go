package jsonschema

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type parityCase struct {
	Name       string          `json:"name"`
	Schema     string          `json:"schema"`
	Zod        bool            `json:"zod"`
	JSONSchema bool            `json:"jsonSchema"`
	Document   json.RawMessage `json:"document"`
}

func TestEverySchemaCompiles(t *testing.T) {
	v, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := len(v.Names()); got != 24 {
		t.Fatalf("compiled %d schemas %v, want the 24 the export writes", got, v.Names())
	}
}

// The fixtures are shared with packages/types/json-schema-registry.test.ts, which asserts the `zod` column. A case where the two columns differ is a zod refinement that JSON Schema cannot express and the service layer must enforce in Go.
func TestParityWithZod(t *testing.T) {
	v, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw, err := os.ReadFile("testdata/parity.json")
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var cases []parityCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.Schema+"/"+tc.Name, func(t *testing.T) {
			err := v.Validate(tc.Schema, tc.Document)
			if got := err == nil; got != tc.JSONSchema {
				t.Fatalf("valid = %v (err: %v), want %v", got, err, tc.JSONSchema)
			}
		})
	}
}

func TestValidateRejectsUnknownSchemasAndBrokenJSON(t *testing.T) {
	v, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Validate("survey.nope", []byte(`{}`)); err == nil {
		t.Error("an unknown schema name was accepted")
	}
	if err := v.Validate("survey.blocks", []byte(`[{`)); err == nil {
		t.Error("malformed JSON was accepted")
	}
}

func TestViolationsLocateTheFailureInsideTheSelectedAlternative(t *testing.T) {
	v, err := New()
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Violations("survey.blocks", []byte(`[{"id": "abc", "name": "B", "elements": [{"id": "q1", "type": "rating", "headline": {"default": "?"}, "required": true, "scale": "number", "range": 8}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || strings.Join(got[0].Location, ".") != "0.elements.0.range" {
		t.Fatalf("violations %+v, want one at the rating's range", got)
	}
	got, _ = v.Violations("survey.blocks", []byte(`[{"id": "abc", "name": "B", "elements": [{"id": "q1", "type": "telepathy", "headline": {"default": "?"}, "required": true}]}]`))
	if len(got) != 1 || got[0].Message != "Invalid input" || strings.Join(got[0].Location, ".") != "0.elements.0" {
		t.Fatalf("violations %+v, want one Invalid input at the element", got)
	}
	got, _ = v.Violations("survey.recaptcha", []byte(`{"enabled": true, "threshold": 2}`))
	if len(got) != 1 || strings.Join(got[0].Location, ".") != "threshold" {
		t.Fatalf("violations %+v, want one at the threshold of the nullable object", got)
	}
	if got, err := v.Violations("survey.recaptcha", []byte(`null`)); err != nil || got != nil {
		t.Fatalf("a conforming document: %v, %v", got, err)
	}
}
