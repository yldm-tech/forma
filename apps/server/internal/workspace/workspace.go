// Package workspace is the workspace module: creating, listing, reading, updating and deleting the workspaces of an organization, with forma2's fields, defaults and rules (apps/web/modules/workspaces, apps/web/lib/workspace), plus the redirects that depend on them: where a deleted workspace's user goes next, the onboarding gate, and the environment-id aliases.
package workspace

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DefaultName is the name of the workspace every new organization starts with (forma2's DEFAULT_WORKSPACE_NAME).
const DefaultName = "My workspace"

// ItemsPerPage is forma2's ITEMS_PER_PAGE, the page size of every paginated list.
const ItemsPerPage = 30

// defaultLanguage is the survey language every new workspace starts with, forma2's DEFAULT_LOCALE.
const defaultLanguage = "en-US"

// defaultContactAttributeKeys are the keys every new workspace is seeded with (forma2's DEFAULT_CONTACT_ATTRIBUTE_KEYS), all of type "default".
var defaultContactAttributeKeys = []struct {
	key, name, description string
	unique                 bool
}{
	{"userId", "User Id", "The user id of a contact", true},
	{"email", "Email", "The email of a contact", true},
	{"firstName", "First Name", "Your contact's first name", false},
	{"lastName", "Last Name", "Your contact's last name", false},
	{"language", "Language", "The language preference of a contact", false},
}

// Workspace is forma2's TWorkspace as the SPA reads it.
type Workspace struct {
	ID                  uuid.UUID       `json:"id"`
	CreatedAt           time.Time       `json:"createdAt"`
	UpdatedAt           time.Time       `json:"updatedAt"`
	LegacyEnvironmentID *string         `json:"legacyEnvironmentId"`
	Name                string          `json:"name"`
	OrganizationID      uuid.UUID       `json:"organizationId"`
	Styling             json.RawMessage `json:"styling"`
	RecontactDays       int             `json:"recontactDays"`
	InAppSurveyBranding bool            `json:"inAppSurveyBranding"`
	LinkSurveyBranding  bool            `json:"linkSurveyBranding"`
	Config              json.RawMessage `json:"config"`
	Placement           string          `json:"placement"`
	ClickOutsideClose   bool            `json:"clickOutsideClose"`
	Overlay             string          `json:"overlay"`
	Languages           []Language      `json:"languages"`
	AppSetupCompleted   bool            `json:"appSetupCompleted"`
	Logo                json.RawMessage `json:"logo"`
	CustomHeadScripts   *string         `json:"customHeadScripts"`
}

// Language is one of a workspace's survey languages.
type Language struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Code        string    `json:"code"`
	Alias       *string   `json:"alias"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

// SPA paths the redirect rules send a user to. They are forma2's, so bookmarked links keep working.

// HomePath is a workspace's landing page.
func HomePath(workspaceID uuid.UUID) string {
	return "/workspaces/" + workspaceID.String() + "/"
}

// LandingPath is the page of an organization in which the user can open no workspace.
func LandingPath(organizationID uuid.UUID) string {
	return "/organizations/" + organizationID.String() + "/landing"
}

// BillingPath is forma2's getOrganizationBillingPath, where a billing-role member is sent: the billing settings on Forma Cloud, and otherwise their own account settings, since off Cloud nothing in the organization is theirs.
func BillingPath(organizationID uuid.UUID, isFormaCloud bool) string {
	if isFormaCloud {
		return "/organizations/" + organizationID.String() + "/settings/billing"
	}
	return "/account/settings/profile"
}

// onboardingPath is the first step of the new-workspace flow: plan selection on Forma Cloud, the first survey elsewhere.
func onboardingPath(organizationID uuid.UUID, isFormaCloud bool) string {
	if isFormaCloud {
		return "/organizations/" + organizationID.String() + "/workspaces/new/plan"
	}
	return "/organizations/" + organizationID.String() + "/workspaces/new/survey"
}
