// Package org is the organization module: creating an organization (the setup flow and the organization switcher), reading, renaming and deleting it, listing the caller's organizations, and its memberships (listing members, changing a role, suspending and restoring a member's access, removing a member, leaving), with forma2's role rules and the invariant that an organization always keeps an active owner (apps/web/lib/organization, lib/membership, modules/organization, modules/role-management). It also answers the redirect questions the root page and the organization route asked.
package org

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Organization roles (memberships.role).
const (
	RoleOwner   = "owner"
	RoleManager = "manager"
	RoleMember  = "member"
	RoleBilling = "billing"
)

// ItemsPerPage is forma2's ITEMS_PER_PAGE.
const ItemsPerPage = 30

// Organization is forma2's TOrganization as the SPA reads it.
type Organization struct {
	ID                    uuid.UUID       `json:"id"`
	CreatedAt             time.Time       `json:"createdAt"`
	UpdatedAt             time.Time       `json:"updatedAt"`
	Name                  string          `json:"name"`
	Billing               Billing         `json:"billing"`
	IsAISmartToolsEnabled bool            `json:"isAISmartToolsEnabled"`
	Whitelabel            json.RawMessage `json:"whitelabel"`
	// DisplayTimeZone is an IANA zone; nil means UTC.
	DisplayTimeZone *string `json:"displayTimeZone"`
	// SurveySchedulingTimeZone is the IANA zone the organization's scheduled surveys open and close in; nil means the instance's SURVEY_SCHEDULING_TIME_ZONE.
	SurveySchedulingTimeZone *string `json:"surveySchedulingTimeZone"`
	// TwoFactorEnforcedAt is nil while two-factor authentication is optional. Once an owner requires it, it is the end of the grace period: members who sign in with a password and have not turned it on are reminded until then and refused the organization from then on (authz).
	TwoFactorEnforcedAt *time.Time `json:"twoFactorEnforcedAt"`
}

// limited is the organization as a member it refuses (suspended, or held to its two-factor requirement) sees it: its name and its two-factor requirement, which the suspension notice, the guide to enrolment and the account's two-factor page show. Its billing, branding and settings are left out: such a member may decide nothing else in the organization, and a suspension is often how someone leaving it is cut off.
func (o *Organization) limited() *Organization {
	return &Organization{ID: o.ID, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, Name: o.Name, Whitelabel: json.RawMessage(`{}`), TwoFactorEnforcedAt: o.TwoFactorEnforcedAt}
}

// Billing is the organization's billing state, forma2's TOrganizationBilling.
type Billing struct {
	StripeCustomerID *string         `json:"stripeCustomerId"`
	Limits           json.RawMessage `json:"limits"`
	UsageCycleAnchor *time.Time      `json:"usageCycleAnchor"`
	Stripe           json.RawMessage `json:"stripe,omitempty"`
}

// defaultLimits is forma2's getDefaultOrganizationBilling limits: one workspace and 250 responses a month on Forma Cloud, three and 1500 self-hosted, and no included workflow runs. They are recorded but, as in forma2, the workspace limit is not enforced.
func defaultLimits(isFormaCloud bool) json.RawMessage {
	limits := map[string]any{"workspaces": 3, "monthly": map[string]any{"responses": 1500, "workflowRuns": nil}}
	if isFormaCloud {
		limits = map[string]any{"workspaces": 1, "monthly": map[string]any{"responses": 250, "workflowRuns": nil}}
	}
	b, _ := json.Marshal(limits)
	return b
}

// Membership is one user's place in one organization.
type Membership struct {
	OrganizationID uuid.UUID `json:"organizationId"`
	UserID         uuid.UUID `json:"userId"`
	Accepted       bool      `json:"accepted"`
	Role           string    `json:"role"`
	// SuspendedAt is when an owner or manager suspended the member's access to the organization, nil while they have it (authz refuses a suspended member everything in the organization but its name, their membership and leaving).
	SuspendedAt *time.Time `json:"suspendedAt"`
}

// auditID is how forma2's audit events named a membership.
func (m Membership) auditID() string {
	return m.UserID.String() + "-" + m.OrganizationID.String()
}

// Member is a membership with its user, forma2's TMember.
type Member struct {
	UserID   uuid.UUID `json:"userId"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Accepted bool      `json:"accepted"`
	Role     string    `json:"role"`
	// IsActive is the account's own switch (users.is_active), which only the instance's operator turns off; a suspension is SuspendedAt.
	IsActive bool `json:"isActive"`
	// SuspendedAt is when the member's access to this organization was suspended, nil while they have it.
	SuspendedAt *time.Time `json:"suspendedAt"`
	// TwoFactor is the member's two-factor status (authz.SecondFactorEnabled, SecondFactorDisabled or SecondFactorSSO), shown only to owners and managers, who decide on the requirement and follow it up; it is empty for anyone else.
	TwoFactor string `json:"twoFactor,omitempty"`
}

// UserManagementAllowed is forma2's getUserManagementAccess: whether a role may manage members and invites under USER_MANAGEMENT_MINIMUM_ROLE. It shapes the SPA's pages and bounds what an organization API key may do with users and teams (the key acts with its creator's role); the session endpoints decide through organization.manage_access, which the authorizer maps the same way.
func UserManagementAllowed(role, minimumRole string) bool {
	switch minimumRole {
	case "owner":
		return role == RoleOwner
	case "manager":
		return role == RoleOwner || role == RoleManager
	default:
		return false
	}
}
