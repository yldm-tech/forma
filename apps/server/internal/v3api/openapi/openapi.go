// Package openapi builds the OpenAPI 3.1 document of the v3 management API that GET /api/v3/openapi.json serves. Each feature module describes its own operations next to its handlers (a Spec), and the schemas of what they answer are inferred from the Go types the handlers serialize, so the document follows the code: internal/app's route test fails when a route that accepts an API key is missing from it, and the modules' tests validate real answers against it.
package openapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

// Schema is a JSON Schema (draft 2020-12, the dialect OpenAPI 3.1 uses).
type Schema = jsonschema.Schema

// Operation is one documented operation.
type Operation struct {
	Method string
	// Path is the OpenAPI form of the route: /api/v3/surveys/{surveyId}.
	Path        string
	ID          string
	Tag         string
	Summary     string
	Description string
	// Public operations take no credentials; every other one takes an API key (or the session of a signed-in browser).
	Public     bool
	Parameters []Parameter
	// Body is the schema of the JSON request body, or nil when the operation reads none. BodyOptional marks a body that may be left out.
	Body         *jsonschema.Schema
	BodyOptional bool
	// Responses are the answers other than problems, by status.
	Responses []Response
	// Problems are the statuses the operation answers with a problem document, besides the 401 and 429 every authenticated operation can give.
	Problems []int
}

// Parameter is a path or query parameter.
type Parameter struct {
	Name        string
	In          string
	Description string
	Required    bool
	Schema      *jsonschema.Schema
}

// Response is a successful answer.
type Response struct {
	Status      int
	Description string
	// Schema is the JSON body's schema; nil for an answer without a body.
	Schema *jsonschema.Schema
}

// Spec is one module's part of the document: its operations and the named schemas they reference with Ref.
type Spec struct {
	Operations []Operation
	Schemas    map[string]*jsonschema.Schema
}

// ProblemSchema is the name of the shared problem document schema.
const ProblemSchema = "Problem"

// Ref points at a named schema of the document.
func Ref(name string) *jsonschema.Schema {
	return &jsonschema.Schema{Ref: "#/components/schemas/" + name}
}

// typeSchemas are the types whose JSON form reflection cannot see: a UUID is a string, and a raw JSON value is any value.
var typeSchemas = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[uuid.UUID]():       {Type: "string", Format: "uuid"},
	reflect.TypeFor[json.RawMessage](): {Description: "Any JSON value."},
}

// Infer is the schema of the JSON a value of T marshals to. The types are fixed at compile time, so a type the inference cannot read is a programming error the document's tests catch; it panics.
func Infer[T any]() *jsonschema.Schema {
	return InferWith[T](nil)
}

// InferWith is Infer with the schemas of types whose JSON form their fields do not show, such as a type with its own MarshalJSON.
func InferWith[T any](overrides map[reflect.Type]*jsonschema.Schema) *jsonschema.Schema {
	types := maps.Clone(typeSchemas)
	maps.Copy(types, overrides)
	s, err := jsonschema.For[T](&jsonschema.ForOptions{TypeSchemas: types})
	if err != nil {
		panic(fmt.Sprintf("openapi: %v", err))
	}
	return s
}

// NotNull drops null from the inferred types of s's named properties and returns s. Inference types a Go slice, map or pointer as possibly null, since a nil one marshals to null; a property the handler always fills is published as never null, so generated clients need no null check and the modules' tests catch a handler that starts answering null. A name that is not a nullable property of s is a programming error; it panics.
func NotNull(s *jsonschema.Schema, names ...string) *jsonschema.Schema {
	for _, name := range names {
		p := s.Properties[name]
		if p == nil || !slices.Contains(p.Types, "null") {
			panic(fmt.Sprintf("openapi: %q is not a nullable property", name))
		}
		types := slices.DeleteFunc(slices.Clone(p.Types), func(t string) bool { return t == "null" })
		if len(types) == 1 {
			p.Type, p.Types = types[0], nil
		} else {
			p.Types = types
		}
	}
	return s
}

// Object is a closed object schema with the given properties, of which required must be present.
func Object(description string, properties map[string]*jsonschema.Schema, required ...string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "object", Description: description, Properties: properties, Required: required, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}
}

// String is a string schema.
func String(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Description: description}
}

