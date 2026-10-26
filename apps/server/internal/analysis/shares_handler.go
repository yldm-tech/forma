package analysis

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/response"
)

// createResultShareRequest is a new results link. No filter shows every response.
type createResultShareRequest struct {
	Name               string             `json:"name"`
	FilterCriteria     *response.Criteria `json:"filterCriteria"`
	RevealedElementIDs []string           `json:"revealedElementIds" binding:"max=500"`
	ExpiresAt          *time.Time         `json:"expiresAt"`
}

// updateResultShareRequest replaces what may change of a link: its name, the cards it reveals and its expiry (null for never).
type updateResultShareRequest struct {
	Name               string     `json:"name"`
	RevealedElementIDs []string   `json:"revealedElementIds" binding:"required,max=500"`
	ExpiresAt          *time.Time `json:"expiresAt"`
}

// sharedResultsRequest names a link by its key.
type sharedResultsRequest struct {
	Token string `json:"token" binding:"required,max=100"`
}

// sharedTrendsRequest is a link's trend at the interval asked for; no interval lets the server pick one from the range.
type sharedTrendsRequest struct {
	Token    string `json:"token" binding:"required,max=100"`
	Interval string `json:"interval" binding:"omitempty,oneof=day week month"`
}

func shareParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := httpapi.ParamUUID(c, "shareId")
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) resultShares(c *gin.Context) {
	surveyID, ok := surveyParam(c)
	if !ok {
		return
	}
	out, err := h.svc.ResultShares(c.Request.Context(), userID(c), surveyID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, out)
}

func (h *Handler) createResultShare(c *gin.Context) {
	surveyID, ok := surveyParam(c)
	if !ok {
		return
	}
	var in createResultShareRequest
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.CreateResultShare(c.Request.Context(), userID(c), surveyID, ResultShareInput(in))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) updateResultShare(c *gin.Context) {
	surveyID, ok := surveyParam(c)
	if !ok {
		return
	}
	shareID, ok := shareParam(c)
	if !ok {
		return
	}
	var in updateResultShareRequest
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.UpdateResultShare(c.Request.Context(), userID(c), surveyID, shareID, ResultShareUpdate(in))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, out)
}

func (h *Handler) deleteResultShare(c *gin.Context) {
	surveyID, ok := surveyParam(c)
	if !ok {
		return
	}
	shareID, ok := shareParam(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteResultShare(c.Request.Context(), userID(c), surveyID, shareID); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// sharedSummary answers the summary a results link shows. It is never cached: a revoked link must stop answering at once.
func (h *Handler) sharedSummary(c *gin.Context) {
	var in sharedResultsRequest
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.SharedResults(c.Request.Context(), in.Token)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

func (h *Handler) sharedTrends(c *gin.Context) {
	var in sharedTrendsRequest
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.SharedTrends(c.Request.Context(), in.Token, in.Interval)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}
