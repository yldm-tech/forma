package analytics_test

import (
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics/analyticstest"
)

func TestNothingIsSentWithoutAClient(t *testing.T) {
	if analytics.Enabled() {
		t.Fatal("a client is installed without POSTHOG_KEY")
	}
	analytics.Capture("user-1", "survey_created", analytics.Properties{"survey_id": "s"}, analytics.Groups{})
	analytics.Identify("user-1", nil)
	analytics.GroupIdentify(analytics.GroupOrganization, "org-1", nil)
}

func TestMessagesCarryForma2sShape(t *testing.T) {
	rec := analyticstest.Start(t)
	if !analytics.Enabled() {
		t.Fatal("Enabled() = false with a client installed")
	}
	analytics.Capture("user-1", "survey_created", analytics.Properties{"survey_id": "s-1"}, analytics.Groups{OrganizationID: "org-1", WorkspaceID: "ws-1"})
	analytics.Capture("org-1", "survey_response_received", nil, analytics.Groups{OrganizationID: "org-1"})
	analytics.Identify("user-1", analytics.Properties{"email_domain": "example.com"})
	analytics.GroupIdentify(analytics.GroupWorkspace, "ws-1", analytics.Properties{"name": "Website"})

	created := rec.Events(t, "survey_created", 1)[0]
	if created.DistinctID != "user-1" || created.Properties["survey_id"] != "s-1" || created.Properties["source"] != "server" || created.Properties["$lib"] != "posthog-node" {
		t.Errorf("survey_created = %+v", created)
	}
	if groups, _ := created.Properties["$groups"].(map[string]any); groups["organization"] != "org-1" || groups["workspace"] != "ws-1" {
		t.Errorf("groups = %v, want the organization and the workspace", created.Properties["$groups"])
	}
	received := rec.Events(t, "survey_response_received", 1)[0]
	if groups, _ := received.Properties["$groups"].(map[string]any); len(groups) != 1 || groups["organization"] != "org-1" {
		t.Errorf("groups = %v, want only the organization", received.Properties["$groups"])
	}
	if identify := rec.Events(t, "$identify", 1)[0]; identify.DistinctID != "user-1" || identify.Set["email_domain"] != "example.com" {
		t.Errorf("$identify = %+v", identify)
	}
	group := rec.Events(t, "$groupidentify", 1)[0]
	if group.Properties["$group_type"] != "workspace" || group.Properties["$group_key"] != "ws-1" {
		t.Errorf("$groupidentify = %+v", group)
	}
	if set, _ := group.Properties["$group_set"].(map[string]any); set["name"] != "Website" {
		t.Errorf("$group_set = %v", group.Properties["$group_set"])
	}
}

func TestEmailDomain(t *testing.T) {
	for email, want := range map[string]string{"Jane@Example.COM": "example.com", "no-at-sign": "", "a@": ""} {
		if got := analytics.EmailDomain(email); got != want {
			t.Errorf("EmailDomain(%q) = %q, want %q", email, got, want)
		}
	}
}

func TestFeatureFlagsDefaultToFalse(t *testing.T) {
	if v := analytics.FeatureFlag("org-1", "a-b_billing_shorten-trial-days", analytics.Groups{OrganizationID: "org-1"}); v != false {
		t.Errorf("flag without PostHog = %v, want false", v)
	}
	// The test endpoint answers flag requests with an error, as an unreachable PostHog would.
	analyticstest.Start(t)
	if v := analytics.FeatureFlag("org-1", "a-b_billing_shorten-trial-days", analytics.Groups{OrganizationID: "org-1"}); v != false {
		t.Errorf("flag PostHog could not answer = %v, want false", v)
	}
}
