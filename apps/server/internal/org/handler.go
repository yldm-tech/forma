package org

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Handler serves the organization routes of /api/app.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth) *Handler {
	return &Handler{svc: svc, sessions: sessions}
}

// Register mounts the routes on the /api/app group, which already applies the error envelope and the CSRF check to every mutation.
func (h *Handler) Register(r gin.IRouter) {
	r.GET("/home", h.sessions.Optional(), h.home)
	g := r.Group("/organizations", h.sessions.Require())
	g.GET("", h.list)
	g.POST("", h.create)
	g.GET("/:organizationId", h.get)
	g.PATCH("/:organizationId", h.update)
	g.DELETE("/:organizationId", h.delete)
	g.GET("/:organizationId/navigation", h.navigate)
	g.GET("/:organizationId/members", h.members)
	g.PATCH("/:organizationId/members/:userId", h.changeRole)
	g.DELETE("/:organizationId/members/:userId", h.removeMember)
	g.POST("/:organizationId/members/:userId/suspension", h.suspendMember)
	g.DELETE("/:organizationId/members/:userId/suspension", h.restoreMember)
	g.POST("/:organizationId/leave", h.leave)
	g.GET("/:organizationId/pretty-urls", h.prettyURLs)
	g.GET("/:organizationId/retention", h.retention)
	g.PUT("/:organizationId/retention", h.setRetention)
	g.GET("/:organizationId/retention/preview", h.retentionPreview)
	g.PUT("/:organizationId/whitelabel/email-logo", h.setEmailLogo)
	g.DELETE("/:organizationId/whitelabel/email-logo", h.removeEmailLogo)
	g.PUT("/:organizationId/whitelabel/favicon", h.setFavicon)
	g.DELETE("/:organizationId/whitelabel/favicon", h.removeFavicon)
	g.POST("/:organizationId/whitelabel/test-email", h.sendTestEmail)
}

// userID is the authenticated caller; the routes that call it are mounted behind SessionAuth.Require.
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

// organizationID reads the :organizationId path parameter, failing the request when it is malformed.
func organizationID(c *gin.Context) (uuid.UUID, bool) {
	id, err := httpapi.ParamUUID(c, "organizationId")
	if err != nil {
		httpapi.Fail(c, err)
		return uuid.Nil, false
	}
	return id, true
}

// checkName is forma2's ZOrganization name rule, `.trim().min(1)`, which the organization forms applied. The server actions accepted an empty name; here the rule holds on every path.
func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apperr.Validation([]apperr.FieldError{{Field: "name", Rule: "min", Param: "1"}})
	}
	return name, nil
}

type pageQuery struct {
	Page int `form:"page" binding:"omitempty,min=1"`
}

func (h *Handler) home(c *gin.Context) {
	var user *uuid.UUID
	if a, ok := httpapi.ActorFrom(c.Request.Context()); ok {
		user = &a.UserID
	}
	res, err := h.svc.Home(c.Request.Context(), user)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type listResponse struct {
	Organizations []Summary `json:"organizations"`
}

func (h *Handler) list(c *gin.Context) {
	var q pageQuery
	if err := httpapi.BindQuery(c, &q); err != nil {
		httpapi.Fail(c, err)
		return
	}
	orgs, err := h.svc.List(c.Request.Context(), userID(c), q.Page)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, listResponse{Organizations: orgs})
}

type createRequest struct {
	Name string `json:"name"`
	// TimeZone is the creator's browser zone (Intl.DateTimeFormat().resolvedOptions().timeZone), which the new organization displays dates and schedules surveys in. It is a default, not a choice the creator made, so a zone this build cannot load is dropped rather than refused: the organization then falls back to UTC and the instance's zone as before, and creating it never fails over a browser's zone name.
	TimeZone *string `json:"timeZone"`
}

func (r *createRequest) validate() error {
	name, err := checkName(r.Name)
	r.Name = name
	if r.TimeZone != nil && !ValidTimeZone(*r.TimeZone) {
		r.TimeZone = nil
	}
	return err
}

func (h *Handler) create(c *gin.Context) {
	req := &createRequest{}
	if !bind(c, req) {
		return
	}
	res, err := h.svc.Create(c.Request.Context(), userID(c), req.Name, req.TimeZone)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, res)
}

