package openapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type widget struct {
	ID      uuid.UUID       `json:"id"`
	Parent  *uuid.UUID      `json:"parent"`
	Payload json.RawMessage `json:"payload"`
	Tags    []string        `json:"tags,omitempty"`
}

func TestInfer(t *testing.T) {
	raw, err := json.Marshal(Infer[widget]())
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		`"id":{"type":"string","format":"uuid"}`,
		`"parent":{"type":["null","string"],"format":"uuid"}`,
		`"payload":{"description":"Any JSON value."}`,
		`"required":["id","parent","payload"]`,
		`"additionalProperties":false`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("schema %s lacks %s", got, want)
		}
	}
}

func TestNotNull(t *testing.T) {
	type list struct {
		Items []string   `json:"items"`
		Owner *uuid.UUID `json:"owner"`
	}
	s := NotNull(Infer[list](), "items", "owner")
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"items":{"type":"array","items":{"type":"string"}}`, `"owner":{"type":"string","format":"uuid"}`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("schema %s lacks %s", raw, want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("NotNull of a property that is not nullable did not panic")
		}
	}()
	NotNull(s, "items")
}

func TestDocument(t *testing.T) {
	spec := Spec{
		Schemas: map[string]*Schema{"Widget": Infer[widget]()},
		Operations: []Operation{
			{Method: http.MethodGet, Path: "/api/v3/widgets/{widgetId}", ID: "getWidget", Tag: "Widgets", Summary: "Get a widget", Parameters: []Parameter{PathID("widgetId", "")}, Responses: []Response{{Status: http.StatusOK, Description: "The widget.", Schema: Data(Ref("Widget"))}}, Problems: []int{http.StatusForbidden}},
			{Method: http.MethodPost, Path: "/api/v3/widgets", ID: "createWidget", Tag: "Widgets", Summary: "Create a widget", Body: Object("", nil), BodyOptional: true, Responses: []Response{{Status: http.StatusCreated, Description: "Created.", Schema: Data(Ref("Widget"))}}},
			{Method: http.MethodGet, Path: "/api/v3/widgets.json", ID: "doc", Tag: "Widgets", Summary: "Public", Public: true, Responses: []Response{{Status: http.StatusOK, Description: "Doc."}}},
		},
	}
	indented, err := Document("https://forma.example.com/", spec)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, indented); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	var doc struct {
		OpenAPI  string                                           `json:"openapi"`
		Servers  []map[string]string                              `json:"servers"`
		Security []map[string][]string                            `json:"security"`
		Paths    map[string]map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.1.0" || doc.Servers[0]["url"] != "https://forma.example.com" || len(doc.Security) != 2 {
		t.Fatalf("document %s", raw)
	}
	get := doc.Paths["/api/v3/widgets/{widgetId}"]["get"]
	var responses map[string]json.RawMessage
	_ = json.Unmarshal(get["responses"], &responses)
	for _, status := range []string{"200", "401", "403", "429", "default"} {
		if _, ok := responses[status]; !ok {
			t.Errorf("getWidget lacks the %s answer: %s", status, get["responses"])
		}
	}
	if string(responses["403"]) != `{"$ref":"#/components/responses/Forbidden"}` || string(responses["default"]) != `{"$ref":"#/components/responses/UnexpectedProblem"}` {
		t.Errorf("the problem answers are not the shared ones: %s", get["responses"])
	}
	if !strings.Contains(string(raw), `"Forbidden":{"content":{"application/problem+json":{"schema":{"$ref":"#/components/schemas/Problem"}}},"description":"Forbidden"}`) {
		t.Errorf("the shared Forbidden answer is not a problem: %s", raw)
	}
	post := doc.Paths["/api/v3/widgets"]["post"]
	if !strings.Contains(string(post["requestBody"]), `"required":false`) || !strings.Contains(string(post["responses"]), `"Location"`) {
		t.Errorf("createWidget %s %s", post["requestBody"], post["responses"])
	}
	public := doc.Paths["/api/v3/widgets.json"]["get"]
	if string(public["security"]) != "[]" || strings.Contains(string(public["responses"]), `"401"`) {
		t.Errorf("a public operation %s %s", public["security"], public["responses"])
	}

	if _, err := Document("", spec, spec); err == nil {
		t.Fatal("a schema defined twice was accepted")
	}
	if _, err := Document("", Spec{Operations: spec.Operations[:1]}, Spec{Operations: spec.Operations[:1]}); err == nil {
		t.Fatal("an operation described twice was accepted")
	}
}
