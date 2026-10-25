package sdkidentity

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves the app connection page's SDK identity routes under /api/app. Every route needs a session.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth) *Handler {
	return &Handler{svc: svc, sessions: sessions}
}

// Register mounts the routes on the /api/app group, which already applies the error envelope and the CSRF check to every mutation.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.sessions.Require())
	g.GET("/workspaces/:workspaceId/sdk-identity", h.get)
	g.PATCH("/workspaces/:workspaceId/sdk-identity", h.update)
	g.POST("/workspaces/:workspaceId/sdk-identity/secret", h.rotateSecret)
	g.DELETE("/workspaces/:workspaceId/sdk-identity/secret", h.removeSecret)
}

// params reads the caller and the workspace of a route; ok is false when the request was answered.
func params(c *gin.Context) (userID, workspaceID uuid.UUID, ok bool) {
	workspaceID, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, uuid.Nil, false
	}
	actor, _ := httpapi.ActorFrom(c.Request.Context())
	return actor.UserID, workspaceID, true
}

func (h *Handler) get(c *gin.Context) {
	userID, workspaceID, ok := params(c)
	if !ok {
		return
	}
	v, err := h.svc.Get(c.Request.Context(), userID, workspaceID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// updateRequest is a settings change; a field left out is left as it is.
type updateRequest struct {
	Required *bool `json:"required"`
	// ProtectedAttributeKeys is raw so that an explicit list, an empty one included, is told from an absent field.
	ProtectedAttributeKeys json.RawMessage `json:"protectedAttributeKeys"`
}

func (h *Handler) update(c *gin.Context) {
	userID, workspaceID, ok := params(c)
	if !ok {
		return
	}
	var req updateRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	in := Update{Required: req.Required}
	if len(req.ProtectedAttributeKeys) > 0 {
		if err := json.Unmarshal(req.ProtectedAttributeKeys, &in.ProtectedAttributeKeys); err != nil || in.ProtectedAttributeKeys == nil {
			httpapi.Fail(c, invalidKeys())
			return
		}
		in.SetProtectedAttributeKeys = true
	}
	v, err := h.svc.Update(c.Request.Context(), userID, workspaceID, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) rotateSecret(c *gin.Context) {
	userID, workspaceID, ok := params(c)
	if !ok {
		return
	}
	v, err := h.svc.RotateSecret(c.Request.Context(), userID, workspaceID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	// The answer carries the secret in clear, which must not be cached anywhere on its way to the browser.
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, v)
}

func (h *Handler) removeSecret(c *gin.Context) {
	userID, workspaceID, ok := params(c)
	if !ok {
		return
	}
	v, err := h.svc.RemoveSecret(c.Request.Context(), userID, workspaceID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// invalidKeys answers a protectedAttributeKeys that is not a list of strings.
func invalidKeys() error {
	return apperr.Validation([]apperr.FieldError{{Field: "protectedAttributeKeys", Rule: "array"}})
}
