package ai

import "testing"

// The SPA is told what can make AI work: the plan first, then an unconfigured instance, which no toggle fixes, then the organization's toggle.
func TestAvailabilityNamesTheInstanceBeforeTheToggle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  OrganizationConfig
		want Availability
	}{
		{"available", OrganizationConfig{IsAISmartToolsEnabled: true, IsAISmartToolsEntitled: true, IsInstanceConfigured: true}, Availability{Available: true}},
		{"not in the plan", OrganizationConfig{IsInstanceConfigured: false}, Availability{Reason: ReasonNotInPlan}},
		{"toggle off on an unconfigured instance", OrganizationConfig{IsAISmartToolsEntitled: true}, Availability{Reason: ReasonInstanceNotConfigured}},
		{"toggle on, unconfigured instance", OrganizationConfig{IsAISmartToolsEntitled: true, IsAISmartToolsEnabled: true}, Availability{Reason: ReasonInstanceNotConfigured}},
		{"toggle off on a configured instance", OrganizationConfig{IsAISmartToolsEntitled: true, IsInstanceConfigured: true}, Availability{Reason: ReasonNotEnabled}},
	}
	for _, tc := range cases {
		if got := availability(&tc.cfg); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	// Generation keeps forma2's order, which the v3 API reproduces.
	if r := (OrganizationConfig{IsAISmartToolsEntitled: true}).UnavailableReason(); r != ReasonNotEnabled {
		t.Errorf("UnavailableReason %q", r)
	}
}
