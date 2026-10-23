package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// Handler serves the response views' routes under /api/app: listing, counting and reading a survey's responses, deleting one, and tagging. The client API's routes belong to the client API module, which drives Service's collection steps.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth) *Handler {
	return &Handler{svc: svc, sessions: sessions}
}

// Register mounts the routes on the /api/app group, which applies the error envelope and the CSRF check.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.sessions.Require())
	g.POST("/surveys/:surveyId/responses/search", h.list)
	g.POST("/surveys/:surveyId/responses/count", h.count)
	g.GET("/responses/:responseId", h.get)
	g.DELETE("/responses/:responseId", h.delete)
	g.POST("/responses/:responseId/tags", h.addTag)
	g.DELETE("/responses/:responseId/tags/:tagId", h.removeTag)
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

// listRequest is forma2's ZGetResponsesAction without the survey id. forma2 took any limit; a page here is at most 500 responses (the response table asks for 25 at a time).
type listRequest struct {
	Limit          *int      `json:"limit" binding:"omitempty,min=0,max=500"`
	Offset         *int      `json:"offset" binding:"omitempty,min=0"`
	FilterCriteria *Criteria `json:"filterCriteria"`
	Cursor         *Cursor   `json:"cursor"`
}

func (h *Handler) list(c *gin.Context) {
	surveyID, ok := param(c, "surveyId")
	if !ok {
		return
	}
	var in listRequest
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	responses, err := h.svc.List(c.Request.Context(), userID(c), surveyID, ListInput{Limit: in.Limit, Offset: in.Offset, Filter: in.FilterCriteria, Cursor: in.Cursor})
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, responses)
}

type countRequest struct {
	FilterCriteria *Criteria `json:"filterCriteria"`
}

type countResponse struct {
	Count int64 `json:"count"`
}

func (h *Handler) count(c *gin.Context) {
	surveyID, ok := param(c, "surveyId")
	if !ok {
		return
	}
	var in countRequest
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	n, err := h.svc.Count(c.Request.Context(), userID(c), surveyID, in.FilterCriteria)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, countResponse{Count: n})
}

func (h *Handler) get(c *gin.Context) {
	id, ok := param(c, "responseId")
	if !ok {
		return
	}
	r, err := h.svc.Get(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, r)
}

type deleteQuery struct {
	DecrementQuotas bool `form:"decrementQuotas"`
}

func (h *Handler) delete(c *gin.Context) {
	id, ok := param(c, "responseId")
	if !ok {
		return
	}
	var in deleteQuery
	if err := httpapi.BindQuery(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	r, err := h.svc.Delete(c.Request.Context(), userID(c), id, in.DecrementQuotas)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, r)
}

type addTagRequest struct {
	TagID uuid.UUID `json:"tagId" binding:"required"`
}

func (h *Handler) addTag(c *gin.Context) {
	id, ok := param(c, "responseId")
	if !ok {
		return
	}
	var in addTagRequest
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	change, err := h.svc.AddTag(c.Request.Context(), userID(c), id, in.TagID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, change)
}

func (h *Handler) removeTag(c *gin.Context) {
	id, ok := param(c, "responseId")
	if !ok {
		return
	}
	tagID, ok := param(c, "tagId")
	if !ok {
		return
	}
	change, err := h.svc.RemoveTag(c.Request.Context(), userID(c), id, tagID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, change)
}
