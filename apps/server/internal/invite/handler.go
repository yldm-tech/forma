package invite

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/userinput"
)

// Handler serves the invite routes of /api/app. Everything but inspecting a link needs a session.
type Handler struct {
	svc      *Service
	sessions httpapi.SessionAuth
}

func NewHandler(svc *Service, sessions httpapi.SessionAuth) *Handler {
	return &Handler{svc: svc, sessions: sessions}
}

// Register mounts the routes on the /api/app group, which already applies the error envelope and the CSRF check to every mutation. Links travel in request bodies, never in a URL, so they cannot reach access logs.
func (h *Handler) Register(r gin.IRouter) {
	r.POST("/invites/inspect", h.sessions.Optional(), h.inspect)
	g := r.Group("", h.sessions.Require())
	g.GET("/organizations/:organizationId/invites", h.list)
	g.POST("/organizations/:organizationId/invites", h.create)
	g.POST("/organizations/:organizationId/invites/bulk", h.bulk)
	g.POST("/organizations/:organizationId/invites/:inviteId/resend", h.resend)
	g.POST("/organizations/:organizationId/setup-invites", h.setup)
	g.GET("/organizations/:organizationId/user-management", h.access)
	g.PATCH("/invites/:inviteId", h.updateRole)
	g.DELETE("/invites/:inviteId", h.delete)
	g.POST("/invites/:inviteId/link", h.link)
	g.POST("/invites/accept", h.accept)
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

// bindJSON decodes and validates the body, failing the request on error.
func bindJSON(c *gin.Context, dst any) bool {
	if err := httpapi.BindJSON(c, dst); err != nil {
		httpapi.Fail(c, err)
		return false
	}
	return true
}

type fieldErrors []apperr.FieldError

func (f *fieldErrors) add(field, rule, param string) {
	*f = append(*f, apperr.FieldError{Field: field, Rule: rule, Param: param})
}

func (f fieldErrors) err() error {
	if len(f) == 0 {
		return nil
	}
	return apperr.Validation(f)
}

// checkEmail applies ZUserEmail and returns the address lowercased, the form every invite is stored and compared in. forma2's single invite took any string here, and compared it case-sensitively.
func (f *fieldErrors) checkEmail(field, email string) string {
	switch {
	case email == "":
		f.add(field, "required", "")
	case userinput.UTF16Len(email) > userinput.EmailMaxLength:
		f.add(field, "max", strconv.Itoa(userinput.EmailMaxLength))
	case !userinput.ValidEmailFormat(email):
		f.add(field, "email", "")
	}
	return strings.ToLower(email)
}

// checkName requires a name that is not blank once trimmed, and with userName also ZUserName's character set, and returns it trimmed.
func (f *fieldErrors) checkName(field, name string, userName bool) string {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		f.add(field, "min", "1")
	case userName && !userinput.ValidNameFormat(name):
		f.add(field, "name_format", "")
	}
	return name
}

// checkRole requires one of the organization roles (ZOrganizationRole).
func (f *fieldErrors) checkRole(field, role string) {
	switch role {
	case RoleOwner, RoleManager, RoleMember, RoleBilling:
	default:
		f.add(field, "oneof", "owner manager member billing")
	}
}

type listQuery struct {
	Page int `form:"page" binding:"omitempty,min=1"`
}

func (h *Handler) list(c *gin.Context) {
	orgID, ok := pathID(c, "organizationId")
	if !ok {
		return
	}
	var q listQuery
	if err := httpapi.BindQuery(c, &q); err != nil {
		httpapi.Fail(c, err)
		return
	}
	invites, err := h.svc.List(c.Request.Context(), userID(c), orgID, q.Page)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, invites)
}

type idResponse struct {
	ID uuid.UUID `json:"id"`
}

// createRequest is forma2's ZInviteUserAction.
type createRequest struct {
	Email   string      `json:"email"`
	Name    string      `json:"name"`
	Role    string      `json:"role"`
	TeamIDs []uuid.UUID `json:"teamIds"`
}

func (h *Handler) create(c *gin.Context) {
	orgID, ok := pathID(c, "organizationId")
	if !ok {
		return
	}
	var req createRequest
	if !bindJSON(c, &req) {
		return
	}
	var errs fieldErrors
	in := Invitee{Email: errs.checkEmail("email", req.Email), Name: errs.checkName("name", req.Name, false), Role: req.Role, TeamIDs: req.TeamIDs}
	errs.checkRole("role", req.Role)
	// "An invite is limited to 100 teams": a team admin's invite is authorized per named team, so the count bounds the decisions one request can cause.
	if len(req.TeamIDs) > maxTeams {
		errs.add("teamIds", "max", strconv.Itoa(maxTeams))
	}
	if err := errs.err(); err != nil {
		httpapi.Fail(c, err)
		return
	}
	if in.TeamIDs == nil {
		in.TeamIDs = []uuid.UUID{}
	}
	id, err := h.svc.Create(c.Request.Context(), userID(c), orgID, in)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, idResponse{ID: id})
}

