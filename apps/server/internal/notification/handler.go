package notification

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Handler answers the unsubscribe link of the response-finished email, which needs the server: it is a browser navigation from an email, under the app's base path ahead of the SPA, which owns every other page there.
type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Register mounts the link's path.
func (h *Handler) Register(r gin.IRouter) {
	r.GET(config.AppBasePath+"/workspaces/:workspaceId/settings/notifications", h.unsubscribe)
}

// unsubscribe redirects to the account notification settings with the switch the link names, which the page turns off once the user is signed in.
func (h *Handler) unsubscribe(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, UnsubscribeTarget(c.Query("type"), c.Query("elementId")))
}
