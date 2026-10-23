package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
)

// toolsJSON is the tools/list forma2 answered, in its order, recorded from its zod input schemas through the MCP SDK: names, titles, descriptions, annotations and input schemas. One change: the ids a tool takes (workspaceId, surveyId, workflowId, runId, responseId, and a workflow trigger's surveyId) accept the rewrite's UUIDs as well as the cuid2 ids forma2 issued, which resolve to the rows migrated from them.
//
//go:embed tools.json
var toolsJSON []byte

// definition is an advertised tool with the schema its arguments are validated against.
type definition struct {
	tool   *sdk.Tool
	schema *jsonschema.Resolved
}

func loadTools() ([]definition, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(toolsJSON, &raw); err != nil {
		return nil, fmt.Errorf("decode tools: %w", err)
	}
	out := make([]definition, len(raw))
	for i, r := range raw {
		var tool sdk.Tool
		if err := json.Unmarshal(r, &tool); err != nil {
			return nil, fmt.Errorf("decode tool: %w", err)
		}
		var fields struct {
			InputSchema json.RawMessage `json:"inputSchema"`
		}
		if err := json.Unmarshal(r, &fields); err != nil {
			return nil, fmt.Errorf("decode tool %s: %w", tool.Name, err)
		}
		// Advertised exactly as recorded.
		tool.InputSchema = fields.InputSchema
		schema, err := validationSchema(fields.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("tool %s: %w", tool.Name, err)
		}
		out[i] = definition{tool: &tool, schema: schema}
	}
	return out, nil
}

// validationSchema resolves an input schema for validation. zod's email pattern uses lookaheads Go's regexp lacks; a pattern it cannot compile is left out here, and the v3 operation the tool calls validates the value itself.
func validationSchema(raw json.RawMessage) (*jsonschema.Resolved, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode input schema: %w", err)
	}
	dropUncompilablePatterns(doc)
	cleaned, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode input schema: %w", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(cleaned, &schema); err != nil {
		return nil, fmt.Errorf("decode input schema: %w", err)
	}
	return schema.Resolve(nil)
}

func dropUncompilablePatterns(node any) {
	switch v := node.(type) {
	case map[string]any:
		if p, ok := v["pattern"].(string); ok {
			if _, err := regexp.Compile(p); err != nil {
				delete(v, "pattern")
			}
		}
		for _, child := range v {
			dropUncompilablePatterns(child)
		}
	case []any:
		for _, child := range v {
			dropUncompilablePatterns(child)
		}
	}
}

// arguments validates a call's arguments against the tool's schema after applying its defaults, as the SDK does for typed tools: every object in a schema is strict, so an undeclared or misspelled key fails here and names itself.
func (d definition) arguments(raw json.RawMessage) (map[string]any, error) {
	args := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("unmarshaling arguments: %w", err)
		}
	}
	var instance any = args
	if err := d.schema.ApplyDefaults(&instance); err != nil {
		return nil, fmt.Errorf("applying schema defaults: %w", err)
	}
	if err := d.schema.Validate(&instance); err != nil {
		return nil, err
	}
	out, ok := instance.(map[string]any)
	if !ok {
		return nil, errors.New("arguments must be an object")
	}
	return out, nil
}

// toolSpec is what forma2's registerScopedTool held besides the advertised definition: the scopes the gate requires (all of them, or any one with anyOf) and the handler.
type toolSpec struct {
	scopes []string
	anyOf  bool
	run    func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult
}

func (s toolSpec) allowed(p *principal) bool {
	if s.anyOf {
		return p.hasAny(s.scopes)
	}
	return p.hasAll(s.scopes)
}

// toolHandler validates the arguments, then applies the scope gate before the tool can reach an operation, forma2's registerScopedTool.
func (h *Handler) toolHandler(d definition, spec toolSpec) sdk.ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		p, ok := principalFrom(ctx)
		if !ok {
			return nil, errors.New("tool called without an authenticated caller")
		}
		args, err := d.arguments(req.Params.Arguments)
		if err != nil {
			res := &sdk.CallToolResult{}
			res.SetError(fmt.Errorf(`validating "arguments": %w`, err))
			return res, nil
		}
		if !spec.allowed(p) {
			return errorResult(toolError{
				Status: http.StatusForbidden, Title: "Forbidden", Code: "forbidden", RequestID: p.requestID,
				Detail: "OAuth token does not include the required MCP scope: " + strings.Join(spec.scopes, " "),
			}), nil
		}
		return spec.run(ctx, h, p, args), nil
	}
}

