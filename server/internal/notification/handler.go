package notification

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/webui"
)

// Handler answers the links of the response-finished email that need the server, outside /api/app: they are browser navigations from an email.
type Handler struct {
	web *webui.Handler
}

func NewHandler(web *webui.Handler) *Handler { return &Handler{web: web} }

// Register mounts the links' paths ahead of the SPA, which owns every other page.
func (h *Handler) Register(r gin.IRouter) {
	r.GET("/workspaces/:workspaceId/settings/notifications", h.unsubscribe)
	r.GET("/workspaces/:workspaceId/surveys/:surveyId/responses", h.responses)
}

// unsubscribe redirects to the account notification settings with the switch the link names, which the page turns off once the user is signed in.
func (h *Handler) unsubscribe(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, UnsubscribeTarget(c.Query("type"), c.Query("elementId")))
}

// responses is the SPA's responses page. The view-responses CTA of an email sent before the migration names the workspace and survey by their forma2 ids, which the page cannot load; it redirects to the ids migrated from them, keeping the link's query.
func (h *Handler) responses(c *gin.Context) {
	ws, _ := legacyid.Resolve(c.Param("workspaceId"))
	survey, _ := legacyid.Resolve(c.Param("surveyId"))
	if ws.String() == c.Param("workspaceId") && survey.String() == c.Param("surveyId") {
		h.web.ServePage(c)
		return
	}
	target := "/workspaces/" + ws.String() + "/surveys/" + survey.String() + "/responses"
	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, target)
}

// resolveElementID is the id an unsubscribe link names: a UUID as it is, a forma2 id as the id migrated from it.
func resolveElementID(raw string) (string, bool) {
	id, ok := legacyid.Resolve(raw)
	if !ok {
		return "", false
	}
	return id.String(), true
}
