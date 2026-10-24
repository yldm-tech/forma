package org

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func orgPath(id uuid.UUID, rest string) string {
	return "/api/app/organizations/" + id.String() + rest
}

func TestCreateOrganization(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	e.exec(`UPDATE users SET notification_settings = '{"alert": {"s1": true}, "weeklySummary": {"w": true}}' WHERE id = ?`, owner)

	c := e.organization(owner, "  Acme  ")
	if c.Name != "Acme" {
		t.Fatalf("name %q, want it trimmed", c.Name)
	}
	if c.WorkspaceID == uuid.Nil {
		t.Fatal("no default workspace id")
	}
	var limits map[string]any
	_ = json.Unmarshal(c.Billing.Limits, &limits)
	if limits["workspaces"] != float64(3) || limits["monthly"].(map[string]any)["responses"] != float64(1500) || c.Billing.StripeCustomerID != nil {
		t.Fatalf("self-hosted billing defaults: %s", c.Billing.Limits)
	}
	if e.count(`SELECT count(*) FROM memberships WHERE organization_id = ? AND user_id = ? AND role = 'owner' AND accepted`, c.ID, owner) != 1 {
		t.Fatal("creator is not the accepted owner")
	}
	if e.count(`SELECT count(*) FROM organization_billing WHERE organization_id = ?`, c.ID) != 1 {
		t.Fatal("no billing row")
	}
	var wsName string
	e.scalar(&wsName, `SELECT name FROM workspaces WHERE id = ? AND organization_id = ?`, c.WorkspaceID, c.ID)
	if wsName != "My workspace" {
		t.Fatalf("default workspace %q", wsName)
	}
	if e.count(`SELECT count(*) FROM contact_attribute_keys WHERE workspace_id = ? AND type = 'default'`, c.WorkspaceID) != 5 ||
		e.count(`SELECT count(*) FROM contact_attribute_keys WHERE workspace_id = ? AND is_unique`, c.WorkspaceID) != 2 ||
		e.count(`SELECT count(*) FROM languages WHERE workspace_id = ? AND code = 'en-US' AND alias IS NULL`, c.WorkspaceID) != 1 {
		t.Fatal("default workspace seeds missing")
	}

	// A second organization is opted out too, once each, and the other notification settings survive.
	second := e.organization(owner, "Beta")
	var settings string
	e.scalar(&settings, `SELECT notification_settings::text FROM users WHERE id = ?`, owner)
	var ns struct {
		Alert         map[string]bool `json:"alert"`
		Unsubscribed  []string        `json:"unsubscribedOrganizationIds"`
		WeeklySummary map[string]bool `json:"weeklySummary"`
	}
	_ = json.Unmarshal([]byte(settings), &ns)
	if !slices.Equal(ns.Unsubscribed, []string{c.ID.String(), second.ID.String()}) || !ns.Alert["s1"] || !ns.WeeklySummary["w"] {
		t.Fatalf("notification settings %s", settings)
	}
	if err := e.svc.repo.unsubscribeAlerts(t.Context(), owner, c.ID); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT jsonb_array_length(notification_settings -> 'unsubscribedOrganizationIds') FROM users WHERE id = ?`, owner) != 2 {
		t.Fatal("unsubscribing twice duplicated the organization")
	}

	events := e.auditEvents()
	if len(events) == 0 || events[0]["action"] != "created" || events[0]["target"].(map[string]any)["type"] != "organization" || events[0]["organizationId"] != c.ID.String() {
		t.Fatalf("audit events %v", events)
	}

	for _, name := range []string{"", "   "} {
		e.fails(e.do(owner, http.MethodPost, "/api/app/organizations", map[string]string{"name": name}), http.StatusBadRequest, apperr.CodeValidation, "")
	}
	e.fails(e.do(uuid.Nil, http.MethodPost, "/api/app/organizations", map[string]string{"name": "X"}), http.StatusUnauthorized, apperr.CodeNotAuthenticated, "")
}

func TestCreateOrganizationOnCloudUsesCloudBilling(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	c := e.organization(e.user(), "Acme")
	var limits map[string]any
	_ = json.Unmarshal(c.Billing.Limits, &limits)
	if limits["workspaces"] != float64(1) || limits["monthly"].(map[string]any)["responses"] != float64(250) {
		t.Fatalf("cloud billing defaults: %s", c.Billing.Limits)
	}
}

// On Forma Cloud with Stripe, a new organization gets its Stripe customer and Hobby subscription and a deleted one has its subscriptions cancelled, forma2's ensureCloudStripeSetupForOrganization and cleanupStripeCustomer, both as jobs for `forma worker`.
func TestOrganizationBillingJobs(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud, c.Stripe.SecretKey = true, "sk_test_123" })
	owner := e.user()
	c := e.organization(owner, "Acme")
	if e.count(`SELECT count(*) FROM river_job WHERE kind = 'billing.setup-organization' AND args ->> 'organizationId' = ?`, c.ID.String()) != 1 {
		t.Fatal("the billing setup was not queued")
	}
	e.exec(`UPDATE organization_billing SET stripe_customer_id = 'cus_123' WHERE organization_id = ?`, c.ID)
	e.ok(e.do(owner, http.MethodDelete, orgPath(c.ID, ""), nil), http.StatusNoContent, nil)
	if e.count(`SELECT count(*) FROM river_job WHERE kind = 'billing.cleanup-customer' AND args ->> 'stripeCustomerId' = 'cus_123'`) != 1 {
		t.Fatal("the Stripe cleanup was not queued")
	}
}

func TestGetOrganization(t *testing.T) {
	e := newEnv(t)
	owner, manager, billing, outsider := e.user(), e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, manager, RoleManager)
	e.member(c.ID, billing, RoleBilling)

	var d Detail
	e.ok(e.do(owner, http.MethodGet, orgPath(c.ID, ""), nil), http.StatusOK, &d)
	if d.Name != "Acme" || d.Membership.Role != RoleOwner || !d.Membership.Accepted || !d.UserManagementAllowed || d.DisplayTimeZone != nil {
		t.Fatalf("owner view %+v", d)
	}
	e.ok(e.do(billing, http.MethodGet, orgPath(c.ID, ""), nil), http.StatusOK, &d)
	if d.Membership.Role != RoleBilling || d.UserManagementAllowed {
		t.Fatalf("billing view %+v", d)
	}
	// A non-member and a missing organization are both not found.
	e.fails(e.do(outsider, http.MethodGet, orgPath(c.ID, ""), nil), http.StatusNotFound, apperr.CodeNotFound, "Organization not found")
	e.fails(e.do(owner, http.MethodGet, orgPath(uuid.New(), ""), nil), http.StatusNotFound, apperr.CodeNotFound, "Organization not found")
	e.fails(e.do(owner, http.MethodGet, "/api/app/organizations/not-a-uuid", nil), http.StatusBadRequest, apperr.CodeValidation, "")
}

func TestListOrganizations(t *testing.T) {
	e := newEnv(t)
	ada, bob := e.user(), e.user()
	first := e.organization(ada, "First")
	second := e.organization(bob, "Second")
	e.member(second.ID, ada, RoleMember)
	e.organization(bob, "Bob only")

	var res listResponse
	e.ok(e.do(ada, http.MethodGet, "/api/app/organizations", nil), http.StatusOK, &res)
	if len(res.Organizations) != 2 || res.Organizations[0].ID != first.ID || res.Organizations[0].Role != RoleOwner || res.Organizations[1].ID != second.ID || res.Organizations[1].Role != RoleMember {
		t.Fatalf("organizations %+v", res.Organizations)
	}
	e.ok(e.do(ada, http.MethodGet, "/api/app/organizations?page=2", nil), http.StatusOK, &res)
	if len(res.Organizations) != 0 {
		t.Fatalf("page 2 holds %d organizations", len(res.Organizations))
	}
	e.fails(e.do(ada, http.MethodGet, "/api/app/organizations?page=-1", nil), http.StatusBadRequest, apperr.CodeValidation, "")
}

func TestUpdateOrganization(t *testing.T) {
	e := newEnv(t)
	owner, manager, member, outsider := e.user(), e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, manager, RoleManager)
	e.member(c.ID, member, RoleMember)
	path := orgPath(c.ID, "")

	var o Organization
	e.ok(e.do(owner, http.MethodPatch, path, map[string]any{"name": " Renamed "}), http.StatusOK, &o)
	if o.Name != "Renamed" {
		t.Fatalf("name %q", o.Name)
	}
	// Renaming is the owner's; the time zone is the owner's and the manager's.
	e.fails(e.do(manager, http.MethodPatch, path, map[string]any{"name": "Mine"}), http.StatusForbidden, apperr.CodeForbidden, "Not authorized")
	e.ok(e.do(manager, http.MethodPatch, path, map[string]any{"displayTimeZone": "Europe/Berlin"}), http.StatusOK, &o)
	if o.DisplayTimeZone == nil || *o.DisplayTimeZone != "Europe/Berlin" || o.Name != "Renamed" {
		t.Fatalf("time zone update %+v", o)
	}
	e.ok(e.do(manager, http.MethodPatch, path, map[string]any{"displayTimeZone": nil}), http.StatusOK, &o)
	if o.DisplayTimeZone != nil {
		t.Fatal("null did not reset the time zone to UTC")
	}
	e.fails(e.do(member, http.MethodPatch, path, map[string]any{"displayTimeZone": "Europe/Berlin"}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(outsider, http.MethodPatch, path, map[string]any{"displayTimeZone": "Europe/Berlin"}), http.StatusForbidden, apperr.CodeForbidden, "")
	for _, tz := range []string{"Mars/Olympus", "", "Local"} {
		e.fails(e.do(owner, http.MethodPatch, path, map[string]any{"displayTimeZone": tz}), http.StatusBadRequest, apperr.CodeValidation, "")
	}
	e.fails(e.do(owner, http.MethodPatch, path, map[string]any{"name": ""}), http.StatusBadRequest, apperr.CodeValidation, "")

	// No AI provider is configured on this instance: enabling is refused, disabling always works.
	e.fails(e.do(manager, http.MethodPatch, path, map[string]any{"isAISmartToolsEnabled": true}), http.StatusForbidden, CodeAIInstanceNotConfigured, "")
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, c.ID)
	e.ok(e.do(manager, http.MethodPatch, path, map[string]any{"isAISmartToolsEnabled": false}), http.StatusOK, &o)
	if o.IsAISmartToolsEnabled {
		t.Fatal("AI smart tools still enabled")
	}
	e.fails(e.do(member, http.MethodPatch, path, map[string]any{"isAISmartToolsEnabled": false}), http.StatusForbidden, apperr.CodeForbidden, "")
}

// forma2's assertOrganizationAISettingsUpdateAllowed: with an AI provider configured, owners and managers switch AI smart tools on and off.
func TestUpdateOrganizationAISmartToolsOnConfiguredInstance(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.AI = config.AIConfig{Provider: config.AIProviderOpenAICompatible, Model: "qwen", OpenAICompatibleBaseURL: "http://vllm:8000/v1"}
	})
	owner, manager, member := e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, manager, RoleManager)
	e.member(c.ID, member, RoleMember)
	path := orgPath(c.ID, "")

	var o Organization
	e.ok(e.do(manager, http.MethodPatch, path, map[string]any{"isAISmartToolsEnabled": true}), http.StatusOK, &o)
	if !o.IsAISmartToolsEnabled {
		t.Fatal("AI smart tools not enabled")
	}
	e.fails(e.do(member, http.MethodPatch, path, map[string]any{"isAISmartToolsEnabled": false}), http.StatusForbidden, apperr.CodeForbidden, "")
	e.ok(e.do(owner, http.MethodPatch, path, map[string]any{"isAISmartToolsEnabled": false}), http.StatusOK, &o)
	if o.IsAISmartToolsEnabled {
		t.Fatal("AI smart tools still enabled")
	}
}

func TestDeleteOrganization(t *testing.T) {
	e := newEnv(t)
	owner, manager := e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, manager, RoleManager)

	e.fails(e.do(manager, http.MethodDelete, orgPath(c.ID, ""), nil), http.StatusForbidden, apperr.CodeForbidden, "")
	e.ok(e.do(owner, http.MethodDelete, orgPath(c.ID, ""), nil), http.StatusNoContent, nil)
	if e.count(`SELECT count(*) FROM workspaces WHERE organization_id = ?`, c.ID) != 0 || e.count(`SELECT count(*) FROM memberships WHERE organization_id = ?`, c.ID) != 0 {
		t.Fatal("organization rows survived")
	}
	// The projection follows: the former owner can no longer read it.
	e.fails(e.do(owner, http.MethodGet, orgPath(c.ID, ""), nil), http.StatusNotFound, apperr.CodeNotFound, "")
	e.fails(e.do(owner, http.MethodGet, "/api/app/workspaces/"+c.WorkspaceID.String(), nil), http.StatusForbidden, apperr.CodeForbidden, "")
	// Nothing stops a user from deleting their only organization; the root page sends them to create one.
	var home Home
	e.ok(e.do(owner, http.MethodGet, "/api/app/home", nil), http.StatusOK, &home)
	if home.Path != "/setup/organization/create" {
		t.Fatalf("home %+v", home)
	}
}

func TestValidTimeZone(t *testing.T) {
	cases := map[string]bool{"UTC": true, "Europe/Berlin": true, "America/Argentina/Buenos_Aires": true, "Etc/GMT+5": true, "": false, "Local": false, "Nowhere/City": false, "../etc/passwd": false}
	for tz, want := range cases {
		if got := ValidTimeZone(tz); got != want {
			t.Errorf("ValidTimeZone(%q) = %v, want %v", tz, got, want)
		}
	}
}

func TestUserManagementAllowed(t *testing.T) {
	cases := []struct {
		minimum string
		allowed []string
	}{
		{"manager", []string{RoleOwner, RoleManager}},
		{"owner", []string{RoleOwner}},
		{"disabled", nil},
	}
	for _, tc := range cases {
		for _, role := range []string{RoleOwner, RoleManager, RoleMember, RoleBilling} {
			if got := UserManagementAllowed(role, tc.minimum); got != slices.Contains(tc.allowed, role) {
				t.Errorf("UserManagementAllowed(%s, %s) = %v", role, tc.minimum, got)
			}
		}
	}
}