const (
	scopeSurveysRead    = "surveys:read"
	scopeSurveysWrite   = "surveys:write"
	scopeWorkflowsRead  = "workflows:read"
	scopeWorkflowsWrite = "workflows:write"
)

// toolSpecs are forma2's tools by name.
var toolSpecs = map[string]toolSpec{
	"list_surveys": {scopes: []string{scopeSurveysRead}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodGet, path: "/api/v3/surveys", query: listSurveysQuery(args)})
	}},
	"get_survey": {scopes: []string{scopeSurveysRead}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		q := url.Values{}
		for _, lang := range strs(args, "lang") {
			q.Add("lang", lang)
		}
		return h.run(ctx, p, operation{method: http.MethodGet, path: surveyPath(args), query: q})
	}},
	"create_survey": {scopes: []string{scopeSurveysWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		args["workspaceId"] = id(str(args, "workspaceId"))
		return h.run(ctx, p, operation{method: http.MethodPost, path: "/api/v3/surveys", body: args})
	}},
	// A dry run of a create or patch payload: read access is enough, as forma2 gated it.
	"validate_survey": {scopes: []string{scopeSurveysRead}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		if surveyID := str(args, "surveyId"); surveyID != "" {
			args["surveyId"] = id(surveyID)
		}
		return h.run(ctx, p, operation{method: http.MethodPost, path: "/api/v3/surveys/validate", body: args})
	}},
	"patch_survey": {scopes: []string{scopeSurveysWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodPatch, path: surveyPath(args), body: args["data"]})
	}},
	"delete_survey": {scopes: []string{scopeSurveysWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodDelete, path: surveyPath(args)})
	}},
	"archive_survey": {scopes: []string{scopeSurveysWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodPost, path: surveyPath(args) + "/archive"})
	}},
	"restore_survey": {scopes: []string{scopeSurveysWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodPost, path: surveyPath(args) + "/restore"})
	}},
	"list_workflows": {scopes: []string{scopeWorkflowsRead}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodGet, path: "/api/v3/workflows", query: listWorkflowsQuery(args)})
	}},
	"get_workflow": {scopes: []string{scopeWorkflowsRead}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodGet, path: workflowPath(args)})
	}},
	"list_workflow_runs": {scopes: []string{scopeWorkflowsRead}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodGet, path: "/api/v3/workflows/runs", query: listWorkflowRunsQuery(args)})
	}},
	"get_workflow_run": {scopes: []string{scopeWorkflowsRead}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodGet, path: "/api/v3/workflows/runs/" + url.PathEscape(id(str(args, "runId")))})
	}},
	// A dry run that creates nothing: read access is enough.
	"test_workflow": {scopes: []string{scopeWorkflowsRead}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodPost, path: workflowPath(args) + "/test"})
	}},
	"create_workflow": {scopes: []string{scopeWorkflowsWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		args["workspaceId"] = id(str(args, "workspaceId"))
		return h.run(ctx, p, operation{method: http.MethodPost, path: "/api/v3/workflows", body: args})
	}},
	"patch_workflow": {scopes: []string{scopeWorkflowsWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodPatch, path: workflowPath(args), body: args["data"]})
	}},
	"duplicate_workflow": {scopes: []string{scopeWorkflowsWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		body := map[string]any{}
		if name := str(args, "name"); name != "" {
			body["name"] = name
		}
		return h.run(ctx, p, operation{method: http.MethodPost, path: workflowPath(args) + "/duplicate", body: body})
	}},
	"delete_workflow": {scopes: []string{scopeWorkflowsWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodDelete, path: workflowPath(args)})
	}},
	"enable_workflow":    workflowLifecycle("enable"),
	"disable_workflow":   workflowLifecycle("disable"),
	"archive_workflow":   workflowLifecycle("archive"),
	"unarchive_workflow": workflowLifecycle("unarchive"),
	// The workspaceId every other tool needs is found here, so either read scope admits it.
	"list_workspaces": {scopes: []string{scopeSurveysRead, scopeWorkflowsRead}, anyOf: true, run: func(ctx context.Context, h *Handler, p *principal, _ map[string]any) *sdk.CallToolResult {
		return h.listWorkspaces(ctx, p)
	}},
}