// bulkRequest is forma2's ZInvitees: every row a valid email, role and ZUserName. Team ids are strings, as forma2's were: one that is not an id fails its row, not the request.
type bulkRequest struct {
	Invitees []struct {
		Email   string   `json:"email"`
		Name    string   `json:"name"`
		Role    string   `json:"role"`
		TeamIDs []string `json:"teamIds"`
	} `json:"invitees"`
}

type bulkResponse struct {
	Results []BulkResult `json:"results"`
}

func (h *Handler) bulk(c *gin.Context) {
	orgID, ok := pathID(c, "organizationId")
	if !ok {
		return
	}
	var req bulkRequest
	if !bindJSON(c, &req) {
		return
	}
	var errs fieldErrors
	invitees := make([]BulkInvitee, 0, len(req.Invitees))
	for i, row := range req.Invitees {
		prefix := fmt.Sprintf("invitees[%d].", i)
		in := BulkInvitee{Invitee: Invitee{
			Email: errs.checkEmail(prefix+"email", row.Email), Name: errs.checkName(prefix+"name", row.Name, true), Role: row.Role, TeamIDs: []uuid.UUID{},
		}}
		errs.checkRole(prefix+"role", row.Role)
		for _, raw := range row.TeamIDs {
			id, err := uuid.Parse(raw)
			if err != nil {
				in.InvalidTeamIDs = true
				continue
			}
			in.TeamIDs = append(in.TeamIDs, id)
		}
		invitees = append(invitees, in)
	}
	if err := errs.err(); err != nil {
		httpapi.Fail(c, err)
		return
	}
	results, err := h.svc.Bulk(c.Request.Context(), userID(c), orgID, invitees)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, bulkResponse{Results: results})
}

// setupRequest is forma2's ZInviteOrganizationMemberAction: ZUserEmail and ZUserName.
type setupRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (h *Handler) setup(c *gin.Context) {
	orgID, ok := pathID(c, "organizationId")
	if !ok {
		return
	}
	var req setupRequest
	if !bindJSON(c, &req) {
		return
	}
	var errs fieldErrors
	email, name := errs.checkEmail("email", req.Email), errs.checkName("name", req.Name, true)
	if err := errs.err(); err != nil {
		httpapi.Fail(c, err)
		return
	}
	id, err := h.svc.Setup(c.Request.Context(), userID(c), orgID, email, name)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, idResponse{ID: id})
}

func (h *Handler) access(c *gin.Context) {
	orgID, ok := pathID(c, "organizationId")
	if !ok {
		return
	}
	access, err := h.svc.Access(c.Request.Context(), userID(c), orgID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, access)
}

func (h *Handler) resend(c *gin.Context) {
	orgID, ok := pathID(c, "organizationId")
	if !ok {
		return
	}
	inviteID, ok := pathID(c, "inviteId")
	if !ok {
		return
	}
	resent, err := h.svc.Resend(c.Request.Context(), userID(c), orgID, inviteID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, resent)
}

type roleRequest struct {
	Role string `json:"role" binding:"oneof=owner manager member billing"`
}

func (h *Handler) updateRole(c *gin.Context) {
	inviteID, ok := pathID(c, "inviteId")
	if !ok {
		return
	}
	var req roleRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.UpdateRole(c.Request.Context(), userID(c), inviteID, req.Role); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) delete(c *gin.Context) {
	inviteID, ok := pathID(c, "inviteId")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), userID(c), inviteID); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) link(c *gin.Context) {
	inviteID, ok := pathID(c, "inviteId")
	if !ok {
		return
	}
	link, err := h.svc.Link(c.Request.Context(), userID(c), inviteID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, link)
}

type tokenRequest struct {
	Token string `json:"token" binding:"required,max=512"`
}

func (h *Handler) inspect(c *gin.Context) {
	var req tokenRequest
	if !bindJSON(c, &req) {
		return
	}
	var caller *uuid.UUID
	if a, ok := httpapi.ActorFrom(c.Request.Context()); ok {
		caller = &a.UserID
	}
	res, err := h.svc.Inspect(c.Request.Context(), req.Token, caller)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type acceptResponse struct {
	OrganizationID uuid.UUID `json:"organizationId"`
}

func (h *Handler) accept(c *gin.Context) {
	var req tokenRequest
	if !bindJSON(c, &req) {
		return
	}
	orgID, err := h.svc.Accept(c.Request.Context(), userID(c), req.Token)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, acceptResponse{OrganizationID: orgID})
}
