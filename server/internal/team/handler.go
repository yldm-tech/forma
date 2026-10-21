package team

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// Handler serves the team routes of /api/app. Every route needs a session.
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
	g.GET("/organizations/:organizationId/teams", h.list)
	g.POST("/organizations/:organizationId/teams", h.create)
	g.GET("/teams/:teamId", h.details)
	g.PUT("/teams/:teamId", h.update)
	g.DELETE("/teams/:teamId", h.delete)
	g.GET("/teams/:teamId/role", h.role)
	g.GET("/workspaces/:workspaceId/teams", h.workspaceTeams)
}

func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}

// pathID reads a UUID path parameter and fails the request when it is malformed.
func pathID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := httpapi.ParamUUID(c, name)
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, false
	}
	return id, true
}

// teamName is the trimmed name forma2's schemas required to be non-empty ("Team name is required").
func teamName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", apperr.Validation([]apperr.FieldError{{Field: "name", Rule: "min", Param: "1"}})
	}
	return name, nil
}

func (h *Handler) list(c *gin.Context) {
	orgID, ok := pathID(c, "organizationId")
	if !ok {
		return
	}
	teams, err := h.svc.List(c.Request.Context(), userID(c), orgID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, teams)
}

type createRequest struct {
	Name string `json:"name"`
}

type idResponse struct {
	ID uuid.UUID `json:"id"`
}

func (h *Handler) create(c *gin.Context) {
	orgID, ok := pathID(c, "organizationId")
	if !ok {
		return
	}
	var req createRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	name, err := teamName(req.Name)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	id, err := h.svc.Create(c.Request.Context(), userID(c), orgID, name)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, idResponse{ID: id})
}

func (h *Handler) details(c *gin.Context) {
	teamID, ok := pathID(c, "teamId")
	if !ok {
		return
	}
	d, err := h.svc.Details(c.Request.Context(), userID(c), teamID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

// updateRequest is forma2's ZTeamSettingsFormSchema: at least one member ("Please add at least one member") and one workspace ("Please add at least one workspace").
type updateRequest struct {
	Name    string `json:"name"`
	Members []struct {
		UserID uuid.UUID `json:"userId"`
		Role   string    `json:"role" binding:"oneof=admin contributor"`
	} `json:"members" binding:"required,min=1,dive"`
	Workspaces []struct {
		WorkspaceID uuid.UUID `json:"workspaceId"`
		Permission  string    `json:"permission" binding:"oneof=read readWrite manage"`
	} `json:"workspaces" binding:"required,min=1,dive"`
}

func (h *Handler) update(c *gin.Context) {
	teamID, ok := pathID(c, "teamId")
	if !ok {
		return
	}
	var req updateRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	name, err := teamName(req.Name)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	in := UpdateInput{Name: name}
	for _, m := range req.Members {
		in.Members = append(in.Members, MemberInput{UserID: m.UserID, Role: m.Role})
	}
	for _, w := range req.Workspaces {
		in.Workspaces = append(in.Workspaces, WorkspaceInput{WorkspaceID: w.WorkspaceID, Permission: w.Permission})
	}
	d, err := h.svc.Update(c.Request.Context(), userID(c), teamID, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) delete(c *gin.Context) {
	teamID, ok := pathID(c, "teamId")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), userID(c), teamID); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type roleResponse struct {
	Role *string `json:"role"`
}

func (h *Handler) role(c *gin.Context) {
	teamID, ok := pathID(c, "teamId")
	if !ok {
		return
	}
	role, err := h.svc.Role(c.Request.Context(), userID(c), teamID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, roleResponse{Role: role})
}

func (h *Handler) workspaceTeams(c *gin.Context) {
	workspaceID, ok := pathID(c, "workspaceId")
	if !ok {
		return
	}
	teams, err := h.svc.WorkspaceTeams(c.Request.Context(), userID(c), workspaceID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, teams)
}
