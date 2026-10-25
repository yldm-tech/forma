// Package openapitest checks real answers of the v3 API against the OpenAPI document that describes them, so a module's tests fail when what a handler answers and what the document publishes part ways.
package openapitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/yldm-tech/forma/apps/server/internal/v3api/openapi"
)

// Document is a parsed OpenAPI document.
type Document struct {
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas   map[string]json.RawMessage `json:"schemas"`
		Responses map[string]response        `json:"responses"`
	} `json:"components"`
}

type operation struct {
	Responses map[string]response `json:"responses"`
}

// response is an answer, or a reference to one of the shared answers.
type response struct {
	Ref     string `json:"$ref"`
	Content map[string]struct {
		Schema json.RawMessage `json:"schema"`
	} `json:"content"`
}

// New builds the document of specs.
func New(t testing.TB, specs ...openapi.Spec) *Document {
	t.Helper()
	raw, err := openapi.Document("", specs...)
	if err != nil {
		t.Fatal(err)
	}
	return Parse(t, raw)
}

// Parse reads a document.
func Parse(t testing.TB, raw []byte) *Document {
	t.Helper()
	var d Document
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("openapitest: %v", err)
	}
	return &d
}

// Check fails the test unless rec is an answer the document describes for a request of method to target (a path, with or without its query): a documented status, and a body its schema accepts.
func (d *Document) Check(t testing.TB, method, target string, rec *httptest.ResponseRecorder) {
	t.Helper()
	op, ok := d.operation(method, target)
	if !ok {
		t.Fatalf("the document does not describe %s %s", method, target)
	}
	if err := d.validate(op, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Bytes()); err != nil {
		t.Fatalf("%s %s answered %d %s: %v", method, target, rec.Code, rec.Body.String(), err)
	}
}

// Describes reports whether the document has an operation for a request of method to target.
func (d *Document) Describes(method, target string) bool {
	_, ok := d.operation(method, target)
	return ok
}

// operation finds the operation a request is routed to: the path template whose segments match, a literal segment winning over a parameter, as the router decides.
func (d *Document) operation(method, target string) (operation, bool) {
	path, _, _ := strings.Cut(target, "?")
	segments := strings.Split(path, "/")
	best, bestScore := "", -1
	for template, item := range d.Paths {
		if _, ok := item[strings.ToLower(method)]; !ok {
			continue
		}
		parts := strings.Split(template, "/")
		if len(parts) != len(segments) {
			continue
		}
		score := 0
		for i, part := range parts {
			switch {
			case part == segments[i]:
				score++
			case strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") && segments[i] != "":
			default:
				score = -1
			}
			if score < 0 {
				break
			}
		}
		if score > bestScore {
			best, bestScore = template, score
		}
	}
	if bestScore < 0 {
		return operation{}, false
	}
	return d.Paths[best][strings.ToLower(method)], true
}

func (d *Document) validate(op operation, status int, contentType string, body []byte) error {
	response, ok := op.Responses[strconv.Itoa(status)]
	if !ok {
		response, ok = op.Responses["default"]
		if !ok || status < 500 {
			return fmt.Errorf("status %d is not documented", status)
		}
	}
	if name, shared := strings.CutPrefix(response.Ref, "#/components/responses/"); shared {
		if response, ok = d.Components.Responses[name]; !ok {
			return fmt.Errorf("the answer refers to the undefined shared answer %s", name)
		}
	}
	if len(response.Content) == 0 {
		if len(bytes.TrimSpace(body)) > 0 {
			return fmt.Errorf("the documented answer has no body")
		}
		return nil
	}
	mediaType, _, _ := strings.Cut(contentType, ";")
	content, ok := response.Content[strings.TrimSpace(mediaType)]
	if !ok {
		return fmt.Errorf("content type %q is not documented", contentType)
	}
	schema, err := d.compile(content.Schema)
	if err != nil {
		return err
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("body is not JSON: %w", err)
	}
	return schema.Validate(value)
}

// compile makes schema a document of its own: the components become its $defs, and the references to them follow.
func (d *Document) compile(schema json.RawMessage) (*jsonschema.Schema, error) {
	var root map[string]any
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil, err
	}
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$defs"] = d.Components.Schemas
	raw, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	raw = bytes.ReplaceAll(raw, []byte(`"#/components/schemas/`), []byte(`"#/$defs/`))
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	const url = "https://forma.test/schema.json"
	if err := c.AddResource(url, doc); err != nil {
		return nil, err
	}
	return c.Compile(url)
}
