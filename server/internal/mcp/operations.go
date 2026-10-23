package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/workflow"
)

// newOperations mounts the v3 survey and workflow operations on an engine of their own, which tool calls are served by in-process, as forma2's tools called the v3 operations directly. The caller is already authenticated and charged its rate limit, so no guard runs here.
func newOperations(surveys *survey.Handler, workflows *workflow.Handler, log *slog.Logger) http.Handler {
	r := gin.New()
	r.Use(
		gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
			log.ErrorContext(c.Request.Context(), "panic serving an MCP tool operation", "route", c.FullPath(), "panic", recovered)
			v3api.WriteProblem(c, v3api.Internal())
		}),
		func(c *gin.Context) {
			httpserver.SetRequestID(c, c.GetHeader("X-Request-Id"))
			c.Next()
		},
		authz.DecisionMemo(),
	)
	surveys.MountV3(r.Group("/api/v3/surveys"))
	workflows.MountV3(r.Group("/api/v3/workflows"))
	return r
}

// operation is one v3 call a tool makes.
type operation struct {
	method string
	path   string
	query  url.Values
	body   any
}

// run serves op as the caller and maps its answer to a tool result. Audit events it emits record the MCP resource as their apiUrl, as forma2's runMcpMutation did.
func (h *Handler) run(ctx context.Context, p *principal, op operation) *sdk.CallToolResult {
	body := []byte{}
	if op.body != nil {
		encoded, err := json.Marshal(op.body)
		if err != nil {
			return h.failed(ctx, p, err)
		}
		body = encoded
	}
	target := op.path
	if len(op.query) > 0 {
		target += "?" + op.query.Encode()
	}
	ctx = audit.WithAPIURL(v3api.WithActor(ctx, p.actor), h.urls.Resource)
	req, err := http.NewRequestWithContext(ctx, op.method, target, bytes.NewReader(body))
	if err != nil {
		return h.failed(ctx, p, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", p.requestID)
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	h.ops.ServeHTTP(rec, req)
	return toolResult(rec.status, rec.header, rec.body.Bytes(), p.requestID)
}

func (h *Handler) failed(ctx context.Context, p *principal, err error) *sdk.CallToolResult {
	h.log.ErrorContext(ctx, "MCP tool failed", "request_id", p.requestID, "err", err)
	return errorResult(toolError{Status: http.StatusInternalServerError, Title: "Internal Server Error", Detail: "An unexpected error occurred.", RequestID: p.requestID, Code: "internal_server_error"})
}

// recorder captures an in-process response.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *recorder) WriteHeader(status int)      { r.status = status }

// toolError is a failed operation as a tool sees it, forma2's TMcpErrorPayload.
type toolError struct {
	Status        int             `json:"status"`
	Title         string          `json:"title"`
	Detail        string          `json:"detail"`
	RequestID     string          `json:"requestId"`
	Code          string          `json:"code,omitempty"`
	InvalidParams json.RawMessage `json:"invalid_params,omitempty"`
}

// toolResult is forma2's responseToMcpToolResult: a success is the v3 body with the request id added, a failure the problem's status, title, detail, code and offending params. The request id is the body's, else the response header's, else the call's.
func toolResult(status int, header http.Header, body []byte, fallbackRequestID string) *sdk.CallToolResult {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		fields, body = map[string]json.RawMessage{}, []byte("{}")
	}
	requestID := fallbackRequestID
	if id := header.Get("X-Request-Id"); id != "" {
		requestID = id
	}
	var bodyRequestID string
	if json.Unmarshal(fields["requestId"], &bodyRequestID) == nil && bodyRequestID != "" {
		requestID = bodyRequestID
	}
	if status >= 200 && status < 300 {
		return result(withRequestID(body, fields, requestID), false)
	}
	e := toolError{Status: status, Title: "Error", Detail: http.StatusText(status), RequestID: requestID}
	if e.Detail == "" {
		e.Detail = "Request failed"
	}
	var s string
	if json.Unmarshal(fields["title"], &s) == nil && fields["title"] != nil {
		e.Title = s
	}
	if json.Unmarshal(fields["detail"], &s) == nil && fields["detail"] != nil {
		e.Detail = s
	}
	if json.Unmarshal(fields["code"], &s) == nil && fields["code"] != nil {
		e.Code = s
	}
	if raw := bytes.TrimSpace(fields["invalid_params"]); len(raw) > 0 && raw[0] == '[' {
		e.InvalidParams = raw
	}
	return errorResult(e)
}

// withRequestID adds requestId to a JSON object, keeping the body's own order.
func withRequestID(body []byte, fields map[string]json.RawMessage, requestID string) []byte {
	encodedID, _ := json.Marshal(requestID)
	if _, ok := fields["requestId"]; ok {
		fields["requestId"] = encodedID
		out, _ := json.Marshal(fields)
		return out
	}
	trimmed := bytes.TrimSpace(body)
	inner := strings.TrimSpace(string(trimmed[1 : len(trimmed)-1]))
	if inner == "" {
		return []byte(`{"requestId":` + string(encodedID) + `}`)
	}
	return []byte(`{` + inner + `,"requestId":` + string(encodedID) + `}`)
}

func errorResult(e toolError) *sdk.CallToolResult {
	payload, _ := json.Marshal(map[string]toolError{"error": e})
	return result(payload, true)
}

// result carries the payload both as structured content and as its JSON text.
func result(payload []byte, isError bool) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		StructuredContent: json.RawMessage(payload),
		Content:           []sdk.Content{&sdk.TextContent{Text: string(payload)}},
		IsError:           isError,
	}
}
