package tag

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/v3api"
)

// Handler serves tag creation under /api/app and the tag endpoints of /api/v3, which accept a session only: they replaced server actions that only ever ran for a signed-in user.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
	v3       *v3api.Middleware
	auditor  *v3api.Auditor
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth, v3 *v3api.Middleware, auditor *v3api.Auditor) *Handler {
	return &Handler{svc: svc, sessions: sessions, v3: v3, auditor: auditor}
}

// Register mounts the /api/app routes.
func (h *Handler) Register(r gin.IRouter) {
	r.POST("/workspaces/:workspaceId/tags", h.sessions.Require(), h.create)
}

// RegisterV3 mounts the /api/v3 routes.
func (h *Handler) RegisterV3(r gin.IRouter) {
	g := r.Group("", h.v3.Auth(v3api.AuthSession))
	g.GET("/tags", h.listV3)
	g.PATCH("/tags/:tagId", h.renameV3)
	g.DELETE("/tags/:tagId", h.deleteV3)
	g.POST("/tags/:tagId/merge", h.mergeV3)
}

type createRequest struct {
	Name string `json:"name"`
}

func (h *Handler) create(c *gin.Context) {
	ws, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var req createRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	name, ok := NormalizeName(req.Name)
	if !ok {
		httpapi.Fail(c, apperr.Validation([]apperr.FieldError{{Field: "name", Rule: "len", Param: "1-255"}}))
		return
	}
	a, _ := httpapi.ActorFrom(c.Request.Context())
	t, err := h.svc.Create(c.Request.Context(), a.UserID, ws, name)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, t)
}

// v3Tag is forma2's serializeV3Tag, with timestamps as toISOString wrote them.
type v3Tag struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
	CreatedAt   string    `json:"createdAt"`
	UpdatedAt   string    `json:"updatedAt"`
	Count       int64     `json:"count"`
}

const isoMillis = "2006-01-02T15:04:05.000Z07:00"

func serialize(t Counted) v3Tag {
	return v3Tag{ID: t.ID, Name: t.Name, WorkspaceID: t.WorkspaceID, CreatedAt: t.CreatedAt.UTC().Format(isoMillis), UpdatedAt: t.UpdatedAt.UTC().Format(isoMillis), Count: t.Count}
}

func principal(c *gin.Context) *v3api.Principal {
	return v3api.PrincipalFrom(c.Request.Context())
}

func (h *Handler) listV3(c *gin.Context) {
	q := v3api.ReadQuery(c, "workspaceId")
	ws := q.UUID("workspaceId")
	if err := q.Err(); err != nil {
		v3api.Fail(c, err)
		return
	}
	tags, err := h.svc.ListV3(c.Request.Context(), principal(c), ws)
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	out := make([]v3Tag, len(tags))
	for i, t := range tags {
		out[i] = serialize(t)
	}
	v3api.OK(c, out)
}

// record emits the audit event of a v3 tag mutation, refused requests included.
func (h *Handler) record(c *gin.Context, action audit.Action, change Change, err error) {
	h.auditor.Record(c, audit.Event{Action: action, TargetType: audit.TargetTag, TargetID: change.TargetID, OrganizationID: change.OrganizationID, OldObject: change.OldObject, NewObject: change.NewObject}, err)
}

type renameRequest struct {
	Name *string `json:"name"`
}

// renameInput parses PATCH /api/v3/tags/{tagId}: ZV3RenameTagBody, then the route parameter.
func renameInput(c *gin.Context) (uuid.UUID, string, error) {
	var req renameRequest
	if err := v3api.ReadBody(c, &req); err != nil {
		return uuid.Nil, "", err
	}
	if req.Name == nil {
		return uuid.Nil, "", v3api.InvalidBody(v3api.RequiredString("name"))
	}
	name, ok := NormalizeName(*req.Name)
	if !ok {
		reason := "Too small: expected string to have >=1 characters"
		if name != "" {
			reason = "Too big: expected string to have <=255 characters"
		}
		return uuid.Nil, "", v3api.InvalidBody(v3api.InvalidParam{Name: "name", Reason: reason})
	}
	id, err := v3api.ParamUUID(c, "tagId")
	return id, name, err
}

// Input that fails validation is answered before an audit event exists, as forma2's wrapper built the event only after parsing; every request past that point is recorded, refused or not.

func (h *Handler) renameV3(c *gin.Context) {
	id, name, err := renameInput(c)
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	var change Change
	t, err := h.svc.RenameV3(c.Request.Context(), principal(c), id, name, &change)
	h.record(c, audit.ActionUpdated, change, err)
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	v3api.OK(c, serialize(*t))
}

func (h *Handler) deleteV3(c *gin.Context) {
	id, err := v3api.ParamUUID(c, "tagId")
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	var change Change
	err = h.svc.DeleteV3(c.Request.Context(), principal(c), id, &change)
	h.record(c, audit.ActionDeleted, change, err)
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	v3api.OK(c, gin.H{"id": id})
}

type mergeRequest struct {
	NewTagID *string `json:"newTagId"`
}

// mergeInput parses POST /api/v3/tags/{tagId}/merge: ZV3MergeTagBody, then the route parameter.
func mergeInput(c *gin.Context) (id, target uuid.UUID, err error) {
	var req mergeRequest
	if err := v3api.ReadBody(c, &req); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if req.NewTagID == nil {
		return uuid.Nil, uuid.Nil, v3api.InvalidBody(v3api.RequiredString("newTagId"))
	}
	if target, err = uuid.Parse(*req.NewTagID); err != nil {
		return uuid.Nil, uuid.Nil, v3api.InvalidBody(v3api.InvalidParam{Name: "newTagId", Reason: "Invalid UUID"})
	}
	id, err = v3api.ParamUUID(c, "tagId")
	return id, target, err
}

func (h *Handler) mergeV3(c *gin.Context) {
	id, target, err := mergeInput(c)
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	var change Change
	err = h.svc.MergeV3(c.Request.Context(), principal(c), id, target, &change)
	h.record(c, audit.ActionMerged, change, err)
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	v3api.OK(c, gin.H{"id": target})
}
