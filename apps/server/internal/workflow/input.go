package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
)

// maxBodyBytes is the v3 API's request body limit, which the workflow routes enforced themselves.
const maxBodyBytes = 2 << 20

// The details of a request forma2's workflow routes refused before reading its fields.
const (
	bodyTooLarge   = "Request body is too large."
	malformedBody  = "Malformed JSON body."
	invalidPayload = "The request payload is invalid."
)

var (
	workflowStatuses = []string{StatusDraft, StatusEnabled, StatusDisabled, StatusArchived}
	runStatuses      = []string{RunQueued, RunRunning, RunCompleted, RunFailed, RunCanceled}
	sortOrders       = []string{sortCreatedAt, sortUpdatedAt, sortName}
)

// readBody is forma2's readJsonBody: a body over the limit, by its Content-Length or its bytes, is a 400, as is one that is not JSON. allowEmpty reads an empty body as {}.
func readBody(c *gin.Context, allowEmpty bool) (any, error) {
	if c.Request.ContentLength > maxBodyBytes {
		return nil, v3api.BadRequest(bodyTooLarge)
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if len(raw) > maxBodyBytes {
		return nil, v3api.BadRequest(bodyTooLarge)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		if allowEmpty {
			return map[string]any{}, nil
		}
		return nil, v3api.BadRequest(malformedBody)
	}
	v, err := decodeJSON(raw)
	if err != nil || !json.Valid(raw) {
		return nil, v3api.BadRequest(malformedBody)
	}
	return v, nil
}

// payloadProblem is the 400 a body or query that breaks its contract answers with, every issue named by its dotted path.
func payloadProblem(issues []Issue) *v3api.Problem {
	params := make([]v3api.InvalidParam, len(issues))
	for i, is := range issues {
		params[i] = v3api.InvalidParam{Name: is.Name(), Reason: is.Message}
	}
	return v3api.BadRequest(invalidPayload, params...)
}

// strict reports the keys of m a strict zod object does not declare.
func (p *parser) strict(m map[string]any, at []any, known ...string) {
	var unknown []string
	for k := range m {
		if !slices.Contains(known, k) {
			unknown = append(unknown, strconv.Quote(k))
		}
	}
	if len(unknown) == 0 {
		return
	}
	slices.Sort(unknown)
	if len(unknown) == 1 {
		p.add(at, "Unrecognized key: "+unknown[0])
		return
	}
	p.add(at, "Unrecognized keys: "+strings.Join(unknown, ", "))
}

// description reads `z.string().max(500).nullable().optional()`: set reports whether the key was present, so an explicit null clears it.
func (p *parser) description(m map[string]any) (value *string, set bool) {
	v, present := m["description"]
	if !present {
		return nil, false
	}
	if v == nil {
		return nil, true
	}
	s, ok := v.(string)
	if !ok {
		p.expected([]any{"description"}, "string", v, true)
		return nil, true
	}
	p.lengthChecks(s, []any{"description"}, -1, 500)
	return &s, true
}

// definitionMember parses the body's definition through ZWorkflowDefinition, its issues under "definition".
func (p *parser) definitionMember(v any, present bool) *Definition {
	if !present {
		p.expected([]any{"definition"}, "object", v, present)
		return nil
	}
	d, issues, aborted := parseDefinitionValue(v, []any{"definition"}, false)
	p.issues = append(p.issues, issues...)
	p.aborted = p.aborted || aborted
	return d
}

func (p *parser) uuidMember(m map[string]any, key string) uuid.UUID {
	s := p.str(m, key, nil, -1, -1)
	if _, ok := m[key].(string); !ok {
		return uuid.Nil
	}
	p.uuidString(s, []any{key})
	id, _ := uuid.Parse(s)
	return id
}

type createInput struct {
	WorkspaceID uuid.UUID
	Name        string
	Description *string
	Definition  *Definition
}

// parseCreate is ZCreateWorkflowInput: a strict object of the workspace, a name of 1 to 120 characters, an optional description, an optional status that can only be draft, and a storable definition.
func parseCreate(v any) (*createInput, []Issue) {
	p := &parser{}
	m, ok := p.object(v, true, nil)
	if !ok {
		return nil, p.issues
	}
	in := &createInput{}
	in.WorkspaceID = p.uuidMember(m, "workspaceId")
	in.Name = p.str(m, "name", nil, 1, 120)
	in.Description, _ = p.description(m)
	if status, present := m["status"]; present && status != StatusDraft {
		p.fail([]any{"status"}, `Invalid input: expected "draft"`)
	}
	def, present := m["definition"]
	in.Definition = p.definitionMember(def, present)
	p.strict(m, nil, "workspaceId", "name", "description", "status", "definition")
	if len(p.issues) > 0 {
		return nil, p.issues
	}
	return in, nil
}

type patchInput struct {
	Name           *string
	Description    *string
	DescriptionSet bool
	Definition     *Definition
}

// parsePatch is ZPatchWorkflowInput: a strict object of an optional name, description and definition, at least one of them. The status is never patched.
func parsePatch(v any) (*patchInput, []Issue) {
	p := &parser{}
	m, ok := p.object(v, true, nil)
	if !ok {
		return nil, p.issues
	}
	in := &patchInput{}
	in.Name = p.optStr(m, "name", nil, 1, 120)
	in.Description, in.DescriptionSet = p.description(m)
	if def, present := m["definition"]; present {
		in.Definition = p.definitionMember(def, present)
	}
	p.strict(m, nil, "name", "description", "definition")
	if len(m) == 0 {
		p.add(nil, "At least one field must be provided")
	}
	if len(p.issues) > 0 {
		return nil, p.issues
	}
	return in, nil
}

// parseDuplicate is ZDuplicateWorkflowInput: a strict object with an optional name.
func parseDuplicate(v any) (*string, []Issue) {
	p := &parser{}
	m, ok := p.object(v, true, nil)
	if !ok {
		return nil, p.issues
	}
	name := p.optStr(m, "name", nil, 1, 120)
	p.strict(m, nil, "name")
	if len(p.issues) > 0 {
		return nil, p.issues
	}
	return name, nil
}

// query reads forma2's list query families. Unknown parameters are ignored.
type query struct {
	values url.Values
	p      parser
}

// multi is forma2's multiValue: repeated keys or comma-separated values, blanks dropped; nil when none remain.
func (q *query) multi(key string) []string {
	var out []string
	for _, v := range q.values[key] {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func (q *query) enumList(key, name string, allowed []string) []string {
	values := q.multi(key)
	for i, v := range values {
		if !slices.Contains(allowed, v) {
			q.p.fail([]any{name, i}, oneOf(allowed))
		}
	}
	return values
}

// get is URLSearchParams.get: the first value, and whether the key is present.
func (q *query) get(key string) (string, bool) {
	vs, ok := q.values[key]
	if !ok || len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}

func (q *query) workspaceID() uuid.UUID {
	v, ok := q.get("workspaceId")
	if !ok {
		q.p.expected([]any{"workspaceId"}, "string", nil, false)
		return uuid.Nil
	}
	return q.uuid(v, "workspaceId")
}

func (q *query) uuid(v, name string) uuid.UUID {
	id, err := uuid.Parse(v)
	if err != nil || len(v) != 36 {
		q.p.add([]any{name}, "Invalid UUID")
		return uuid.Nil
	}
	return id
}

func (q *query) optUUID(key string) *uuid.UUID {
	v, ok := q.get(key)
	if !ok {
		return nil
	}
	id := q.uuid(v, key)
	return &id
}

// limit is Number(limit) through z.number().int().min(1).max(100).default(20).
func (q *query) limit() int {
	v, ok := q.get("limit")
	if !ok {
		return 20
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if strings.TrimSpace(v) == "" {
		f, err = 0, nil
	}
	switch {
	case err != nil:
		q.p.fail([]any{"limit"}, "Invalid input: expected number, received NaN")
	case math.IsInf(f, 0):
		q.p.fail([]any{"limit"}, "Invalid input: expected number, received number")
	case f != float64(int64(f)):
		q.p.add([]any{"limit"}, "Invalid input: expected int, received number")
	case f < 1:
		q.p.add([]any{"limit"}, "Too small: expected number to be >=1")
	case f > 100:
		q.p.add([]any{"limit"}, "Too big: expected number to be <=100")
	}
	return int(f)
}

func (q *query) optString(key, name string, maximum int) string {
	v, ok := q.get(key)
	if !ok {
		return ""
	}
	q.p.lengthChecks(v, []any{name}, 1, maximum)
	return v
}

// parseListQuery is forma2's parseListWorkflowsQuery.
func parseListQuery(values url.Values) (listQuery, string, error) {
	q := &query{values: values}
	out := listQuery{WorkspaceID: q.workspaceID(), Limit: q.limit(), SortBy: sortUpdatedAt}
	cursor := q.optString("cursor", "cursor", -1)
	out.StatusIn = q.enumList("filter[status][in]", "statusIn", workflowStatuses)
	out.NameContains = q.optString("filter[name][contains]", "nameContains", 512)
	if v, ok := q.get("sortBy"); ok {
		if slices.Contains(sortOrders, v) {
			out.SortBy = v
		} else {
			q.p.fail([]any{"sortBy"}, oneOf(sortOrders))
		}
	}
	if len(q.p.issues) > 0 {
		return out, "", payloadProblem(q.p.issues)
	}
	return out, cursor, nil
}

// parseRunListQuery is forma2's parseListWorkflowRunsQuery. filter[isDryRun][eq] takes only "true" or "false".
func parseRunListQuery(values url.Values) (runQuery, string, error) {
	q := &query{values: values}
	out := runQuery{WorkspaceID: q.workspaceID(), Limit: q.limit()}
	cursor := q.optString("cursor", "cursor", -1)
	out.WorkflowID = q.optUUID("workflowId")
	out.ResponseID = q.optUUID("responseId")
	out.StatusIn = q.enumList("filter[status][in]", "statusIn", runStatuses)
	if v, ok := q.get("filter[isDryRun][eq]"); ok {
		switch v {
		case "true", "false":
			b := v == "true"
			out.IsDryRun = &b
		default:
			q.p.fail([]any{"isDryRun"}, "Invalid input: expected boolean, received string")
		}
	}
	if len(q.p.issues) > 0 {
		return out, "", payloadProblem(q.p.issues)
	}
	return out, cursor, nil
}

// paramID reads a route id; the rewrite's ids are UUIDs.
func paramID(c *gin.Context, name string) (uuid.UUID, error) {
	raw := c.Param(name)
	id, err := uuid.Parse(raw)
	if err != nil || len(raw) != 36 {
		return uuid.Nil, &v3api.Problem{Status: http.StatusBadRequest, Title: "Bad Request", Detail: "Invalid route parameters", Code: "bad_request",
			InvalidParams: []v3api.InvalidParam{{Name: name, Reason: "Invalid UUID"}}}
	}
	return id, nil
}
