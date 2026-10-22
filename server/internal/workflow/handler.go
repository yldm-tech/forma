package workflow

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/v3api"
)

// Handler serves /api/v3/workflows, the management API the workflow builder and API clients call by session or API key, and the builder's authoring context under /api/app.
type Handler struct {
	svc      *Service
	guard    *v3api.Guard
	sessions httpapi.SessionAuth
	audit    *audit.Emitter
	cfg      config.Config
	log      *slog.Logger
}

func NewHandler(cfg config.Config, svc *Service, guard *v3api.Guard, sessions httpapi.SessionAuth, emitter *audit.Emitter, log *slog.Logger) *Handler {
	return &Handler{svc: svc, guard: guard, sessions: sessions, audit: emitter, cfg: cfg, log: log}
}

// RegisterV3 mounts /api/v3/workflows on the engine.
func (h *Handler) RegisterV3(r gin.IRouter) {
	g := r.Group("/api/v3/workflows", h.guard.Handlers(v3api.SessionOrAPIKey)...)
	g.GET("", h.list)
	g.POST("", h.create)
	g.GET("/runs", h.listRuns)
	g.GET("/runs/:runId", h.getRun)
	g.GET("/:workflowId", h.get)
	g.PATCH("/:workflowId", h.patch)
	g.DELETE("/:workflowId", h.delete)
	g.POST("/:workflowId/duplicate", h.duplicate)
	g.POST("/:workflowId/enable", h.lifecycle(h.svc.Enable))
	g.POST("/:workflowId/disable", h.lifecycle(h.svc.Disable))
	g.POST("/:workflowId/archive", h.lifecycle(h.svc.Archive))
	g.POST("/:workflowId/unarchive", h.lifecycle(h.svc.Unarchive))
	g.POST("/:workflowId/test", h.test)
}

// Register mounts the builder's authoring context on the /api/app group.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.sessions.Require())
	g.GET("/workspaces/:workspaceId/workflows/authoring-context", h.authoringContext)
}

