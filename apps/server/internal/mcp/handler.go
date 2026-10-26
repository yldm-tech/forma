// Package mcp is the MCP server at /api/mcp, forma2's apps/web/modules/mcp: Streamable HTTP without sessions, authenticated by an API key or an OAuth access token from the authorization server in internal/oauthserver, exposing forma2's survey, workflow and workspace tools and two of its own that read survey responses. The survey and workflow tools run the v3 operations in-process, so they answer with the v3 API's bodies and problems and are authorized by the same workspace checks; the response tools have no v3 operation and answer in the same shapes (responses.go).
package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/analysis"
	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/oauthserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
	"github.com/yldm-tech/forma/apps/server/internal/workflow"
)

// The server's identity, forma2's MCP_SERVER_NAME and MCP_SERVER_VERSION.
const (
	ServerName    = "forma-xm-suite"
	ServerVersion = "0.2.0"
)

// metadataCacheControl is how long the protected resource metadata may be cached, forma2's.
const metadataCacheControl = "public, max-age=15, stale-while-revalidate=15, stale-if-error=86400"

// Handler serves /api/mcp and the OAuth protected resource metadata MCP clients discover the authorization server through.
type Handler struct {
	urls      oauthserver.URLs
	db        *gorm.DB
	authz     *authz.Authorizer
	keys      *apikey.Authenticator
	verifier  *oauthserver.TokenVerifier
	limiter   *ratelimit.Limiter
	analysis  *analysis.Service
	ops       http.Handler
	transport http.Handler
	log       *slog.Logger
}

func NewHandler(cfg config.Config, db *gorm.DB, authorizer *authz.Authorizer, keys *apikey.Authenticator, verifier *oauthserver.TokenVerifier, limiter *ratelimit.Limiter, surveys *survey.Handler, workflows *workflow.Handler, analyses *analysis.Service, log *slog.Logger) (*Handler, error) {
	h := &Handler{
		urls: oauthserver.NewURLs(cfg), db: db, authz: authorizer, keys: keys, verifier: verifier, limiter: limiter, analysis: analyses,
		ops: newOperations(surveys, workflows, log), log: log,
	}
	server, err := h.newServer()
	if err != nil {
		return nil, err
	}
	// Stateless, as forma2 served it (protocol 2026-07-28 has no sessions, and older clients get a fresh session per request), answering with JSON rather than an event stream.
	h.transport = sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: v3api.MaxBodyBytes,
		// The Origin check is authenticate's, against WEBAPP_URL; the server is reached through proxies whose Host is not loopback.
		DisableLocalhostProtection: true,
	})
	return h, nil
}

// newServer registers the tools in forma2's order: surveys, workflows, then list_workspaces, followed by the response tools forma2 did not have. There are no resources or prompts.
func (h *Handler) newServer() (*sdk.Server, error) {
	server := sdk.NewServer(&sdk.Implementation{Name: ServerName, Version: ServerVersion}, &sdk.ServerOptions{Capabilities: &sdk.ServerCapabilities{}})
	definitions, err := loadTools()
	if err != nil {
		return nil, err
	}
	order := make([]string, len(definitions))
	for i, d := range definitions {
		order[i] = d.tool.Name
	}
	server.AddReceivingMiddleware(refuseSubscriptions, listToolsInOrder(order))
	for _, d := range definitions {
		spec, ok := toolSpecs[d.tool.Name]
		if !ok {
			return nil, fmt.Errorf("tool %q has no implementation", d.tool.Name)
		}
		server.AddTool(d.tool, h.toolHandler(d, spec))
	}
	return server, nil
}

// refuseSubscriptions answers subscriptions/listen with forma2's "Subscription limit reached" (maxSubscriptions: 0). Nothing would ever be notified on such a stream, since the tools never change, and each one would hold a connection open.
func refuseSubscriptions(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
		if method == "subscriptions/listen" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "Subscription limit reached"}
		}
		return next(ctx, method, req)
	}
}

