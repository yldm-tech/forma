package authz

import (
	"testing"
)

func TestPermissionMapsActionsToTheCheckedSpiceDBPermission(t *testing.T) {
	cases := []struct {
		actor    ActorType
		action   Action
		resource ResourceType
		role     string
		want     string
	}{
		{ActorUser, OrganizationRead, ResourceOrganization, "manager", "read"},
		{ActorUser, OrganizationManageAPIKeys, ResourceOrganization, "manager", "manage_api_keys"},
		{ActorUser, APIKeyManage, ResourceAPIKey, "manager", "manage"},
		{ActorUser, TeamDelete, ResourceTeam, "manager", "delete"},
		{ActorUser, WorkspaceShare, ResourceWorkspace, "manager", "share"},
		// Survey and response actions are evaluated on the workspace.
		{ActorUser, SurveyRead, ResourceSurvey, "manager", "read"},
		{ActorUser, SurveyResponseRead, ResourceSurvey, "manager", "read"},
		{ActorUser, SurveyResponseExport, ResourceSurvey, "manager", "read"},
		{ActorUser, SurveyWrite, ResourceSurvey, "manager", "write"},
		{ActorUser, SurveyDelete, ResourceSurvey, "manager", "write"},
		{ActorUser, SurveyPublish, ResourceSurvey, "manager", "write"},
		{ActorUser, SurveyManage, ResourceSurvey, "manager", "manage"},
		{ActorUser, ResponseRead, ResourceResponse, "manager", "read"},
		{ActorUser, ResponseExport, ResourceResponse, "manager", "read"},
		{ActorUser, ResponseWrite, ResourceResponse, "manager", "write"},
		{ActorUser, ResponseManage, ResourceResponse, "manager", "manage"},
		// USER_MANAGEMENT_MINIMUM_ROLE rewrites a user's manage_access only.
		{ActorUser, OrganizationManageAccess, ResourceOrganization, "manager", "manage_access"},
		{ActorUser, OrganizationManageAccess, ResourceOrganization, "owner", "write"},
		{ActorUser, OrganizationManageAccess, ResourceOrganization, "disabled", ""},
		{ActorAPIKey, OrganizationManageAccess, ResourceOrganization, "disabled", "manage_access"},
		{ActorAPIKey, OrganizationManageAccess, ResourceOrganization, "owner", "manage_access"},
	}
	for _, tc := range cases {
		a := &Authorizer{userManagementMinimumRole: tc.role}
		got, err := a.permission(Actor{Type: tc.actor}, tc.action, tc.resource)
		if err != nil || got != tc.want {
			t.Errorf("%s %s with %s = %q, %v; want %q", tc.actor, tc.action, tc.role, got, err, tc.want)
		}
	}
}

func TestPermissionRejectsActionsOutsideTheVocabulary(t *testing.T) {
	a := &Authorizer{userManagementMinimumRole: "manager"}
	for _, tc := range []struct {
		action   Action
		resource ResourceType
	}{
		{WorkspaceRead, ResourceOrganization},
		{"organization.delete", ResourceOrganization},
		{"workspace", ResourceWorkspace},
		{"survey.export", ResourceSurvey},
		{TeamRead, "dashboard"},
	} {
		if _, err := a.permission(User([16]byte{}), tc.action, tc.resource); err == nil {
			t.Errorf("%s on %s accepted", tc.action, tc.resource)
		}
	}
}

func TestEveryDeclaredActionIsInTheVocabulary(t *testing.T) {
	actions := map[Action]ResourceType{
		APIKeyRead: ResourceAPIKey, APIKeyManage: ResourceAPIKey,
		OrganizationRead: ResourceOrganization, OrganizationWrite: ResourceOrganization, OrganizationManage: ResourceOrganization,
		OrganizationManageBilling: ResourceOrganization, OrganizationReadAccess: ResourceOrganization, OrganizationManageAccess: ResourceOrganization,
		OrganizationManageAPIKeys: ResourceOrganization,
		TeamRead:                  ResourceTeam, TeamManage: ResourceTeam, TeamDelete: ResourceTeam,
		WorkspaceRead: ResourceWorkspace, WorkspaceWrite: ResourceWorkspace, WorkspaceManage: ResourceWorkspace, WorkspaceShare: ResourceWorkspace,
		SurveyRead: ResourceSurvey, SurveyWrite: ResourceSurvey, SurveyManage: ResourceSurvey, SurveyDelete: ResourceSurvey,
		SurveyPublish: ResourceSurvey, SurveyResponseRead: ResourceSurvey, SurveyResponseExport: ResourceSurvey,
		ResponseRead: ResourceResponse, ResponseWrite: ResourceResponse, ResponseManage: ResourceResponse, ResponseExport: ResourceResponse,
	}
	total := 0
	for _, perms := range permissions {
		total += len(perms)
	}
	if len(actions) != total {
		t.Fatalf("%d declared actions, %d permissions in the vocabulary", len(actions), total)
	}
	for action, resource := range actions {
		if _, err := action.parse(resource); err != nil {
			t.Errorf("%s: %v", action, err)
		}
	}
}

func TestPermissionLadders(t *testing.T) {
	for in, want := range map[string]Action{"manage": WorkspaceManage, "readWrite": WorkspaceWrite, "read": WorkspaceRead, "": WorkspaceRead, "other": WorkspaceRead} {
		if got := WorkspaceActionForTeamPermission(in); got != want {
			t.Errorf("team permission %q = %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]Action{"DELETE": WorkspaceManage, "GET": WorkspaceRead, "POST": WorkspaceWrite, "PUT": WorkspaceWrite, "PATCH": WorkspaceWrite} {
		if got := WorkspaceActionForMethod(in); got != want {
			t.Errorf("method %s = %s, want %s", in, got, want)
		}
	}
	if OrganizationActionForAccessType("write") != OrganizationManageAccess || OrganizationActionForAccessType("read") != OrganizationReadAccess {
		t.Error("organization access type mapping")
	}
}

func TestSpiceDBTypeNames(t *testing.T) {
	if spicedbType(ActorAPIKey) != "api_key" || spicedbType(ResourceAPIKey) != "api_key" || spicedbType(ResourceWorkspace) != "workspace" || spicedbType(ActorUser) != "user" {
		t.Fatal("unexpected SpiceDB type mapping")
	}
}