func workflowLifecycle(action string) toolSpec {
	return toolSpec{scopes: []string{scopeWorkflowsWrite}, run: func(ctx context.Context, h *Handler, p *principal, args map[string]any) *sdk.CallToolResult {
		return h.run(ctx, p, operation{method: http.MethodPost, path: workflowPath(args) + "/" + action})
	}}
}

// id is a tool's id argument as the v3 API takes it: a UUID as it is, a forma2 id as the id its row was migrated to.
func id(raw string) string {
	resolved, ok := legacyid.Resolve(raw)
	if !ok {
		return raw
	}
	return resolved.String()
}

func surveyPath(args map[string]any) string {
	return "/api/v3/surveys/" + url.PathEscape(id(str(args, "surveyId")))
}

func workflowPath(args map[string]any) string {
	return "/api/v3/workflows/" + url.PathEscape(id(str(args, "workflowId")))
}

func str(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func obj(args map[string]any, key string) map[string]any {
	m, _ := args[key].(map[string]any)
	return m
}

func strs(args map[string]any, key string) []string {
	list, _ := args[key].([]any)
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// limit is the page size, 20 unless given.
func limit(args map[string]any) string {
	if n, ok := args["limit"].(float64); ok {
		return strconv.Itoa(int(n))
	}
	return "20"
}

// listSurveysQuery is forma2's buildListSurveysSearchParams.
func listSurveysQuery(args map[string]any) url.Values {
	q := url.Values{"workspaceId": {id(str(args, "workspaceId"))}, "limit": {limit(args)}}
	if cursor := str(args, "cursor"); cursor != "" {
		q.Set("cursor", cursor)
	}
	if include, ok := args["includeTotalCount"].(bool); ok && !include {
		q.Set("includeTotalCount", "false")
	}
	if sortBy := str(args, "sortBy"); sortBy != "" {
		q.Set("sortBy", sortBy)
	}
	filter := obj(args, "filter")
	if contains := str(obj(filter, "name"), "contains"); contains != "" {
		q.Set("filter[name][contains]", contains)
	}
	for _, status := range strs(obj(filter, "status"), "in") {
		q.Add("filter[status][in]", status)
	}
	for _, t := range strs(obj(filter, "type"), "in") {
		q.Add("filter[type][in]", t)
	}
	return q
}

// listWorkflowsQuery is forma2's buildListWorkflowsSearchParams.
func listWorkflowsQuery(args map[string]any) url.Values {
	q := url.Values{"workspaceId": {id(str(args, "workspaceId"))}, "limit": {limit(args)}}
	if cursor := str(args, "cursor"); cursor != "" {
		q.Set("cursor", cursor)
	}
	if sortBy := str(args, "sortBy"); sortBy != "" {
		q.Set("sortBy", sortBy)
	}
	filter := obj(args, "filter")
	if contains := str(obj(filter, "name"), "contains"); contains != "" {
		q.Set("filter[name][contains]", contains)
	}
	for _, status := range strs(obj(filter, "status"), "in") {
		q.Add("filter[status][in]", status)
	}
	return q
}

// listWorkflowRunsQuery is forma2's buildListWorkflowRunsSearchParams.
func listWorkflowRunsQuery(args map[string]any) url.Values {
	q := url.Values{"workspaceId": {id(str(args, "workspaceId"))}, "limit": {limit(args)}}
	if cursor := str(args, "cursor"); cursor != "" {
		q.Set("cursor", cursor)
	}
	if workflowID := str(args, "workflowId"); workflowID != "" {
		q.Set("workflowId", id(workflowID))
	}
	if responseID := str(args, "responseId"); responseID != "" {
		q.Set("responseId", id(responseID))
	}
	filter := obj(args, "filter")
	for _, status := range strs(obj(filter, "status"), "in") {
		q.Add("filter[status][in]", status)
	}
	if dryRun, ok := filter["isDryRun"].(bool); ok {
		q.Set("filter[isDryRun][eq]", strconv.FormatBool(dryRun))
	}
	return q
}
