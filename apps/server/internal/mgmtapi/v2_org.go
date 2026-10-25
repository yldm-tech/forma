package mgmtapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/apikey/disable"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/org"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/userinput"
)

// The organization routes of the v2 API, /api/v2/organizations/{organizationId}/(teams|users|workspace-teams). They accept organization-only keys; a key acts only on its own organization and needs its organization access for reading (GET) or writing (the rest).

// v2OrgParam reads the organizationId path parameter (422 when malformed).
func v2OrgParam(c *gin.Context) (uuid.UUID, bool) {
	return v2PathID(c, "organizationId")
}

// v2OrgAccess is forma2's hasOrganizationIdAndAccess: the path names the key's own organization and the key holds the access type on it; 401 otherwise.
func (h *Handler) v2OrgAccess(c *gin.Context, organizationID uuid.UUID, accessType string) bool {
	p := v2Principal(c)
	if organizationID != p.OrganizationID {
		h.log.ErrorContext(c.Request.Context(), "Organization ID from params does not match the authenticated organization ID")
		v2Error(c, http.StatusUnauthorized, nil)
		return false
	}
	allowed, err := h.authz.Can(c.Request.Context(), p.Actor(), authz.OrganizationActionForAccessType(accessType), authz.Organization(organizationID))
	if err != nil {
		h.v2Unexpected(c, err)
		return false
	}
	if !allowed {
		v2Error(c, http.StatusUnauthorized, nil)
		return false
	}
	return true
}

// v2CreatorRole is the role the key acts with, its creator's: forma2 anchored user and team management to it, as a key has no role of its own. "" when the creator is gone.
func (h *Handler) v2CreatorRole(c *gin.Context) (string, bool) {
	p := v2Principal(c)
	role, err := h.repo.creatorRole(c.Request.Context(), p.APIKeyID, p.OrganizationID)
	if err != nil {
		h.v2Unexpected(c, err)
		return "", false
	}
	return role, true
}

// canManageUsers is forma2's canManageOrganizationUsers: the creator's role clears USER_MANAGEMENT_MINIMUM_ROLE.
func (h *Handler) canManageUsers(role string) bool {
	return role != "" && org.UserManagementAllowed(role, h.cfg.UserManagementMinimumRole)
}

// Issues of the 403s a key's creator role causes. forma2 answered them bare; the key holder cannot see their creator's role anywhere, so the answer now says which rule refused the request.
const (
	issueCreatorGone        = "Key creator is no longer a member of the organization"
	issueCreatorCannotTeams = "Key creator's current role cannot manage teams"
	issueCreatorCannotUsers = "Key creator's current role cannot manage users"
	issueCreatorCannotRole  = "Key creator's current role cannot assign this role"
	issueCreatorCannotOwner = "Key creator's current role cannot change an owner"
)

// v2CreatorForbidden is the 403 of a change the key creator's role does not allow, with the rule as the apiKey detail; a creator who left the organization is named as such.
func v2CreatorForbidden(c *gin.Context, role, issue string) {
	if role == "" {
		issue = issueCreatorGone
	}
	v2Error(c, http.StatusForbidden, detail("apiKey", issue))
}

// canAssignRole is forma2's canAssignOrganizationRole: an owner may assign any role, a manager only member and never over an owner, anyone else nothing.
func canAssignRole(assigner, target, targetCurrent string) bool {
	if assigner == org.RoleOwner {
		return true
	}
	if targetCurrent == org.RoleOwner {
		return false
	}
	return assigner == org.RoleManager && target == org.RoleMember
}

// ---- teams ----

func (h *Handler) v2ListTeams(c *gin.Context) {
	var is issues
	f := is.commonFilter(v2Query(c))
	is.dateRange(f)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	organizationID, ok := v2OrgParam(c)
	if !ok || !h.v2Limit(c) || !h.v2OrgAccess(c, organizationID, "read") {
		return
	}
	found, total, err := h.repo.teamPage(c.Request.Context(), organizationID, f)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	v2OK(c, found, f.meta(total))
}

// v2TeamName is ZTeamInput: {name}.
func v2TeamName(c *gin.Context) (string, bool) {
	body, ok := v2Body(c)
	if !ok {
		return "", false
	}
	var is issues
	name, _ := is.str("name", object(body).get("name"), required)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return "", false
	}
	return name, true
}

