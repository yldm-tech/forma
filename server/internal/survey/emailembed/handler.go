package emailembed

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// Handler serves the share modal's Email tab under /api/app. Every route needs a session.
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
	g.GET("/surveys/:surveyId/email-html", h.html)
	g.POST("/surveys/:surveyId/email-preview", h.preview)
}

func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}

func (h *Handler) html(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "surveyId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.HTML(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"html": out})
}

func (h *Handler) preview(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "surveyId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	sent, err := h.svc.SendPreview(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": sent})
}
