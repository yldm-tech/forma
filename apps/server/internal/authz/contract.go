// Package authz is Forma's authorization: one engine-independent contract (actors, resources and actions, forma2's apps/web/lib/authorization) evaluated by SpiceDB, the projection that keeps SpiceDB's relationships in step with PostgreSQL through the authz_projection_outbox table, and the reconciliation that audits and repairs the two against each other.
//
// Services ask Authorizer.Can or Authorizer.Require; they never talk to SpiceDB directly. A missing actor or resource is a denial; anything that prevents an answer (SpiceDB unreachable, a revocation not yet delivered, a database error) is an *Error, never a silent deny or allow.
package authz

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ActorType is the kind of principal a decision is made for.
type ActorType string

const (
	ActorUser   ActorType = "user"
	ActorAPIKey ActorType = "apiKey"
)

// Actor is a principal: a user (users.id) or an API key (api_keys.id).
type Actor struct {
	Type ActorType
	ID   uuid.UUID
}

// User and APIKey name an actor.
func User(id uuid.UUID) Actor   { return Actor{Type: ActorUser, ID: id} }
func APIKey(id uuid.UUID) Actor { return Actor{Type: ActorAPIKey, ID: id} }

// ResourceType is one of the resource kinds the vocabulary covers.
type ResourceType string

const (
	ResourceAPIKey       ResourceType = "apiKey"
	ResourceOrganization ResourceType = "organization"
	ResourceTeam         ResourceType = "team"
	ResourceWorkspace    ResourceType = "workspace"
	ResourceSurvey       ResourceType = "survey"
	ResourceResponse     ResourceType = "response"
)

// Resource is the object of a decision.
type Resource struct {
	Type ResourceType
	ID   uuid.UUID
}

// Organization, Workspace, Team, APIKeyResource, Survey and Response name a resource.
func Organization(id uuid.UUID) Resource   { return Resource{Type: ResourceOrganization, ID: id} }
func Workspace(id uuid.UUID) Resource      { return Resource{Type: ResourceWorkspace, ID: id} }
func Team(id uuid.UUID) Resource           { return Resource{Type: ResourceTeam, ID: id} }
func APIKeyResource(id uuid.UUID) Resource { return Resource{Type: ResourceAPIKey, ID: id} }
func Survey(id uuid.UUID) Resource         { return Resource{Type: ResourceSurvey, ID: id} }
func Response(id uuid.UUID) Resource       { return Resource{Type: ResourceResponse, ID: id} }

// Action is "<resource>.<permission>", forma2's TAuthorizationAction.
type Action string

const (
	APIKeyRead   Action = "apiKey.read"
	APIKeyManage Action = "apiKey.manage"

	OrganizationRead          Action = "organization.read"
	OrganizationWrite         Action = "organization.write"
	OrganizationManage        Action = "organization.manage"
	OrganizationManageBilling Action = "organization.manage_billing"
	OrganizationReadAccess    Action = "organization.read_access"
	OrganizationManageAccess  Action = "organization.manage_access"
	OrganizationManageAPIKeys Action = "organization.manage_api_keys"

	TeamRead   Action = "team.read"
	TeamManage Action = "team.manage"
	TeamDelete Action = "team.delete"

	WorkspaceRead   Action = "workspace.read"
	WorkspaceWrite  Action = "workspace.write"
	WorkspaceManage Action = "workspace.manage"
	WorkspaceShare  Action = "workspace.share"

	SurveyRead           Action = "survey.read"
	SurveyWrite          Action = "survey.write"
	SurveyManage         Action = "survey.manage"
	SurveyDelete         Action = "survey.delete"
	SurveyPublish        Action = "survey.publish"
	SurveyResponseRead   Action = "survey.response_read"
	SurveyResponseExport Action = "survey.response_export"

	ResponseRead   Action = "response.read"
	ResponseWrite  Action = "response.write"
	ResponseManage Action = "response.manage"
	ResponseExport Action = "response.export"
)

// permissions is forma2's AUTHORIZATION_PERMISSION_MAP: the valid permissions of each resource type.
var permissions = map[ResourceType][]string{
	ResourceAPIKey:       {"read", "manage"},
	ResourceOrganization: {"read", "write", "manage", "manage_billing", "read_access", "manage_access", "manage_api_keys"},
	ResourceTeam:         {"read", "manage", "delete"},
	ResourceWorkspace:    {"read", "write", "manage", "share"},
	ResourceSurvey:       {"read", "write", "manage", "delete", "publish", "response_read", "response_export"},
	ResourceResponse:     {"read", "write", "manage", "export"},
}