// Enum is a string schema that takes one of values.
func Enum(description string, values ...string) *jsonschema.Schema {
	s := String(description)
	for _, v := range values {
		s.Enum = append(s.Enum, v)
	}
	return s
}

// EnumList is a list of values, each one of values: a query parameter repeated, or its values separated by commas.
func EnumList(values ...string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "array", Items: Enum("", values...)}
}

// UUID is a UUID string schema.
func UUID(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Format: "uuid", Description: description}
}

// Integer is an integer schema between minimum and maximum, with its default.
func Integer(description string, minimum, maximum, def int) *jsonschema.Schema {
	lo, hi := float64(minimum), float64(maximum)
	return &jsonschema.Schema{Type: "integer", Description: description, Minimum: &lo, Maximum: &hi, Default: json.RawMessage(strconv.Itoa(def))}
}

// Nullable is s that also accepts null.
func Nullable(s *jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{s, {Type: "null"}}}
}

// Data is the {data} envelope of a single resource.
func Data(s *jsonschema.Schema) *jsonschema.Schema {
	return Object("", map[string]*jsonschema.Schema{"data": s}, "data")
}

// List is the {data, meta} envelope of a page of items.
func List(item, meta *jsonschema.Schema) *jsonschema.Schema {
	return Object("", map[string]*jsonschema.Schema{"data": {Type: "array", Items: item}, "meta": meta}, "data", "meta")
}

// PathID is a UUID path parameter.
func PathID(name, description string) Parameter {
	return Parameter{Name: name, In: "path", Required: true, Description: description, Schema: UUID("")}
}

// Query is an optional query parameter.
func Query(name, description string, s *jsonschema.Schema) Parameter {
	return Parameter{Name: name, In: "query", Description: description, Schema: s}
}

// WorkspaceQuery is the required workspaceId query parameter of the workspace lists.
func WorkspaceQuery() Parameter {
	return Parameter{Name: "workspaceId", In: "query", Required: true, Description: "The workspace to list. GET /api/v3/me lists the workspaces the caller may use.", Schema: UUID("")}
}

// CursorQuery is the cursor of a paginated list: the nextCursor of the previous page.
func CursorQuery() Parameter {
	return Query("cursor", "The `meta.nextCursor` of the previous page; leave it out for the first page.", String(""))
}

// problem is the RFC 9457 problem document every v3 failure is answered with.
type problem struct {
	Type      string `json:"type,omitempty" jsonschema:"A URI naming the kind of problem, when there is one."`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	RequestID string `json:"requestId" jsonschema:"The X-Request-Id of the answer; quote it when you report a problem."`
	Instance  string `json:"instance,omitempty" jsonschema:"The request path."`
	Code      string `json:"code,omitempty" jsonschema:"A stable machine-readable code, such as bad_request, not_authenticated, forbidden, not_found, conflict, unprocessable_content, payload_too_large, too_many_requests or internal_server_error; some operations add their own."`
	// Details is free-form context some problems add.
	Details       map[string]any `json:"details,omitempty"`
	InvalidParams []invalidParam `json:"invalid_params,omitempty" jsonschema:"The inputs that were refused, one entry each."`
}

type invalidParam struct {
	Name          string `json:"name" jsonschema:"The parameter or the dotted path in the body."`
	Reason        string `json:"reason"`
	Code          string `json:"code,omitempty"`
	Identifier    string `json:"identifier,omitempty"`
	ReferenceType string `json:"referenceType,omitempty"`
	MissingID     string `json:"missingId,omitempty"`
	FirstUsedAt   string `json:"firstUsedAt,omitempty"`
	ConflictsWith string `json:"conflictsWith,omitempty"`
}

