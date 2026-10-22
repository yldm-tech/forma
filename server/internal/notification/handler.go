package notification

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// Handler answers the unsubscribe links of the response-finished email, outside /api/app: they are browser navigations from an email.
type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Register mounts the link's path ahead of the SPA, which owns every other page.
func (h *Handler) Register(r gin.IRouter) {
	r.GET("/workspaces/:workspaceId/settings/notifications", h.unsubscribe)
}

// unsubscribe redirects to the account notification settings with the switch the link names, which the page turns off once the user is signed in.
func (h *Handler) unsubscribe(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, UnsubscribeTarget(c.Query("type"), c.Query("elementId")))
}

// resolveElementID is the id an unsubscribe link names: a UUID as it is, a forma2 id as the id migrated from it.
func resolveElementID(raw string) (string, bool) {
	id, ok := legacyid.Resolve(raw)
	if !ok {
		return "", false
	}
	return id.String(), true
}
