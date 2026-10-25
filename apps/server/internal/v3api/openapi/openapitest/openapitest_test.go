package openapitest

import (
	"net/http"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/v3api/openapi"
)

type item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestValidate(t *testing.T) {
	ok := func(desc string) []openapi.Response {
		return []openapi.Response{{Status: http.StatusOK, Description: desc, Schema: openapi.Data(openapi.Ref("Item"))}}
	}
	d := New(t, openapi.Spec{
		Schemas: map[string]*openapi.Schema{"Item": openapi.Infer[item]()},
		Operations: []openapi.Operation{
			{Method: http.MethodGet, Path: "/api/v3/items/{itemId}", ID: "getItem", Tag: "Items", Summary: "Get", Parameters: []openapi.Parameter{openapi.PathID("itemId", "")}, Responses: ok("One."), Problems: []int{http.StatusForbidden}},
			{Method: http.MethodGet, Path: "/api/v3/items/runs", ID: "runs", Tag: "Items", Summary: "Runs", Responses: []openapi.Response{{Status: http.StatusNoContent, Description: "None."}}},
		},
	})

	// A literal segment wins over a parameter, as in the router.
	runs, found := d.operation(http.MethodGet, "/api/v3/items/runs?limit=1")
	if !found || runs.Responses["204"].Content != nil {
		t.Fatalf("runs %+v %v", runs, found)
	}
	if d.Describes(http.MethodPost, "/api/v3/items/x") || d.Describes(http.MethodGet, "/api/v3/items") || !d.Describes(http.MethodGet, "/api/v3/items/x") {
		t.Fatal("Describes matched the wrong operations")
	}

	get, _ := d.operation(http.MethodGet, "/api/v3/items/x")
	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
		valid       bool
	}{
		{"documented answer", 200, "application/json; charset=utf-8", `{"data":{"id":"a","name":"b"}}`, true},
		{"missing property", 200, "application/json", `{"data":{"id":"a"}}`, false},
		{"undeclared property", 200, "application/json", `{"data":{"id":"a","name":"b","extra":1}}`, false},
		{"documented problem", 403, "application/problem+json", `{"title":"Forbidden","status":403,"detail":"No","requestId":"r","code":"forbidden"}`, true},
		{"401 every authenticated operation can give", 401, "application/problem+json", `{"title":"Unauthorized","status":401,"detail":"No","requestId":"r"}`, true},
		{"undocumented status", 409, "application/problem+json", `{"title":"Conflict","status":409,"detail":"No","requestId":"r"}`, false},
		{"unexpected failure", 500, "application/problem+json", `{"title":"Internal Server Error","status":500,"detail":"No","requestId":"r"}`, true},
		{"wrong content type", 200, "text/plain", `{"data":{"id":"a","name":"b"}}`, false},
	}
	for _, tc := range cases {
		err := d.validate(get, tc.status, tc.contentType, []byte(tc.body))
		if (err == nil) != tc.valid {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