// Document is the OpenAPI document of specs, as JSON. serverURL is the instance's address; an empty one leaves the servers list out, so paths are relative to wherever the document was read from.
func Document(serverURL string, specs ...Spec) ([]byte, error) {
	schemas := map[string]*jsonschema.Schema{ProblemSchema: NotNull(Infer[problem](), "invalid_params")}
	paths := map[string]map[string]any{}
	responses := map[string]any{}
	for _, spec := range specs {
		for name, s := range spec.Schemas {
			if _, taken := schemas[name]; taken {
				return nil, fmt.Errorf("openapi: schema %q is defined twice", name)
			}
			schemas[name] = s
		}
		for _, op := range spec.Operations {
			item := paths[op.Path]
			if item == nil {
				item = map[string]any{}
				paths[op.Path] = item
			}
			method := strings.ToLower(op.Method)
			if _, taken := item[method]; taken {
				return nil, fmt.Errorf("openapi: %s %s is described twice", op.Method, op.Path)
			}
			item[method] = operation(op, responses)
		}
	}
	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Forma management API",
			"version":     "v3",
			"description": "Manage surveys, workflows and the attributes and actions in-app surveys target. Send an API key in the `X-Api-Key` header, or as `Authorization: Bearer fbk_…`; organization owners and managers create keys under Organization Settings → API Keys. `GET /api/v3/me` tells a key which organization and workspaces it may use. Failures are RFC 9457 problem documents (`application/problem+json`). Each API key and each user may make 100 requests a minute; a 429 says when to retry in `Retry-After`.",
		},
		"paths": paths,
		"components": map[string]any{
			"schemas":   schemas,
			"responses": responses,
			"securitySchemes": map[string]any{
				"apiKey": map[string]any{"type": "apiKey", "in": "header", "name": "X-Api-Key", "description": "An organization API key."},
				"bearer": map[string]any{"type": "http", "scheme": "bearer", "description": "An organization API key in the current `fbk_` format, as a bearer token."},
			},
		},
		"security": []map[string][]string{{"apiKey": {}}, {"bearer": {}}},
	}
	if serverURL != "" {
		doc["servers"] = []map[string]string{{"url": strings.TrimRight(serverURL, "/")}}
	}
	return json.Marshal(doc)
}

// operation is op as an OpenAPI operation object. Its problem answers refer to shared answers it adds to shared, one per status.
func operation(op Operation, shared map[string]any) map[string]any {
	out := map[string]any{"operationId": op.ID, "summary": op.Summary, "tags": []string{op.Tag}}
	if op.Description != "" {
		out["description"] = op.Description
	}
	if op.Public {
		out["security"] = []map[string][]string{}
	}
	if len(op.Parameters) > 0 {
		params := make([]map[string]any, len(op.Parameters))
		for i, p := range op.Parameters {
			params[i] = map[string]any{"name": p.Name, "in": p.In, "required": p.Required, "schema": p.Schema}
			if p.Description != "" {
				params[i]["description"] = p.Description
			}
		}
		out["parameters"] = params
	}
	if op.Body != nil {
		out["requestBody"] = map[string]any{"required": !op.BodyOptional, "content": map[string]any{"application/json": map[string]any{"schema": op.Body}}}
	}
	responses := map[string]any{}
	for _, r := range op.Responses {
		answer := map[string]any{"description": r.Description}
		if r.Schema != nil {
			answer["content"] = map[string]any{"application/json": map[string]any{"schema": r.Schema}}
		}
		if r.Status == http.StatusCreated {
			answer["headers"] = map[string]any{"Location": map[string]any{"description": "The path of the created resource.", "schema": map[string]string{"type": "string"}}}
		}
		responses[strconv.Itoa(r.Status)] = answer
	}
	problems := slices.Clone(op.Problems)
	if !op.Public {
		problems = append(problems, http.StatusUnauthorized, http.StatusTooManyRequests)
	}
	for _, status := range problems {
		name := problemResponseName(status)
		shared[name] = problemResponse(http.StatusText(status))
		responses[strconv.Itoa(status)] = map[string]string{"$ref": "#/components/responses/" + name}
	}
	shared[unexpectedResponse] = problemResponse("An unexpected error.")
	responses["default"] = map[string]string{"$ref": "#/components/responses/" + unexpectedResponse}
	out["responses"] = responses
	return out
}

// unexpectedResponse names the shared answer to a failure no operation expects.
const unexpectedResponse = "UnexpectedProblem"

// problemResponseName names the shared problem answer of a status: Forbidden for 403.
func problemResponseName(status int) string {
	return strings.ReplaceAll(http.StatusText(status), " ", "")
}

func problemResponse(description string) map[string]any {
	return map[string]any{"description": description, "content": map[string]any{"application/problem+json": map[string]any{"schema": Ref(ProblemSchema)}}}
}
