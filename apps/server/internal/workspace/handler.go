package workspace

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves the workspace routes of /api/app. Every route needs a session.
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
	g.GET("/organizations/:organizationId/workspaces", h.list)
	g.POST("/organizations/:organizationId/workspaces", h.create)
	g.GET("/organizations/:organizationId/onboarding", h.onboarding)
	g.GET("/workspaces/:workspaceId", h.get)
	g.PATCH("/workspaces/:workspaceId", h.update)
	g.POST("/workspaces/:workspaceId/delete", h.delete)
	g.GET("/environments/:environmentId", h.environment)
	g.GET("/workspaces/:workspaceId/app-connection", h.appConnection)
	g.POST("/workspaces/:workspaceId/languages", h.createLanguage)
	g.PATCH("/workspaces/:workspaceId/languages/:languageId", h.updateLanguage)
	g.DELETE("/workspaces/:workspaceId/languages/:languageId", h.deleteLanguage)
	g.GET("/workspaces/:workspaceId/languages/:languageId/surveys", h.languageSurveys)
}

// userID is the authenticated caller; every route is mounted behind SessionAuth.Require.
func userID(c *gin.Context) uuid.UUID {
	a, _ := httpapi.ActorFrom(c.Request.Context())
	return a.UserID
}

// bind decodes the body and runs the request's own field rules.
func bind[T interface{ validate() error }](c *gin.Context, req T) bool {
	if err := httpapi.BindJSON(c, req); err != nil {
		httpapi.Fail(c, err)
		return false
	}
	if err := req.validate(); err != nil {
		httpapi.Fail(c, err)
		return false
	}
	return true
}

// trimmedName is forma2's `.trim().min(1)` on a workspace name.
func trimmedName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apperr.Validation([]apperr.FieldError{{Field: "name", Rule: "min", Param: "1"}})
	}
	return name, nil
}

type listQuery struct {
	Page int `form:"page" binding:"omitempty,min=1"`
	// Access "write" lists the workspaces the caller may write to instead of read.
	Access string `form:"access" binding:"omitempty,oneof=read write"`
}

type listResponse struct {
	Workspaces []*Workspace `json:"workspaces"`
}

func (h *Handler) list(c *gin.Context) {
	orgID, err := httpapi.ParamUUID(c, "organizationId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var q listQuery
	if err := httpapi.BindQuery(c, &q); err != nil {
		httpapi.Fail(c, err)
		return
	}
	list, err := h.svc.List(c.Request.Context(), userID(c), orgID, q.Access == "write", q.Page)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, listResponse{Workspaces: list})
}

type createRequest struct {
	Name    string      `json:"name"`
	TeamIDs []uuid.UUID `json:"teamIds"`
	Config  *struct {
		Channel  *string `json:"channel" binding:"omitempty,oneof=link app website"`
		Industry *string `json:"industry" binding:"omitempty,oneof=eCommerce saas other"`
	} `json:"config"`
}

func (r *createRequest) validate() error {
	name, err := trimmedName(r.Name)
	r.Name = name
	return err
}

