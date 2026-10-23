// Package instance answers what the admin SPA asks about the running instance rather than about a tenant: whether file storage is configured (forma2's IS_STORAGE_CONFIGURED), the release it runs, and the latest stable Forma release for the self-hosted update notice (forma2's getLatestStableFbRelease).
package instance

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/buildinfo"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/storage"
)

// Handler serves /api/app/instance to signed-in users.
type Handler struct {
	bucket   *storage.Bucket
	releases *Releases
	sessions httpapi.SessionAuth
}

func NewHandler(bucket *storage.Bucket, releases *Releases, sessions httpapi.SessionAuth) *Handler {
	return &Handler{bucket: bucket, releases: releases, sessions: sessions}
}

// Register mounts the routes on the /api/app group.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/instance", h.sessions.Require())
	g.GET("", h.get)
	g.GET("/latest-release", h.latestRelease)
}

// Info is what the SPA reads about the instance.
type Info struct {
	// StorageConfigured is forma2's IS_STORAGE_CONFIGURED: uploads work only when it is true.
	StorageConfigured bool `json:"storageConfigured"`
	// Version is the release the binary was built as; a development build reports 0.0.0.
	Version string `json:"version"`
}

func (h *Handler) get(c *gin.Context) {
	c.JSON(http.StatusOK, Info{StorageConfigured: h.bucket.Configured(), Version: buildinfo.Version})
}

// LatestRelease is the tag of the latest stable release, null when it is unknown.
type LatestRelease struct {
	TagName *string `json:"tagName"`
}

func (h *Handler) latestRelease(c *gin.Context) {
	c.JSON(http.StatusOK, LatestRelease{TagName: h.releases.Latest(c.Request.Context())})
}