// v2TeamWrite is the guard of every team change: write access to the organization (401), then a creator who may manage users (403).
func (h *Handler) v2TeamWrite(c *gin.Context, organizationID uuid.UUID) bool {
	if !h.v2OrgAccess(c, organizationID, "write") {
		return false
	}
	role, ok := h.v2CreatorRole(c)
	if !ok {
		return false
	}
	if !h.canManageUsers(role) {
		v2CreatorForbidden(c, role, issueCreatorCannotTeams)
		return false
	}
	return true
}

func (h *Handler) v2TeamConflict(c *gin.Context) {
	v2Error(c, http.StatusConflict, detail("team", "Team name already exists"))
}

func (h *Handler) v2CreateTeam(c *gin.Context) {
	name, ok := v2TeamName(c)
	if !ok {
		return
	}
	organizationID, ok := v2OrgParam(c)
	if !ok || !h.v2Limit(c) || !h.v2TeamWrite(c, organizationID) {
		return
	}
	created, err := h.repo.createTeam(c.Request.Context(), organizationID, name)
	if errors.Is(err, errTeamNameTaken) {
		h.v2TeamConflict(c)
		return
	}
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	h.v2Record(c, created.ID.String(), nil, created)
	v2Created(c, created)
}

// v2TeamParams reads the organization and team path parameters.
func v2TeamParams(c *gin.Context) (organizationID, teamID uuid.UUID, ok bool) {
	if organizationID, ok = v2OrgParam(c); !ok {
		return
	}
	teamID, ok = v2PathID(c, "teamId")
	return
}