// workspacePermissionForDerivedAction rewrites survey and response actions into the workspace permission they are evaluated as: surveys and responses carry no relationships of their own.
var workspacePermissionForDerivedAction = map[Action]string{
	ResponseExport:       "read",
	ResponseManage:       "manage",
	ResponseRead:         "read",
	ResponseWrite:        "write",
	SurveyDelete:         "write",
	SurveyManage:         "manage",
	SurveyPublish:        "write",
	SurveyRead:           "read",
	SurveyResponseExport: "read",
	SurveyResponseRead:   "read",
	SurveyWrite:          "write",
}

// parse splits an action and checks it names a permission of resourceType.
func (a Action) parse(resourceType ResourceType) (string, error) {
	typ, permission, ok := strings.Cut(string(a), ".")
	if ok && ResourceType(typ) == resourceType {
		for _, p := range permissions[resourceType] {
			if p == permission {
				return permission, nil
			}
		}
	}
	return "", fmt.Errorf("invalid authorization action/resource combination %q on %q", a, resourceType)
}

// WorkspaceActionForTeamPermission maps a workspace_teams.permission to the action it requires: manage, readWrite, and anything else (including none) as read.
func WorkspaceActionForTeamPermission(permission string) Action {
	switch permission {
	case "manage":
		return WorkspaceManage
	case "readWrite":
		return WorkspaceWrite
	default:
		return WorkspaceRead
	}
}

// WorkspaceActionForMethod maps an HTTP method to the workspace action the management APIs require: DELETE needs manage, GET read, every other method write.
func WorkspaceActionForMethod(method string) Action {
	switch method {
	case "DELETE":
		return WorkspaceManage
	case "GET":
		return WorkspaceRead
	default:
		return WorkspaceWrite
	}
}

// OrganizationActionForAccessType maps an API key's organization access type ("read" or "write") to the action it requires.
func OrganizationActionForAccessType(accessType string) Action {
	if accessType == "write" {
		return OrganizationManageAccess
	}
	return OrganizationReadAccess
}

// SpiceDB definitions and the relation names the projection writes, the single source of truth shared by the projectors and the reconciliation.
const (
	typeUser         = "user"
	typeAPIKey       = "api_key"
	typeOrganization = "organization"
	typeTeam         = "team"
	typeWorkspace    = "workspace"

	relationParent     = "organization"
	relationTeamMember = "member"
)

// spicedbType maps the vocabulary's names to the schema's definitions; only the API key differs.
func spicedbType[T ~string](t T) string {
	if string(t) == "apiKey" {
		return typeAPIKey
	}
	return string(t)
}

// organizationRoleRelations maps memberships.role to organization#<relation>@user.
var organizationRoleRelations = map[string]string{"billing": "billing", "manager": "manager", "member": "member", "owner": "owner"}

// teamRoleRelations maps team_members.role to team#<relation>@user.
var teamRoleRelations = map[string]string{"admin": "admin", "contributor": "contributor"}

// workspaceTeamRelations maps workspace_teams.permission to workspace#<relation>@team#member.
var workspaceTeamRelations = map[string]string{"manage": "manager_team", "read": "reader_team", "readWrite": "writer_team"}

// workspaceAPIKeyRelations maps api_key_workspaces.permission to workspace#<relation>@api_key.
var workspaceAPIKeyRelations = map[string]string{"manage": "manager", "read": "reader", "write": "writer"}

// Organization-level API key access: organization#<relation>@api_key, independent flags rather than a ladder.
//
//nolint:gosec // relation names, not credentials
const (
	relationAPIKeyReader = "api_key_reader"
	relationAPIKeyWriter = "api_key_writer"
)

// Relation names in a fixed order, so the updates the projectors build are deterministic.
var (
	organizationRelationNames = []string{"billing", "manager", "member", "owner"}
	teamRelationNames         = []string{"admin", "contributor"}
	workspaceTeamRelNames     = []string{"manager_team", "reader_team", "writer_team"}
	workspaceAPIKeyRelNames   = []string{"manager", "reader", "writer"}
	apiKeyAccessRelNames      = []string{relationAPIKeyReader, relationAPIKeyWriter}
)