// listToolsInOrder answers tools/list in forma2's registration order; the SDK lists tools by name. All 24 fit on the SDK's first page.
func listToolsInOrder(order []string) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			res, err := next(ctx, method, req)
			if list, ok := res.(*sdk.ListToolsResult); ok && err == nil {
				slices.SortStableFunc(list.Tools, func(a, b *sdk.Tool) int {
					return slices.Index(order, a.Name) - slices.Index(order, b.Name)
				})
			}
			return res, err
		}
	}
}

// Register mounts /api/mcp and the protected resource metadata. Only POST is served; other methods get 405, GET with an empty body and no Allow header, as forma2's route answered it (it offers no event stream).
func (h *Handler) Register(r gin.IRouter) {
	r.POST("/api/mcp", h.serve)
	r.GET("/api/mcp", func(c *gin.Context) { c.Status(http.StatusMethodNotAllowed) })
	r.GET("/.well-known/oauth-protected-resource", h.protectedResource)
	r.GET("/.well-known/oauth-protected-resource/*resource", h.protectedResource)
}

// serve is forma2's POST /api/mcp: the body limit first, then authentication, then the MCP exchange with the caller on the context.
func (h *Handler) serve(c *gin.Context) {
	tooLarge := v3api.PayloadTooLarge(fmt.Sprintf("Request body must not exceed %d bytes", v3api.MaxBodyBytes))
	if c.Request.ContentLength > v3api.MaxBodyBytes {
		v3api.WriteProblem(c, tooLarge)
		return
	}
	// A body without a length is counted as it is read.
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, v3api.MaxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			v3api.WriteProblem(c, tooLarge)
			return
		}
		h.internalError(c, "Read MCP request body", err)
		return
	}
	p, ok := h.authenticate(c)
	if !ok {
		return
	}
	// A tool whose scope came after the token was granted is refused here, so the client can authorize again (stepUpScopes).
	if required, request := stepUpScopes(p, body); required != nil {
		h.log.WarnContext(c.Request.Context(), "MCP OAuth token missing a tool's MCP scope", "request_id", httpserver.RequestID(c), "status_code", http.StatusForbidden, "client_id", p.clientID)
		c.Header("WWW-Authenticate", h.insufficientScopeChallenge(request))
		v3api.WriteProblem(c, v3api.Forbidden("OAuth token does not include the required MCP scope: "+strings.Join(required, " ")+". Reconnect the MCP client to grant it."))
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	p.requestID = httpserver.RequestID(c)
	ctx := withPrincipal(c.Request.Context(), p)
	h.transport.ServeHTTP(&responseHeaders{ResponseWriter: c.Writer, requestID: httpserver.RequestID(c)}, c.Request.WithContext(ctx))
}

// responseHeaders stamps every MCP response with the request id and private, no-store caching, forma2's withMcpResponseHeaders, over whatever the SDK set.
type responseHeaders struct {
	http.ResponseWriter
	requestID string
	written   bool
}

func (w *responseHeaders) stamp() {
	if w.written {
		return
	}
	w.written = true
	w.Header().Set("X-Request-Id", w.requestID)
	w.Header().Set("Cache-Control", v3api.CacheNoStore)
}

func (w *responseHeaders) WriteHeader(status int) {
	w.stamp()
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseHeaders) Write(b []byte) (int, error) {
	w.stamp()
	return w.ResponseWriter.Write(b)
}

func (w *responseHeaders) Flush() {
	w.stamp()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseHeaders) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// protectedResource is the OAuth protected resource metadata (RFC 9728) at the root and at /api/mcp; any other resource is not one.
func (h *Handler) protectedResource(c *gin.Context) {
	if resource := strings.Trim(c.Param("resource"), "/"); resource != "" && resource != "api/mcp" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	c.Header("Cache-Control", metadataCacheControl)
	c.JSON(http.StatusOK, gin.H{
		"resource":                 h.urls.Resource,
		"authorization_servers":    []string{h.urls.Issuer},
		"scopes_supported":         oauthserver.ProtectedResourceScopes,
		"bearer_methods_supported": []string{"header"},
	})
}