func (h *Handler) create(c *gin.Context) {
	orgID, err := httpapi.ParamUUID(c, "organizationId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	req := &createRequest{}
	if !bind(c, req) {
		return
	}
	in := NewWorkspace{Name: req.Name, TeamIDs: req.TeamIDs}
	if req.Config != nil {
		in.Channel, in.Industry = req.Config.Channel, req.Config.Industry
	}
	ws, err := h.svc.Create(c.Request.Context(), userID(c), orgID, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, ws)
}

func (h *Handler) onboarding(c *gin.Context) {
	orgID, err := httpapi.ParamUUID(c, "organizationId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	res, err := h.svc.Onboarding(c.Request.Context(), userID(c), orgID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) get(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// nullableString tells an absent field from an explicit null.
type nullableString struct {
	set   bool
	value *string
}

func (n *nullableString) UnmarshalJSON(b []byte) error {
	n.set = true
	if string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	n.value = &s
	return nil
}

// updateRequest is forma2's ZWorkspaceUpdateInput as far as it maps onto stored columns. organizationId is accepted and ignored, as forma2 stripped it; its highlightBorderColor and teamIds had no column to update and are ignored too.
type updateRequest struct {
	Name *string `json:"name"`
	// RecontactDays is bounded like ZWorkspace's recontactDays; forma2's update input left the bound to the form.
	RecontactDays       *int            `json:"recontactDays" binding:"omitempty,min=0,max=365"`
	InAppSurveyBranding *bool           `json:"inAppSurveyBranding"`
	LinkSurveyBranding  *bool           `json:"linkSurveyBranding"`
	Placement           *string         `json:"placement" binding:"omitempty,oneof=bottomLeft bottomRight topLeft topRight center"`
	ClickOutsideClose   *bool           `json:"clickOutsideClose"`
	Overlay             *string         `json:"overlay" binding:"omitempty,oneof=none light dark"`
	Config              json.RawMessage `json:"config"`
	Styling             json.RawMessage `json:"styling"`
	Logo                json.RawMessage `json:"logo"`
	CustomHeadScripts   nullableString  `json:"customHeadScripts"`
}

func (r *updateRequest) validate() error {
	if r.Name != nil {
		name, err := trimmedName(*r.Name)
		if err != nil {
			return err
		}
		r.Name = &name
	}
	return nil
}

func (h *Handler) update(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	req := &updateRequest{}
	if !bind(c, req) {
		return
	}
	in := UpdateInput{
		Name: req.Name, RecontactDays: req.RecontactDays, InAppSurveyBranding: req.InAppSurveyBranding, LinkSurveyBranding: req.LinkSurveyBranding,
		Placement: req.Placement, ClickOutsideClose: req.ClickOutsideClose, Overlay: req.Overlay, Config: req.Config, Styling: req.Styling, Logo: req.Logo,
	}
	if req.CustomHeadScripts.set {
		in.CustomHeadScripts = &Optional[string]{Value: req.CustomHeadScripts.value}
	}
	ws, err := h.svc.Update(c.Request.Context(), userID(c), id, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, ws)
}

type deleteRequest struct {
	ConfirmationName string `json:"confirmationName"`
}

func (h *Handler) delete(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	req := &deleteRequest{}
	if err := httpapi.BindJSON(c, req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	res, err := h.svc.Delete(c.Request.Context(), userID(c), id, req.ConfirmationName)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type environmentResponse struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

func (h *Handler) environment(c *gin.Context) {
	id, err := h.svc.ResolveEnvironment(c.Request.Context(), userID(c), c.Param("environmentId"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, environmentResponse{WorkspaceID: id})
}

func (h *Handler) appConnection(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	res, err := h.svc.AppConnection(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// languageParams reads the workspace and language ids of a language route.
func languageParams(c *gin.Context) (workspaceID, languageID uuid.UUID, ok bool) {
	workspaceID, err := httpapi.ParamUUID(c, "workspaceId")
	if err == nil {
		languageID, err = httpapi.ParamUUID(c, "languageId")
	}
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, uuid.Nil, false
	}
	return workspaceID, languageID, true
}

// createLanguageRequest is ZLanguageInput.
type createLanguageRequest struct {
	Code  string  `json:"code"`
	Alias *string `json:"alias"`
}

func (h *Handler) createLanguage(c *gin.Context) {
	id, err := httpapi.ParamUUID(c, "workspaceId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	var req createLanguageRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	l, err := h.svc.CreateLanguage(c.Request.Context(), userID(c), id, req.Code, req.Alias)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, l)
}

// updateLanguageRequest is ZLanguageUpdate: a code in the body is ignored, as zod stripped it.
type updateLanguageRequest struct {
	Alias *string `json:"alias"`
}

func (h *Handler) updateLanguage(c *gin.Context) {
	ws, lang, ok := languageParams(c)
	if !ok {
		return
	}
	var req updateLanguageRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	l, err := h.svc.UpdateLanguage(c.Request.Context(), userID(c), ws, lang, req.Alias)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, l)
}

func (h *Handler) deleteLanguage(c *gin.Context) {
	ws, lang, ok := languageParams(c)
	if !ok {
		return
	}
	l, err := h.svc.DeleteLanguage(c.Request.Context(), userID(c), ws, lang)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, l)
}

type languageSurveysResponse struct {
	Surveys []string `json:"surveys"`
}

func (h *Handler) languageSurveys(c *gin.Context) {
	ws, lang, ok := languageParams(c)
	if !ok {
		return
	}
	names, err := h.svc.SurveysUsingLanguage(c.Request.Context(), userID(c), ws, lang)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, languageSurveysResponse{Surveys: names})
}
