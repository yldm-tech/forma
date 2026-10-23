package unsplash

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves the editor's Unsplash routes under /api/app/unsplash.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth) *Handler {
	return &Handler{svc: svc, sessions: sessions}
}

// Register mounts the routes on the group that already applies the error envelope and the CSRF check.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/unsplash", h.sessions.Require())
	g.GET("/status", h.status)
	g.GET("/search", h.search)
	g.POST("/download", h.download)
}

// StatusOutput is whether the editor offers the Unsplash tab.
type StatusOutput struct {
	Configured bool `json:"configured"`
}

func (h *Handler) status(c *gin.Context) {
	c.JSON(http.StatusOK, StatusOutput{Configured: h.svc.Configured()})
}

// SearchInput is a search: forma2's searchQuery and page, the first page when page is absent.
type SearchInput struct {
	Query string `form:"query" binding:"required,max=200"`
	Page  *int   `form:"page" binding:"omitempty,min=1,max=1000"`
}

// SearchOutput is one page of results.
type SearchOutput struct {
	Images []Image `json:"images"`
}

func (h *Handler) search(c *gin.Context) {
	var in SearchInput
	if err := httpapi.BindQuery(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	page := 1
	if in.Page != nil {
		page = *in.Page
	}
	images, err := h.svc.Search(c.Request.Context(), userID(c), in.Query, page)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, SearchOutput{Images: images})
}

// DownloadInput is the chosen photo's download location, from its search result.
type DownloadInput struct {
	DownloadURL string `json:"downloadUrl" binding:"required,url"`
}

func (h *Handler) download(c *gin.Context) {
	var in DownloadInput
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	if err := h.svc.TriggerDownload(c.Request.Context(), userID(c), in.DownloadURL); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}
