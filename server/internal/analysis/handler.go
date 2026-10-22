package analysis

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/response"
)

// Handler serves the analysis views' routes under /api/app.
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
	g.POST("/surveys/:surveyId/summary", h.summary)
	g.GET("/surveys/:surveyId/filter-data", h.filterData)
	g.GET("/surveys/:surveyId/displays", h.displays)
	g.POST("/surveys/:surveyId/responses/export", h.export)
	g.POST("/surveys/:surveyId/reset", h.reset)
}

func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}

func surveyParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := httpapi.ParamUUID(c, "surveyId")
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, false
	}
	return id, true
}

// summaryRequest is forma2's ZGetSurveySummaryAction without the survey id.
type summaryRequest struct {
	FilterCriteria *response.Criteria `json:"filterCriteria"`
}

func (h *Handler) summary(c *gin.Context) {
	surveyID, ok := surveyParam(c)
	if !ok {
		return
	}
	var in summaryRequest
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.Summary(c.Request.Context(), userID(c), surveyID, in.FilterCriteria)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) filterData(c *gin.Context) {
	surveyID, ok := surveyParam(c)
	if !ok {
		return
	}
	out, err := h.svc.FilterData(c.Request.Context(), userID(c), surveyID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// displaysQuery is forma2's ZGetDisplaysWithContactAction without the survey id.
type displaysQuery struct {
	Limit  *int `form:"limit" binding:"required,min=1,max=100"`
	Offset *int `form:"offset" binding:"required,min=0"`
}

func (h *Handler) displays(c *gin.Context) {
	surveyID, ok := surveyParam(c)
	if !ok {
		return
	}
	var in displaysQuery
	if err := httpapi.BindQuery(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.DisplaysWithContact(c.Request.Context(), userID(c), surveyID, *in.Limit, *in.Offset)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// exportRequest is forma2's ZGetResponsesDownloadUrlAction without the survey id: "All" sends empty criteria, "Filtered" the current ones.
type exportRequest struct {
	Format         string             `json:"format" binding:"required,oneof=csv xlsx"`
	FilterCriteria *response.Criteria `json:"filterCriteria" binding:"required"`
}

// export answers the file itself, named by Content-Disposition. It is built in a temporary file first, so a failure halfway is still an error envelope rather than a truncated download.
func (h *Handler) export(c *gin.Context) {
	surveyID, ok := surveyParam(c)
	if !ok {
		return
	}
	var in exportRequest
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	file, err := os.CreateTemp("", "forma-export-*")
	if err != nil {
		httpapi.Fail(c, fmt.Errorf("create export file: %w", err))
		return
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	name, err := h.svc.Export(c.Request.Context(), userID(c), surveyID, in.Format, in.FilterCriteria, file)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	size, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		httpapi.Fail(c, fmt.Errorf("measure export file: %w", err))
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		httpapi.Fail(c, fmt.Errorf("rewind export file: %w", err))
		return
	}
	contentType := ContentTypeCSV
	if in.Format == FormatXLSX {
		contentType = ContentTypeXLSX
	}
	c.Header("Cache-Control", "private, no-store")
	c.DataFromReader(http.StatusOK, size, contentType, file, map[string]string{"Content-Disposition": `attachment; filename="` + name + `"`})
}

func (h *Handler) reset(c *gin.Context) {
	surveyID, ok := surveyParam(c)
	if !ok {
		return
	}
	out, err := h.svc.Reset(c.Request.Context(), userID(c), surveyID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