func (h *Handler) v2GetTeam(c *gin.Context) {
	organizationID, teamID, ok := v2TeamParams(c)
	if !ok || !h.v2Limit(c) || !h.v2OrgAccess(c, organizationID, "read") {
		return
	}
	found, err := h.repo.team(c.Request.Context(), organizationID, teamID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if found == nil {
		v2Error(c, http.StatusNotFound, detail("team", "not found"))
		return
	}
	v2OK(c, found, nil)
}

func (h *Handler) v2UpdateTeam(c *gin.Context) {
	name, ok := v2TeamName(c)
	if !ok {
		return
	}
	organizationID, teamID, ok := v2TeamParams(c)
	if !ok || !h.v2Limit(c) || !h.v2TeamWrite(c, organizationID) {
		return
	}
	ctx := c.Request.Context()
	old, err := h.repo.team(ctx, organizationID, teamID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	h.v2Record(c, teamID.String(), old, nil)
	updated, err := h.repo.renameTeam(ctx, organizationID, teamID, name)
	switch {
	case errors.Is(err, errTeamNameTaken):
		h.v2TeamConflict(c)
	case err != nil:
		h.v2Unexpected(c, err)
	case updated == nil:
		v2Error(c, http.StatusNotFound, detail("team", "not found"))
	default:
		h.v2Record(c, "", nil, updated)
		v2OK(c, updated, nil)
	}
}

func (h *Handler) v2DeleteTeam(c *gin.Context) {
	organizationID, teamID, ok := v2TeamParams(c)
	if !ok || !h.v2Limit(c) || !h.v2TeamWrite(c, organizationID) {
		return
	}
	h.v2Record(c, teamID.String(), nil, nil)
	deleted, err := h.repo.deleteTeam(c.Request.Context(), organizationID, teamID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if deleted == nil {
		v2Error(c, http.StatusNotFound, detail("team", "not found"))
		return
	}
	h.v2Record(c, "", deleted, nil)
	v2OK(c, deleted, nil)
}

// ---- users ----

// organizationRoles are the roles the users API assigns, every organization role but billing.
var organizationRoles = []string{org.RoleOwner, org.RoleManager, org.RoleMember}

// v2NotOnCloud refuses the users routes on Forma Cloud, as forma2 did.
func (h *Handler) v2NotOnCloud(c *gin.Context) bool {
	if h.cfg.IsFormaCloud {
		v2Error(c, http.StatusBadRequest, detail("organizationId", "This endpoint is not supported on Forma Cloud"))
		return false
	}
	return true
}

func (h *Handler) v2ListUsers(c *gin.Context) {
	q := v2Query(c)
	var is issues
	f := is.commonFilter(q)
	var uf userFilter
	uf.ID = filterID(is.optString("id", q.get("id"), optional))
	uf.Email = is.optString("email", q.get("email"), optional)
	is.dateRange(f)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	organizationID, ok := v2OrgParam(c)
	if !ok || !h.v2Limit(c) || !h.v2NotOnCloud(c) || !h.v2OrgAccess(c, organizationID, "read") {
		return
	}
	found, total, err := h.repo.userPage(c.Request.Context(), organizationID, uf, f)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	v2OK(c, found, f.meta(total))
}

// v2UserInput is ZUserInput (or, with patch, ZUserInputPatch): isActive, name, email, role and teams in ZUser's order.
type v2UserInput struct {
	isActive *bool
	name     *string
	email    string
	role     *string
	teams    []string
	hasTeams bool
}

func parseV2UserInput(body map[string]any, patch bool) (v2UserInput, *issues) {
	o := object(body)
	var is issues
	var in v2UserInput
	in.isActive = is.boolean("isActive", o.get("isActive"), optional)
	namePresence := required
	if patch {
		namePresence = optional
	}
	if name, ok := is.str("name", o.get("name"), namePresence); ok {
		name = jsTrim(name)
		switch {
		case name == "":
			is.add("name", "Name should be at least 1 character long")
		case !userinput.ValidNameFormat(name):
			is.add("name", "Invalid name format")
		}
		in.name = &name
	}
	email, ok := o.get("email").(string)
	switch {
	case !ok || !userinput.ValidEmailFormat(email):
		is.add("email", "Invalid email")
	case userinput.UTF16Len(email) > userinput.EmailMaxLength:
		is.add("email", "Too big: expected string to have <=255 characters")
	}
	in.email = email
	rolePresence := required
	if patch {
		rolePresence = optional
	}
	if role, ok := is.enum("role", o.get("role"), rolePresence, organizationRoles); ok {
		in.role = &role
	}
	if v := o.get("teams"); !isAbsent(v) {
		list, ok := v.([]any)
		if !ok {
			is.typeError("teams", "array", v)
		}
		in.hasTeams = ok
		in.teams = []string{}
		for i, t := range list {
			if s, ok := is.str(index("teams", i), t, required); ok {
				in.teams = append(in.teams, s)
			}
		}
	}
	return in, &is
}

// v2UserWrite is the guard of a user change: not on Forma Cloud, write access to the organization (401), then a creator who may manage users (403). It returns the creator's role.
func (h *Handler) v2UserWrite(c *gin.Context, organizationID uuid.UUID) (string, bool) {
	if !h.v2NotOnCloud(c) || !h.v2OrgAccess(c, organizationID, "write") {
		return "", false
	}
	role, ok := h.v2CreatorRole(c)
	if !ok {
		return "", false
	}
	if !h.canManageUsers(role) {
		v2CreatorForbidden(c, role, issueCreatorCannotUsers)
		return "", false
	}
	return role, true
}

// v2CreateUser is forma2's POST /users: a user with an auto-accepted membership in the role (which the key's creator must be allowed to assign) and contributor seats on the named teams of the organization, unknown names ignored.
func (h *Handler) v2CreateUser(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	in, is := parseV2UserInput(body, false)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	organizationID, ok := v2OrgParam(c)
	if !ok || !h.v2Limit(c) {
		return
	}
	creator, ok := h.v2UserWrite(c, organizationID)
	if !ok {
		return
	}
	if !canAssignRole(creator, *in.role, "") {
		v2CreatorForbidden(c, creator, issueCreatorCannotRole)
		return
	}
	ctx := c.Request.Context()
	teams, err := h.repo.teamsByName(ctx, organizationID, in.teams)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	teamIDs := make([]uuid.UUID, 0, len(teams))
	for _, name := range in.teams {
		if id, ok := teams[name]; ok && !containsID(teamIDs, id) {
			teamIDs = append(teamIDs, id)
		}
	}
	created, err := h.repo.createOrgUser(ctx, organizationID, newOrgUser{Name: *in.name, Email: in.email, Role: *in.role, IsActive: in.isActive, TeamIDs: teamIDs})
	if errors.Is(err, errEmailTaken) {
		// The same text whether the email's user is a member here or only has an account elsewhere on the instance, so the detail does not say which. The 409 itself still tells the key that the email has an account on the instance, against a 201 for an unused one; closing that needs the invite flow (decision D-AO01).
		v2Error(c, http.StatusConflict, detail("email", issueEmailUnavailable))
		return
	}
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	created.Teams = []string{}
	for _, name := range in.teams {
		if _, ok := teams[name]; ok && !contains(created.Teams, name) {
			created.Teams = append(created.Teams, name)
		}
	}
	h.v2Record(c, created.ID.String(), nil, created)
	v2Created(c, created)
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// issueEmailUnavailable is the 409 detail of a new user whose email another user has, wherever that user belongs.
const issueEmailUnavailable = "A new user cannot be created with this email"

// issueSharedAccount is the 422 detail of a change to a user's account (name, isActive) refused because the user also belongs to another organization: the account is global, and one organization's key must not rename it or lock it out of the others.
const issueSharedAccount = "The user also belongs to another organization, so this organization cannot change it"

// v2UpdateUser is forma2's PATCH /users: the member named by email, changed within what the key's creator may do to them (only an owner touches an owner), teams replaced within the organization when given; demoting or deactivating the last active owner is 409. The name and isActive belong to the user's account, not the membership: changing them for a user who also belongs to another organization is 422.
func (h *Handler) v2UpdateUser(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	in, is := parseV2UserInput(body, true)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	organizationID, ok := v2OrgParam(c)
	if !ok || !h.v2Limit(c) {
		return
	}
	if !h.v2NotOnCloud(c) || !h.v2OrgAccess(c, organizationID, "write") {
		return
	}
	ctx := c.Request.Context()
	creator, ok := h.v2CreatorRole(c)
	if !ok {
		return
	}
	current, err := h.repo.orgUserByEmail(ctx, organizationID, in.email)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	currentRole := ""
	if current != nil {
		currentRole = current.Role
	}
	if !h.canManageUsers(creator) {
		v2CreatorForbidden(c, creator, issueCreatorCannotUsers)
		return
	}
	if creator != org.RoleOwner && currentRole == org.RoleOwner {
		v2CreatorForbidden(c, creator, issueCreatorCannotOwner)
		return
	}
	if in.role != nil && !canAssignRole(creator, *in.role, currentRole) {
		v2CreatorForbidden(c, creator, issueCreatorCannotRole)
		return
	}
	if current == nil {
		v2Error(c, http.StatusNotFound, detail("user", "not found"))
		return
	}
	current.Teams, err = h.repo.userTeams(ctx, current.ID, &organizationID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	h.v2Record(c, current.ID.String(), *current, nil)
	// Only a value that changes is written: a client that sends the user's current name or isActive back is not refused for a user shared with another organization.
	patch := userPatch{Role: in.role, SetTeams: in.hasTeams}
	if in.name != nil && *in.name != current.Name {
		patch.Name = in.name
	}
	if in.isActive != nil && *in.isActive != current.IsActive {
		patch.IsActive = in.isActive
	}
	var teamNames []string
	if in.hasTeams {
		if patch.TeamIDs, err = h.repo.teamsByName(ctx, organizationID, in.teams); err != nil {
			h.v2Unexpected(c, err)
			return
		}
		for _, name := range in.teams {
			if _, ok := patch.TeamIDs[name]; ok && !contains(teamNames, name) {
				teamNames = append(teamNames, name)
			}
		}
	}
	disabled, err := h.repo.updateOrgUser(ctx, organizationID, current, patch)
	if err != nil {
		switch {
		case errors.Is(err, errLastOwner):
			v2Error(c, http.StatusConflict, detail("role", "Cannot remove the last owner of the organization"))
			return
		case errors.Is(err, errLastActiveOwner):
			v2Error(c, http.StatusConflict, detail("isActive", "Cannot deactivate the last active owner of the organization"))
			return
		case errors.Is(err, errSharedAccount):
			var details []v2Detail
			if patch.Name != nil {
				details = append(details, v2Detail{Field: "name", Issue: issueSharedAccount})
			}
			if patch.IsActive != nil {
				details = append(details, v2Detail{Field: "isActive", Issue: issueSharedAccount})
			}
			v2Error(c, http.StatusUnprocessableEntity, details)
			return
		}
		h.v2Unexpected(c, err)
		return
	}
	updated, err := h.repo.orgUserByID(ctx, organizationID, current.ID)
	if err != nil || updated == nil {
		if err == nil {
			err = errNotFound
		}
		h.v2Unexpected(c, err)
		return
	}
	updated.Teams = current.Teams
	if in.hasTeams {
		updated.Teams = append([]string{}, teamNames...)
	}
	h.v2Record(c, "", nil, updated)
	h.v2DisabledKeys(c, disabled)
	v2OK(c, updated, nil)
}

// v2DisabledKeys records the audit events of the API keys a role change through the users API disabled, attributed, as the change itself is, to the key that made it.
func (h *Handler) v2DisabledKeys(c *gin.Context, keys []disable.Key) {
	ctx := c.Request.Context()
	p := v2Principal(c)
	for _, e := range disable.Events(keys, disable.ReasonCreatorDemoted) {
		e.ActorType, e.ActorID = audit.ActorAPI, p.APIKeyID.String()
		e.APIURL = h.cfg.WebAppURL.Scheme + "://" + h.cfg.WebAppURL.Host + c.Request.URL.RequestURI()
		h.audit.Log(ctx, audit.FromRequest(ctx, e))
	}
}

// ---- workspace teams ----

// workspaceTeamPermissions are ZWorkspaceTeam's permissions.
var workspaceTeamPermissions = []string{"read", "readWrite", "manage"}

// projectAlias is forma2's normaliseProjectIdToWorkspaceId: projectId is the old name of workspaceId, which wins when both are sent.
func projectAlias(workspaceID, projectID *string) *string {
	if workspaceID != nil {
		return workspaceID
	}
	return projectID
}

func resolveOptional(raw *string) *uuid.UUID {
	if raw == nil {
		return nil
	}
	id, _ := rowid.Parse(*raw)
	return &id
}

func (h *Handler) v2ListWorkspaceTeams(c *gin.Context) {
	q := v2Query(c)
	var is issues
	f := is.commonFilter(q)
	teamID := optionalID(&is, "teamId", q.get("teamId"))
	workspaceID := optionalID(&is, "workspaceId", q.get("workspaceId"))
	projectID := optionalID(&is, "projectId", q.get("projectId"))
	is.dateRange(f)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	organizationID, ok := v2OrgParam(c)
	if !ok || !h.v2Limit(c) || !h.v2OrgAccess(c, organizationID, "read") {
		return
	}
	found, total, err := h.repo.workspaceTeamPage(c.Request.Context(), organizationID, resolveOptional(teamID), resolveOptional(projectAlias(workspaceID, projectID)), f)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	v2OK(c, found, f.meta(total))
}

func optionalID(is *issues, path string, v any) *string {
	if id, ok := is.id(path, v, optional); ok {
		return &id
	}
	return nil
}

// workspaceTeamInput is ZWorkspaceTeamInput: workspaceId, teamId, permission and the legacy projectId, in its declared order.
type workspaceTeamInput struct {
	teamID      uuid.UUID
	workspaceID *uuid.UUID
	permission  string
}

func parseWorkspaceTeamInput(body map[string]any) (workspaceTeamInput, *issues) {
	o := object(body)
	var is issues
	var in workspaceTeamInput
	workspaceID := optionalID(&is, "workspaceId", o.get("workspaceId"))
	team, _ := is.id("teamId", o.get("teamId"), required)
	in.teamID, _ = rowid.Parse(team)
	in.permission, _ = is.enum("permission", o.get("permission"), required, workspaceTeamPermissions)
	projectID := optionalID(&is, "projectId", o.get("projectId"))
	in.workspaceID = resolveOptional(projectAlias(workspaceID, projectID))
	return in, &is
}

// v2WorkspaceTeamGuard is what every workspace-team change checks once its input parsed: a workspace named (400, before anything else), write access to the organization (401), a creator who may manage users (403, as for every team change: a grant changes which workspaces the team's members reach), and the team and the workspace both of it (404).
func (h *Handler) v2WorkspaceTeamGuard(c *gin.Context, organizationID uuid.UUID, teamID uuid.UUID, workspaceID *uuid.UUID) bool {
	if workspaceID == nil {
		v2Error(c, http.StatusBadRequest, detail("workspaceId", "Either workspaceId or projectId must be provided"))
		return false
	}
	h.v2Record(c, workspaceID.String()+"-"+teamID.String(), nil, nil)
	if !h.v2TeamWrite(c, organizationID) {
		return false
	}
	ok, err := h.repo.teamAndWorkspaceInOrganization(c.Request.Context(), organizationID, teamID, *workspaceID)
	if err != nil {
		h.v2Unexpected(c, err)
		return false
	}
	if !ok {
		v2Error(c, http.StatusNotFound, detail("teamId/workspaceId", "not_found"))
		return false
	}
	return true
}

// v2CreateWorkspaceTeam is forma2's POST /workspace-teams: a team's access to a workspace; one it already has is 409. forma2 answered 200, not 201.
func (h *Handler) v2CreateWorkspaceTeam(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	in, is := parseWorkspaceTeamInput(body)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	organizationID, ok := v2OrgParam(c)
	if !ok || !h.v2Limit(c) || !h.v2WorkspaceTeamGuard(c, organizationID, in.teamID, in.workspaceID) {
		return
	}
	created, err := h.repo.createWorkspaceTeam(c.Request.Context(), in.teamID, *in.workspaceID, in.permission)
	if errors.Is(err, errWorkspaceTeamExists) {
		v2Error(c, http.StatusConflict, detail("workspaceTeam", "Workspace team already exists"))
		return
	}
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	h.v2Record(c, "", nil, created)
	v2OK(c, created, nil)
}

// v2UpdateWorkspaceTeam is forma2's PUT /workspace-teams: the permission of an existing grant.
func (h *Handler) v2UpdateWorkspaceTeam(c *gin.Context) {
	body, ok := v2Body(c)
	if !ok {
		return
	}
	in, is := parseWorkspaceTeamInput(body)
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	organizationID, ok := v2OrgParam(c)
	if !ok || !h.v2Limit(c) || !h.v2WorkspaceTeamGuard(c, organizationID, in.teamID, in.workspaceID) {
		return
	}
	ctx := c.Request.Context()
	old, err := h.repo.workspaceTeam(ctx, in.teamID, *in.workspaceID)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	updated, err := h.repo.updateWorkspaceTeam(ctx, in.teamID, *in.workspaceID, in.permission)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if updated == nil {
		v2Error(c, http.StatusNotFound, detail("workspaceTeam", "not found"))
		return
	}
	h.v2Record(c, "", old, updated)
	v2OK(c, updated, nil)
}

// v2DeleteWorkspaceTeam is forma2's DELETE /workspace-teams?teamId=&workspaceId=: the grant removed.
func (h *Handler) v2DeleteWorkspaceTeam(c *gin.Context) {
	q := v2Query(c)
	var is issues
	team, _ := is.id("teamId", q.get("teamId"), required)
	workspaceID := optionalID(&is, "workspaceId", q.get("workspaceId"))
	projectID := optionalID(&is, "projectId", q.get("projectId"))
	if is.failed() {
		v2Error(c, http.StatusUnprocessableEntity, is.v2Details())
		return
	}
	teamID, _ := rowid.Parse(team)
	target := resolveOptional(projectAlias(workspaceID, projectID))
	organizationID, ok := v2OrgParam(c)
	if !ok || !h.v2Limit(c) || !h.v2WorkspaceTeamGuard(c, organizationID, teamID, target) {
		return
	}
	deleted, err := h.repo.deleteWorkspaceTeam(c.Request.Context(), teamID, *target)
	if err != nil {
		h.v2Unexpected(c, err)
		return
	}
	if deleted == nil {
		v2Error(c, http.StatusNotFound, detail("workspaceTeam", "not found"))
		return
	}
	h.v2Record(c, "", deleted, nil)
	v2OK(c, deleted, nil)
}
