package mgmtapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/v1api"
)

// rawJSON writes a body as forma2's bare Response.json did: no envelope, no Cache-Control.
func rawJSON(c *gin.Context, status int, body any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Data(status, "application/json", bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

// v1Me is forma2's GET /api/v1/management/me, a route outside the v1 wrapper: with an x-api-key header (Authorization is never read), the one workspace the key is scoped to, shaped as the environment the endpoint once described; without one, the signed-in user. Each path charges api:v1 to its caller.
func (h *Handler) v1Me(c *gin.Context) {
	raw := c.GetHeader(apikey.HeaderAPIKey)
	if raw != "" {
		h.v1MeKey(c, raw)
		return
	}
	h.sessions.Optional()(c)
	if c.IsAborted() {
		return
	}
	actor, ok := httpapi.ActorFrom(c.Request.Context())
	if !ok {
		v1api.NotAuthenticated(c)
		return
	}
	if !h.v1MeLimit(c, actor.UserID.String()) {
		return
	}
	user, err := h.repo.meUser(c.Request.Context(), actor.UserID)
	if err != nil {
		h.v1.Unexpected(c, err, mgmtv1.MessageUnexpected)
		return
	}
	if user == nil {
		rawJSON(c, http.StatusOK, nil)
		return
	}
	rawJSON(c, http.StatusOK, jsView(user))
}

// v1MeLimit charges api:v1 to identifier, answering 429 with the limiter's message when the window is spent.
func (h *Handler) v1MeLimit(c *gin.Context, identifier string) bool {
	err := h.limiter.Apply(c.Request.Context(), ratelimit.APIV1, identifier)
	if err == nil {
		return true
	}
	if e, ok := apperr.As(err); ok && e.Status == http.StatusTooManyRequests {
		v1api.TooManyRequests(c, e.Message)
		return false
	}
	h.v1.Unexpected(c, err, mgmtv1.MessageUnexpected)
	return false
}

// v1MeKey is the API key path: the key exactly as sent (forma2 did not trim this header), its usage recorded, api:v1 charged, then the key must hold exactly one grant, which this endpoint describes.
func (h *Handler) v1MeKey(c *gin.Context, raw string) {
	if strings.TrimSpace(raw) != raw {
		v1api.NotAuthenticated(c)
		return
	}
	header := http.Header{}
	header.Set(apikey.HeaderAPIKey, raw)
	p, err := h.keys.Authenticate(c.Request.Context(), header, apikey.Options{AllowOrganizationOnly: true})
	if err != nil {
		h.v1.Unexpected(c, err, mgmtv1.MessageUnexpected)
		return
	}
	if p == nil {
		v1api.NotAuthenticated(c)
		return
	}
	if !h.v1MeLimit(c, p.APIKeyID.String()) {
		return
	}
	permissions := []string{apikey.PermissionManage, apikey.PermissionRead, apikey.PermissionWrite}
	if len(p.WorkspacePermissions) != 1 || !slices.Contains(permissions, p.WorkspacePermissions[0].Permission) {
		v1api.BadRequest(c, "This endpoint only supports API keys that are scoped to a single workspace. Use GET /api/v2/me to inspect organization-level API keys or keys with access to multiple workspaces.", nil, false)
		return
	}
	id := p.WorkspacePermissions[0].WorkspaceID
	found, err := h.repo.workspaces(c.Request.Context(), []uuid.UUID{id})
	if err != nil {
		h.v1.Unexpected(c, err, mgmtv1.MessageUnexpected)
		return
	}
	w := found[id]
	envID := w.ID.String()
	if w.LegacyEnvironmentID != nil {
		envID = *w.LegacyEnvironmentID
	}
	type ref struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	rawJSON(c, http.StatusOK, jsView(struct {
		ID                string    `json:"id"`
		Type              string    `json:"type"`
		CreatedAt         time.Time `json:"createdAt"`
		UpdatedAt         time.Time `json:"updatedAt"`
		AppSetupCompleted bool      `json:"appSetupCompleted"`
		Workspace         ref       `json:"workspace"`
		Project           ref       `json:"project"`
	}{envID, "production", w.CreatedAt, w.UpdatedAt, w.AppSetupCompleted, ref{w.ID, w.Name}, ref{w.ID, w.Name}}))
}
