package jsonschema

import (
	"encoding/json"
	"os"
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
	if got := len(v.Names()); got != 23 {
		t.Fatalf("compiled %d schemas %v, want the 23 the export writes", got, v.Names())
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
