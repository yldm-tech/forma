package integration

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v1api"
)

// messageUnexpected is what forma2's v1 wrapper answered when a handler threw.
const messageUnexpected = "An unexpected error occurred."

// OAuthHandler serves forma2's integration routes outside /api/app: the authorize routes the integrations pages called with the workspace in a workspaceId header, the callbacks registered with the providers' apps, and the Airtable tables lookup. They authenticate by session only and answer with forma2's v1 envelope; an authorization failure is 401 unauthorized, as forma2's was.
type OAuthHandler struct {
	svc      *Service
	sessions httpapi.SessionAuth
	limiter  *ratelimit.Limiter
	log      *slog.Logger
}

func NewOAuthHandler(svc *Service, sessions httpapi.SessionAuth, limiter *ratelimit.Limiter, log *slog.Logger) *OAuthHandler {
	return &OAuthHandler{svc: svc, sessions: sessions, limiter: limiter, log: log}
}

// Register mounts the routes. Google's sat outside forma2's v1 wrapper, so they are not rate limited; the /api/v1/integrations ones charge api:v1 to the user, as the wrapper did.
func (h *OAuthHandler) Register(r gin.IRouter) {
	google := r.Group("/api/google-sheet", h.sessions.Optional())
	google.GET("", h.googleAuthorize)
	google.GET("/callback", h.callback(TypeGoogleSheets))

	v1 := r.Group("/api/v1/integrations", h.sessions.Optional(), h.v1Session)
	v1.GET("/airtable", h.authorize(TypeAirtable))
	v1.GET("/airtable/callback", h.callback(TypeAirtable))
	v1.GET("/airtable/tables", h.airtableTables)
	v1.GET("/notion", h.authorize(TypeNotion))
	v1.GET("/notion/callback", h.callback(TypeNotion))
	v1.GET("/slack", h.authorize(TypeSlack))
	v1.GET("/slack/callback", h.callback(TypeSlack))
}

// v1Session is the front of forma2's v1 wrapper for an integration route: a session (an API key is never accepted), then api:v1 per user.
func (h *OAuthHandler) v1Session(c *gin.Context) {
	a, ok := httpapi.ActorFrom(c.Request.Context())
	if !ok {
		v1api.NotAuthenticated(c)
		return
	}
	if err := h.limiter.Apply(c.Request.Context(), ratelimit.APIV1, a.UserID.String()); err != nil {
		h.fail(c, err)
		return
	}
	c.Next()
}

// fail answers an error as forma2's routes did: its expected failures with their v1 responses, anything else logged and answered with a generic 500.
func (h *OAuthHandler) fail(c *gin.Context, err error) {
	e, ok := apperr.As(err)
	if !ok {
		h.log.ErrorContext(c.Request.Context(), "integration route failed", "route", c.FullPath(), "err", err)
		v1api.InternalError(c, messageUnexpected)
		return
	}
	switch e.Status {
	case http.StatusBadRequest:
		v1api.BadRequest(c, e.Message, nil, false)
	case http.StatusUnauthorized:
		v1api.NotAuthenticated(c)
	case http.StatusForbidden:
		v1api.Unauthorized(c)
	case http.StatusNotFound:
		v1api.Error(c, http.StatusNotFound, "not_found", e.Message, nil, false)
	case http.StatusTooManyRequests:
		v1api.TooManyRequests(c, apperr.TooManyRequestsMessage)
	case http.StatusInternalServerError:
		v1api.InternalError(c, e.Message)
	default:
		h.log.ErrorContext(c.Request.Context(), "integration route failed", "route", c.FullPath(), "err", err)
		v1api.InternalError(c, messageUnexpected)
	}
}

// headerWorkspace reads the workspaceId header the pages sent, a UUID. One that names nothing cannot be authorized: 401.
func (h *OAuthHandler) headerWorkspace(c *gin.Context) (uuid.UUID, bool) {
	raw := c.GetHeader("workspaceId")
	if raw == "" {
		v1api.BadRequest(c, "workspaceId is missing", nil, false)
		return uuid.Nil, false
	}
	id, ok := rowid.Parse(raw)
	if !ok {
		v1api.Unauthorized(c)
		return uuid.Nil, false
	}
	return id, true
}

// googleAuthorize is GET /api/google-sheet, which read the header before the session.
func (h *OAuthHandler) googleAuthorize(c *gin.Context) {
	if c.GetHeader("workspaceId") == "" {
		v1api.BadRequest(c, "workspaceId is missing", nil, false)
		return
	}
	a, ok := httpapi.ActorFrom(c.Request.Context())
	if !ok {
		v1api.NotAuthenticated(c)
		return
	}
	ws, ok := h.headerWorkspace(c)
	if !ok {
		return
	}
	h.writeAuthorize(c, a, ws, TypeGoogleSheets)
}

func (h *OAuthHandler) authorize(typ string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ws, ok := h.headerWorkspace(c)
		if !ok {
			return
		}
		h.writeAuthorize(c, actor(c), ws, typ)
	}
}

func (h *OAuthHandler) writeAuthorize(c *gin.Context, a httpapi.Actor, ws uuid.UUID, typ string) {
	u, err := h.svc.Authorize(c.Request.Context(), a, ws, typ)
	if err != nil {
		h.fail(c, err)
		return
	}
	v1api.Success(c, AuthorizeResponse{AuthURL: u})
}

// callback completes the flow and sends the browser back to the integration's page with a 302.
func (h *OAuthHandler) callback(typ string) gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := httpapi.ActorFrom(c.Request.Context())
		if !ok {
			v1api.NotAuthenticated(c)
			return
		}
		to, err := h.svc.Callback(c.Request.Context(), a, typ, c.Request.URL.Query())
		if err != nil {
			h.fail(c, err)
			return
		}
		c.Header("Cache-Control", v1api.CacheNoStore)
		c.Redirect(http.StatusFound, to)
	}
}

// TablesResponse is forma2's Airtable tables answer.
type TablesResponse struct {
	Tables []Item `json:"tables"`
}

// airtableTables is GET /api/v1/integrations/airtable/tables?baseId=, which checked the base id before the workspace header.
func (h *OAuthHandler) airtableTables(c *gin.Context) {
	baseID, present := c.GetQuery("baseId")
	if !present {
		v1api.BadRequest(c, "Base Id is Required", nil, false)
		return
	}
	ws, ok := h.headerWorkspace(c)
	if !ok {
		return
	}
	tables, err := h.svc.AirtableTables(c.Request.Context(), actor(c).UserID, ws, baseID)
	if e, isApp := apperr.As(err); isApp && e.Status == http.StatusNotFound {
		v1api.Error(c, http.StatusNotFound, "not_found", "Integration not found not found", map[string]any{"resource_id": nil, "resource_type": "Integration not found"}, false)
		return
	}
	if err != nil {
		h.fail(c, err)
		return
	}
	v1api.Success(c, TablesResponse{Tables: tables})
}