func (h *Handler) get(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
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

type updateRequest struct {
	Name                     *string        `json:"name"`
	DisplayTimeZone          nullableString `json:"displayTimeZone"`
	SurveySchedulingTimeZone nullableString `json:"surveySchedulingTimeZone"`
	IsAISmartToolsEnabled    *bool          `json:"isAISmartToolsEnabled"`
	// RequireTwoFactor turns the two-factor requirement on or off; TwoFactorGracePeriodDays, the days members have to turn it on, comes with true and only with it.
	RequireTwoFactor         *bool `json:"requireTwoFactor"`
	TwoFactorGracePeriodDays *int  `json:"twoFactorGracePeriodDays"`
}

func (r *updateRequest) validate() error {
	if r.Name != nil {
		name, err := checkName(*r.Name)
		if err != nil {
			return err
		}
		r.Name = &name
	}
	if tz := r.DisplayTimeZone.value; tz != nil && !ValidTimeZone(*tz) {
		return apperr.Validation([]apperr.FieldError{{Field: "displayTimeZone", Rule: "timezone"}})
	}
	if tz := r.SurveySchedulingTimeZone.value; tz != nil && !ValidTimeZone(*tz) {
		return apperr.Validation([]apperr.FieldError{{Field: "surveySchedulingTimeZone", Rule: "timezone"}})
	}
	required := r.RequireTwoFactor != nil && *r.RequireTwoFactor
	switch days := r.TwoFactorGracePeriodDays; {
	case required && days == nil:
		return apperr.Validation([]apperr.FieldError{{Field: "twoFactorGracePeriodDays", Rule: "required"}})
	case !required && days != nil:
		return apperr.Validation([]apperr.FieldError{{Field: "twoFactorGracePeriodDays", Rule: "excluded_unless", Param: "requireTwoFactor true"}})
	case days != nil && *days < 0:
		return apperr.Validation([]apperr.FieldError{{Field: "twoFactorGracePeriodDays", Rule: "min", Param: "0"}})
	case days != nil && *days > MaxTwoFactorGracePeriodDays:
		return apperr.Validation([]apperr.FieldError{{Field: "twoFactorGracePeriodDays", Rule: "max", Param: strconv.Itoa(MaxTwoFactorGracePeriodDays)}})
	}
	return nil
}

func (h *Handler) update(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	req := &updateRequest{}
	if !bind(c, req) {
		return
	}
	in := UpdateInput{Name: req.Name, IsAISmartToolsEnabled: req.IsAISmartToolsEnabled}
	if req.RequireTwoFactor != nil {
		in.TwoFactorRequirement = &TwoFactorRequirement{Required: *req.RequireTwoFactor}
		if req.TwoFactorGracePeriodDays != nil {
			in.TwoFactorRequirement.GracePeriodDays = *req.TwoFactorGracePeriodDays
		}
	}
	if req.DisplayTimeZone.set {
		in.DisplayTimeZone = &TimeZone{Value: req.DisplayTimeZone.value}
	}
	if req.SurveySchedulingTimeZone.set {
		in.SurveySchedulingTimeZone = &TimeZone{Value: req.SurveySchedulingTimeZone.value}
	}
	res, err := h.svc.Update(c.Request.Context(), userID(c), id, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) delete(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), userID(c), id); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) navigate(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	res, err := h.svc.Navigate(c.Request.Context(), userID(c), id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type membersResponse struct {
	Members []Member `json:"members"`
}

func (h *Handler) members(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	var q pageQuery
	if err := httpapi.BindQuery(c, &q); err != nil {
		httpapi.Fail(c, err)
		return
	}
	members, err := h.svc.Members(c.Request.Context(), userID(c), id, q.Page)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, membersResponse{Members: members})
}

type changeRoleRequest struct {
	Role string `json:"role" binding:"required,oneof=owner manager member billing"`
}

func (h *Handler) changeRole(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	target, err := httpapi.ParamUUID(c, "userId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	req := &changeRoleRequest{}
	if err := httpapi.BindJSON(c, req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	m, err := h.svc.ChangeRole(c.Request.Context(), userID(c), id, target, req.Role)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) removeMember(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	target, err := httpapi.ParamUUID(c, "userId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if err := h.svc.RemoveMember(c.Request.Context(), userID(c), id, target); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) suspendMember(c *gin.Context) { h.setSuspension(c, true) }

func (h *Handler) restoreMember(c *gin.Context) { h.setSuspension(c, false) }

// setSuspension answers with the membership as it now is.
func (h *Handler) setSuspension(c *gin.Context, suspend bool) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	target, err := httpapi.ParamUUID(c, "userId")
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	m, err := h.svc.SetSuspension(c.Request.Context(), userID(c), id, target, suspend)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) leave(c *gin.Context) {
	id, ok := organizationID(c)
	if !ok {
		return
	}
	if err := h.svc.Leave(c.Request.Context(), userID(c), id); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
