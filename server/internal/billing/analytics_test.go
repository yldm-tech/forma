package billing

import "testing"

func TestLifecycleEvent(t *testing.T) {
	s := func(plan, status string) Snapshot {
		snap := Snapshot{Plan: plan}
		if status != "" {
			snap.SubscriptionStatus = &status
		}
		return snap
	}
	for _, tc := range []struct {
		name          string
		before, after Snapshot
		subscription  bool
		want          string
	}{
		{"hobby to an active Pro starts a paid subscription", s(PlanHobby, "active"), s(PlanPro, "active"), true, "subscription_started"},
		{"a trial is not a paid start", s(PlanHobby, "active"), s(PlanPro, "trialing"), true, ""},
		{"a trial converting starts a paid subscription", s(PlanPro, "trialing"), s(PlanPro, "active"), true, "subscription_started"},
		{"recovering from dunning is not a new start", s(PlanPro, "past_due"), s(PlanPro, "active"), true, ""},
		{"Pro to Scale is an update", s(PlanPro, "active"), s(PlanScale, "active"), true, "subscription_updated"},
		{"an unchanged paid plan is nothing", s(PlanScale, "active"), s(PlanScale, "active"), true, ""},
		{"a canceled paid subscription", s(PlanPro, "active"), s(PlanPro, "canceled"), true, "subscription_canceled"},
		{"a paid subscription that disappeared", s(PlanScale, "active"), s(PlanHobby, ""), false, "subscription_canceled"},
		{"dropping to Hobby out of dunning", s(PlanPro, "unpaid"), s(PlanHobby, "active"), true, "subscription_canceled"},
		{"a canceled Hobby is nothing", s(PlanHobby, "active"), s(PlanHobby, "canceled"), true, ""},
	} {
		if got := lifecycleEvent(tc.before, tc.after, tc.subscription); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
