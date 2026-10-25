package webhook

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves the webhooks page's routes under /api/app: the list, the endpoint test, create, edit and delete, a webhook's delivery log and resend, and a new signing secret.
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
	g.GET("/workspaces/:workspaceId/webhooks", h.list)
	g.POST("/workspaces/:workspaceId/webhooks", h.create)
	g.POST("/workspaces/:workspaceId/webhooks/test", h.test)
	g.PATCH("/webhooks/:webhookId", h.update)
	g.DELETE("/webhooks/:webhookId", h.delete)
	g.GET("/webhooks/:webhookId/deliveries", h.deliveries)
	g.POST("/webhooks/:webhookId/deliveries/:deliveryId/resend", h.resend)
	g.POST("/webhooks/:webhookId/secret", h.regenerateSecret)
}

// ListResponse is the webhooks page's data: the workspace's webhooks, and whether DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS lets the URL field accept http and internal hosts, which forma2's page passed to its client-side URL check.
type ListResponse struct {
	Webhooks          []View `json:"webhooks"`
	AllowInternalURLs bool   `json:"allowInternalUrls"`
}

func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}

func (h *Handler) list(c *gin.Context) {
	ws, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.List(c.Request.Context(), userID(c), ws)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, ListResponse{Webhooks: out, AllowInternalURLs: h.svc.guard.AllowsInternal()})
}

func (h *Handler) create(c *gin.Context) {
	ws, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var in CreateInput
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.Create(c.Request.Context(), userID(c), ws, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) test(c *gin.Context) {
	ws, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var in TestInput
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.TestEndpoint(c.Request.Context(), userID(c), ws, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) update(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "webhookId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var in Input
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.Update(c.Request.Context(), userID(c), id, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) delete(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "webhookId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), userID(c), id); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// DeliveriesResponse is a webhook's delivery log, newest first.
type DeliveriesResponse struct {
	Deliveries []DeliveryView `json:"deliveries"`
}

func (h *Handler) deliveries(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "webhookId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.Deliveries(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, DeliveriesResponse{Deliveries: out})
}

func (h *Handler) resend(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "webhookId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	deliveryID, err := httpapi.ParamUUID(c, "deliveryId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.Resend(c.Request.Context(), userID(c), id, deliveryID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) regenerateSecret(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "webhookId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	out, err := h.svc.RegenerateSecret(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
