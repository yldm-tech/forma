package billing

import (
	"context"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
)

func isPaidPlan(plan string) bool { return plan == PlanPro || plan == PlanScale }

// planOrNil is a snapshot's plan, null when none was synced.
func planOrNil(plan string) any {
	if plan == "" {
		return nil
	}
	return plan
}

func status(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// lifecycleEvent is forma2's resolveSubscriptionLifecycleTransition: the paid-subscription event a sync's change from before to after amounts to, empty for none. A subscription recovering from dunning is not a new start; an ended one (none, canceled, or back on Hobby) is a cancellation only if it was paid; a change between paid plans of an active subscription is an update.
func lifecycleEvent(before, after Snapshot, hasSubscription bool) string {
	wasPaidActive := status(before.SubscriptionStatus) == "active" && isPaidPlan(before.Plan)
	isPaidActive := status(after.SubscriptionStatus) == "active" && isPaidPlan(after.Plan)
	dunning := map[string]bool{"past_due": true, "unpaid": true, "paused": true}[status(before.SubscriptionStatus)]
	wasPaidRecoverable := isPaidPlan(before.Plan) && (status(before.SubscriptionStatus) == "active" || dunning)
	ended := !hasSubscription || status(after.SubscriptionStatus) == "canceled" || after.Plan == PlanHobby
	switch {
	case wasPaidActive && isPaidActive && before.Plan != after.Plan:
		return "subscription_updated"
	case isPaidActive && !wasPaidActive && !dunning:
		return "subscription_started"
	case wasPaidRecoverable && ended:
		return "subscription_canceled"
	}
	return ""
}

// captureSync keeps the organization group's plan facts current on every sync and sends the subscription lifecycle event to the organization's owner, forma2's emitSubscriptionLifecycleEvent. The snapshot is already written, so a failure here is logged and changes nothing.
func (e *Engine) captureSync(ctx context.Context, organizationID uuid.UUID, before, after Snapshot, hasSubscription bool) {
	if !analytics.Enabled() {
		return
	}
	org := organizationID.String()
	analytics.GroupIdentify(analytics.GroupOrganization, org, analytics.Properties{
		"plan":                after.Plan,
		"billing_interval":    after.Interval,
		"subscription_status": after.SubscriptionStatus,
		"has_payment_method":  after.HasPaymentMethod != nil && *after.HasPaymentMethod,
	})
	event := lifecycleEvent(before, after, hasSubscription)
	if event == "" {
		return
	}
	owner, err := e.repo.owner(ctx, organizationID)
	if err != nil || owner == nil {
		if err != nil {
			e.log.ErrorContext(ctx, "Failed to emit subscription lifecycle event to PostHog", "organizationId", org, "err", err)
		}
		return
	}
	props := analytics.Properties{"plan": after.Plan, "interval": after.Interval, "organization_id": org}
	switch event {
	case "subscription_updated":
		props["previous_plan"] = planOrNil(before.Plan)
	case "subscription_canceled":
		// The new snapshot has already dropped to Hobby or none, so the cancellation reports the plan it ended.
		props["plan"], props["interval"] = planOrNil(before.Plan), before.Interval
	}
	analytics.Capture(owner.ID.String(), event, props, analytics.Groups{OrganizationID: org})
}
