package contactsurvey

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves the routes of /api/app. Every route needs a session.
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
	g.GET("/contacts/:contactId/activity", h.activity)
	g.POST("/contacts/:contactId/personal-link", h.personalLink)
	g.GET("/workspaces/:workspaceId/published-link-surveys", h.publishedLinkSurveys)
	g.POST("/surveys/:surveyId/personal-links", h.segmentLinks)
}

func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}

func (h *Handler) activity(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "contactId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	a, err := h.svc.Activity(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, a)
}

func (h *Handler) publishedLinkSurveys(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	surveys, err := h.svc.PublishedLinkSurveys(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, surveys)
}

// personalLinkRequest is forma2's ZGeneratePersonalSurveyLinkAction without the contact, which the path names.
type personalLinkRequest struct {
	SurveyID       uuid.UUID `json:"surveyId" binding:"required"`
	ExpirationDays *float64  `json:"expirationDays"`
}

func (h *Handler) personalLink(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "contactId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var req personalLinkRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	link, err := h.svc.PersonalLink(c.Request.Context(), userID(c), id, req.SurveyID, req.ExpirationDays)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"surveyUrl": link})
}

// segmentLinksRequest is forma2's ZGeneratePersonalLinksAction without the survey, which the path names.
type segmentLinksRequest struct {
	SegmentID      uuid.UUID `json:"segmentId" binding:"required"`
	ExpirationDays *float64  `json:"expirationDays"`
}

func (h *Handler) segmentLinks(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "surveyId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var req segmentLinksRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	file, err := h.svc.SegmentLinks(c.Request.Context(), userID(c), id, req.SegmentID, req.ExpirationDays)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, file)
}
