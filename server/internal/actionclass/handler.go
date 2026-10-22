package actionclass

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/v3api"
)

// Handler serves the action class routes of /api/app (session only) and /api/v3.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
	v3       *v3api.Middleware
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth, v3 *v3api.Middleware) *Handler {
	return &Handler{svc: svc, sessions: sessions, v3: v3}
}

// Register mounts the /api/app routes.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.sessions.Require())
	g.GET("/workspaces/:workspaceId/action-classes", h.list)
	g.POST("/workspaces/:workspaceId/action-classes", h.create)
	g.PATCH("/action-classes/:actionClassId", h.update)
	g.DELETE("/action-classes/:actionClassId", h.delete)
	g.GET("/action-classes/:actionClassId/surveys", h.surveys)
}

// RegisterV3 mounts GET /api/v3/action-classes, which accepts a session or an API key.
func (h *Handler) RegisterV3(r gin.IRouter) {
	r.GET("/action-classes", h.v3.Auth(v3api.AuthBoth), h.listV3)
}

func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}

type listResponse struct {
	ActionClasses []*ActionClass `json:"actionClasses"`
}

func (h *Handler) list(c *gin.Context) {
	ws, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	list, err := h.svc.List(c.Request.Context(), userID(c), ws)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, listResponse{ActionClasses: list})
}

// optionalString tells an absent field from an explicit null.
type optionalString struct {
	set   bool
	value *string
}

func (o *optionalString) UnmarshalJSON(b []byte) error {
	o.set = true
	if string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	o.value = &s
	return nil
}

// inputRequest is ZActionClassInput; its workspaceId is ignored, since the workspace comes from the path or from the stored action class.
type inputRequest struct {
	Name         string          `json:"name"`
	Description  optionalString  `json:"description"`
	Type         string          `json:"type"`
	Key          *string         `json:"key"`
	NoCodeConfig json.RawMessage `json:"noCodeConfig"`
}

func bindInput(c *gin.Context) (Input, bool) {
	var req inputRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return Input{}, false
	}
	in := Input{Name: req.Name, Type: req.Type, Key: req.Key}
	if req.Description.set {
		in.Description = &Optional{Value: req.Description.value}
	}
	if err := ValidateInput(&in, req.NoCodeConfig); err != nil {
		httpapi.Fail(c, err)
		return Input{}, false
	}
	return in, true
}

func (h *Handler) create(c *gin.Context) {
	ws, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	in, ok := bindInput(c)
	if !ok {
		return
	}
	ac, err := h.svc.Create(c.Request.Context(), userID(c), ws, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, ac)
}

func (h *Handler) update(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "actionClassId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	in, ok := bindInput(c)
	if !ok {
		return
	}
	ac, err := h.svc.Update(c.Request.Context(), userID(c), id, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, ac)
}

func (h *Handler) delete(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "actionClassId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	ac, err := h.svc.Delete(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, ac)
}

type surveysQuery struct {
	ExcludeSurveyID string `form:"excludeSurveyId" binding:"omitempty,uuid"`
}

func (h *Handler) surveys(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "actionClassId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var q surveysQuery
	if err := httpapi.BindQuery(c, &q); err != nil {
		httpapi.Fail(c, err)
		return
	}
	var exclude *uuid.UUID
	if q.ExcludeSurveyID != "" {
		ex := uuid.MustParse(q.ExcludeSurveyID)
		exclude = &ex
	}
	res, err := h.svc.SurveysUsing(c.Request.Context(), userID(c), id, exclude)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// v3ActionClass is the public v3 shape, forma2's serializeV3ActionClass: what a client needs to reference an action class from an app-survey trigger, without the no-code configuration, the workspace or the timestamps.
type v3ActionClass struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	Type        string    `json:"type"`
	Key         *string   `json:"key"`
}

func (h *Handler) listV3(c *gin.Context) {
	q := v3api.ReadQuery(c, "workspaceId", "limit", "cursor")
	ws := q.UUID("workspaceId")
	limit, cursor := v3api.ReadListQuery(q)
	if err := q.Err(); err != nil {
		v3api.Fail(c, err)
		return
	}
	list, err := h.svc.ListV3(c.Request.Context(), v3api.PrincipalFrom(c.Request.Context()), ws)
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	page, next, err := v3api.Paginate(list, func(a *ActionClass) string { return a.ID.String() }, limit, cursor)
	if err != nil {
		v3api.Fail(c, err)
		return
	}
	out := make([]v3ActionClass, len(page))
	for i, a := range page {
		out[i] = v3ActionClass{ID: a.ID, Name: a.Name, Description: a.Description, Type: a.Type, Key: a.Key}
	}
	v3api.List(c, out, v3api.ListMeta{Limit: limit, NextCursor: next})
}
