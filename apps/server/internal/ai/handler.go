package ai

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves the admin SPA's AI routes under /api/app: whether AI is available to an organization, a workspace's survey list and a survey's translations, the editor's AI translation, and a survey summary's example responses.
type Handler struct {
	svc      *Service
	examples *ExampleResponses
	sessions httpapi.SessionAuth
}

func NewHandler(svc *Service, examples *ExampleResponses, sessions httpapi.SessionAuth) *Handler {
	return &Handler{svc: svc, examples: examples, sessions: sessions}
}

// Register mounts the routes on the group that already applies the error envelope and the CSRF check.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.sessions.Require())
	g.GET("/organizations/:organizationId/ai", h.organization)
	g.GET("/workspaces/:workspaceId/ai", h.workspace)
	g.POST("/workspaces/:workspaceId/ai/translations", h.translate)
	g.GET("/surveys/:surveyId/ai/translation-availability", h.translationAvailability)
	g.POST("/surveys/:surveyId/ai/example-responses", h.generateExampleResponses)
}

func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}

func param(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := httpapi.ParamUUID(c, name)
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, false
	}
	return id, true
}

func respond[T any](c *gin.Context, out T, err error) {
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) organization(c *gin.Context) {
	id, ok := param(c, "organizationId")
	if !ok {
		return
	}
	out, err := h.svc.OrganizationAvailability(c.Request.Context(), userID(c), id)
	respond(c, out, err)
}

func (h *Handler) workspace(c *gin.Context) {
	id, ok := param(c, "workspaceId")
	if !ok {
		return
	}
	out, err := h.svc.WorkspaceAvailability(c.Request.Context(), userID(c), id)
	respond(c, out, err)
}

func (h *Handler) translationAvailability(c *gin.Context) {
	id, ok := param(c, "surveyId")
	if !ok {
		return
	}
	out, err := h.svc.SurveyTranslationAvailability(c.Request.Context(), userID(c), id)
	respond(c, out, err)
}

func (h *Handler) translate(c *gin.Context) {
	id, ok := param(c, "workspaceId")
	if !ok {
		return
	}
	var in TranslateSurveyFieldsInput
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.TranslateSurveyFields(c.Request.Context(), userID(c), id, in)
	respond(c, out, err)
}

func (h *Handler) generateExampleResponses(c *gin.Context) {
	id, ok := param(c, "surveyId")
	if !ok {
		return
	}
	out, err := h.examples.Generate(c.Request.Context(), userID(c), id)
	respond(c, out, err)
}