// listMeta is the meta of a cursor-paginated list.
type listMeta struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"nextCursor"`
}

func actorOf(c *gin.Context) authz.Actor {
	a, _ := v3api.ActorFrom(c.Request.Context())
	return a
}

// fail answers with the problem err stands for. A stray unique violation is a retryable 409, as forma2's mapper made it.
func (h *Handler) fail(c *gin.Context, err error) {
	if isUniqueViolation(err) {
		err = conflict(conflictingWriteRetry)
	}
	v3api.Fail(c, h.log, err)
}

// record is forma2's v3 audit for a workflow mutation: the caller, the call's absolute URL, and on success the target with redacted before and after snapshots and the workspace's organization; a failure carries the request id instead.
func (h *Handler) record(c *gin.Context, action audit.Action, err error, before, after *Workflow) {
	ctx := c.Request.Context()
	e := audit.Event{Action: action, TargetType: audit.TargetWorkflow, Status: audit.StatusSuccess, APIURL: h.apiURL(c)}
	actor := actorOf(c)
	e.ActorType, e.ActorID = audit.ActorUser, actor.ID.String()
	if actor.Type == authz.ActorAPIKey {
		e.ActorType = audit.ActorAPI
	}
	key := string(h.cfg.EncryptionKeyText)
	switch {
	case err != nil:
		e.Status, e.EventID = audit.StatusFailure, httpserver.RequestID(c)
	default:
		target := after
		if target == nil {
			target = before
		}
		e.TargetID = target.ID.String()
		e.OrganizationID = h.svc.organizationOf(ctx, target.WorkspaceID)
		if before != nil {
			e.OldObject = auditSnapshot(before, key)
		}
		if after != nil {
			e.NewObject = auditSnapshot(after, key)
		}
	}
	h.audit.Log(ctx, audit.FromRequest(ctx, e))
}

func (h *Handler) apiURL(c *gin.Context) string {
	if h.cfg.WebAppURL == nil {
		return ""
	}
	return h.cfg.WebAppURL.Scheme + "://" + h.cfg.WebAppURL.Host + c.Request.URL.RequestURI()
}

func (h *Handler) list(c *gin.Context) {
	q, cursor, err := parseListQuery(c.Request.URL.Query())
	if err != nil {
		h.fail(c, err)
		return
	}
	rows, next, err := h.svc.List(c.Request.Context(), actorOf(c), q, cursor)
	if err != nil {
		h.fail(c, err)
		return
	}
	items := make([]ListItem, len(rows))
	for i := range rows {
		items[i] = toListItem(&rows[i])
	}
	v3api.List(c, items, listMeta{Limit: q.Limit, NextCursor: next})
}

func (h *Handler) create(c *gin.Context) {
	w, err := h.svc.Create(c.Request.Context(), actorOf(c), func() (any, error) { return readBody(c, false) })
	h.record(c, audit.ActionCreated, err, nil, w)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.Created(c, "/api/v3/workflows/"+w.ID.String(), toResource(w))
}

func (h *Handler) get(c *gin.Context) {
	id, err := paramID(c, "workflowId")
	if err != nil {
		h.fail(c, err)
		return
	}
	w, err := h.svc.Get(c.Request.Context(), actorOf(c), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.OK(c, toResource(w))
}

func (h *Handler) patch(c *gin.Context) {
	id, err := paramID(c, "workflowId")
	if err != nil {
		h.fail(c, err)
		return
	}
	before, after, err := h.svc.Patch(c.Request.Context(), actorOf(c), id, func() (any, error) { return readBody(c, false) })
	h.record(c, audit.ActionUpdated, err, before, after)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.OK(c, toResource(after))
}

func (h *Handler) duplicate(c *gin.Context) {
	id, err := paramID(c, "workflowId")
	if err != nil {
		h.fail(c, err)
		return
	}
	_, created, err := h.svc.Duplicate(c.Request.Context(), actorOf(c), id, func() (any, error) { return readBody(c, true) })
	h.record(c, audit.ActionCreated, err, nil, created)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.Created(c, "/api/v3/workflows/"+created.ID.String(), toResource(created))
}

func (h *Handler) delete(c *gin.Context) {
	id, err := paramID(c, "workflowId")
	if err != nil {
		h.fail(c, err)
		return
	}
	w, err := h.svc.Delete(c.Request.Context(), actorOf(c), id)
	h.record(c, audit.ActionDeleted, err, w, nil)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.NoContent(c)
}

// lifecycle serves enable, disable, archive and unarchive, each audited as an update.
func (h *Handler) lifecycle(op func(context.Context, authz.Actor, uuid.UUID) (*Workflow, *Workflow, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := paramID(c, "workflowId")
		if err != nil {
			h.fail(c, err)
			return
		}
		before, after, err := op(c.Request.Context(), actorOf(c), id)
		h.record(c, audit.ActionUpdated, err, before, after)
		if err != nil {
			h.fail(c, err)
			return
		}
		v3api.OK(c, toResource(after))
	}
}

func (h *Handler) test(c *gin.Context) {
	id, err := paramID(c, "workflowId")
	if err != nil {
		h.fail(c, err)
		return
	}
	res, err := h.svc.Test(c.Request.Context(), actorOf(c), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.OK(c, res)
}

func (h *Handler) listRuns(c *gin.Context) {
	q, cursor, err := parseRunListQuery(c.Request.URL.Query())
	if err != nil {
		h.fail(c, err)
		return
	}
	rows, next, err := h.svc.ListRuns(c.Request.Context(), actorOf(c), q, cursor)
	if err != nil {
		h.fail(c, err)
		return
	}
	items := make([]RunListItem, len(rows))
	for i := range rows {
		items[i] = toRunListItem(&rows[i])
	}
	v3api.List(c, items, listMeta{Limit: q.Limit, NextCursor: next})
}

func (h *Handler) getRun(c *gin.Context) {
	id, err := paramID(c, "runId")
	if err != nil {
		h.fail(c, err)
		return
	}
	r, err := h.svc.GetRun(c.Request.Context(), actorOf(c), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	v3api.OK(c, toRunResource(r))
}

func (h *Handler) authoringContext(c *gin.Context) {
	ws, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	a, _ := httpapi.ActorFrom(c.Request.Context())
	members, err := h.svc.Members(c.Request.Context(), a.UserID, ws)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, AuthoringContext{TeamMemberDetails: members, MailFrom: h.cfg.MailFrom})
}
