// Package billing is forma2's Forma Cloud billing (apps/web/modules/billing): the Stripe customer, subscriptions and entitlements of every organization, mirrored into organization_billing; the price catalog read from Stripe's metadata; checkout, setup checkout, customer portal, trial and plan changes; the Stripe webhook at /api/billing/stripe-webhook; and the usage meter events Stripe bills overage from.
//
// Billing runs only on Forma Cloud (IS_FORMA_CLOUD=1) with STRIPE_SECRET_KEY set. Everywhere else every operation is a no-op, the billing page is a 404 and the organization keeps the default limits it was created with. As in forma2, no limit is enforced: the response and workspace limits are shown, and responses and workflow runs past the included volume are billed by Stripe from the meter events.
package billing

import (
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
)

// Plans are forma2's TCloudBillingPlan: the three standard plans, a custom contract, and a subscription whose product carries no recognised forma_plan.
const (
	PlanHobby   = "hobby"
	PlanPro     = "pro"
	PlanScale   = "scale"
	PlanCustom  = "custom"
	PlanUnknown = "unknown"
)

// Intervals are forma2's TCloudBillingInterval.
const (
	IntervalMonthly = "monthly"
	IntervalYearly  = "yearly"
)

// planLevel is forma2's CLOUD_PLAN_LEVEL: the order plans upgrade in.
var planLevel = map[string]int{PlanHobby: 0, PlanPro: 1, PlanScale: 2, PlanCustom: 3, PlanUnknown: -1}

// activeStatuses are the subscription statuses forma2 counted as a live subscription (ACTIVE_SUBSCRIPTION_STATUSES).
var activeStatuses = map[string]bool{"trialing": true, "active": true, "past_due": true, "unpaid": true, "paused": true}

// Feature lookup keys of the Stripe entitlements forma2 read (CLOUD_STRIPE_FEATURE_LOOKUP_KEYS). They are stored in the snapshot's features; only the AI smart tools key is ever written by Forma itself, optimistically after a trial starts.
const (
	FeatureAISmartTools = "ai-smart-tools"
	FeatureWorkflows    = "workflows"
)

// Error codes the billing page maps to its messages, forma2's OperationNotAllowedError messages.
const (
	CodePaidCheckoutRequiresNoSubscription = "paid_checkout_requires_no_existing_subscription"
	CodePaymentMethodRequired              = "payment_method_required"
	CodeCardAuthenticationRequired         = "card_authentication_required"
	CodeMixedIntervalCheckout              = "mixed_interval_checkout_unsupported"
	CodeTrialAlreadyUsed                   = "trial_already_used"
	CodeCheckoutSessionMismatch            = "checkout_session_mismatch"
)

// Limits is forma2's TOrganizationBillingPlanLimits; nil means unlimited.
type Limits struct {
	Workspaces *int64        `json:"workspaces"`
	Monthly    MonthlyLimits `json:"monthly"`
}

// MonthlyLimits are the volumes included in one usage cycle.
type MonthlyLimits struct {
	Responses    *int64 `json:"responses"`
	WorkflowRuns *int64 `json:"workflowRuns"`
}

// DefaultLimits is forma2's getDefaultOrganizationBilling: one workspace and 250 responses a month on Forma Cloud, three and 1500 self-hosted, and no included workflow runs.
func DefaultLimits(isFormaCloud bool) Limits {
	if isFormaCloud {
		return Limits{Workspaces: ptr[int64](1), Monthly: MonthlyLimits{Responses: ptr[int64](250)}}
	}
	return Limits{Workspaces: ptr[int64](3), Monthly: MonthlyLimits{Responses: ptr[int64](1500)}}
}

// Snapshot is the organization's Stripe state as the last sync saw it, forma2's TOrganizationStripeBilling stored in organization_billing.stripe. Timestamps are ISO strings, as forma2 wrote them.
type Snapshot struct {
	Plan                     string               `json:"plan,omitempty"`
	Interval                 *string              `json:"interval"`
	SubscriptionStatus       *string              `json:"subscriptionStatus"`
	SubscriptionID           *string              `json:"subscriptionId"`
	HasPaymentMethod         *bool                `json:"hasPaymentMethod,omitempty"`
	Features                 []string             `json:"features,omitempty"`
	LastStripeEventCreatedAt *string              `json:"lastStripeEventCreatedAt"`
	LastSyncedAt             *string              `json:"lastSyncedAt"`
	LastSyncedEventID        *string              `json:"lastSyncedEventId"`
	TrialEnd                 *string              `json:"trialEnd"`
	PendingChange            *PendingChange       `json:"pendingChange"`
	PaymentAttemptError      *PaymentAttemptError `json:"paymentAttemptError"`
}

// PendingChange is a plan change Stripe will apply at a later date: a scheduled downgrade, or a cancellation at period end that drops the organization to Hobby.
type PendingChange struct {
	Type           string  `json:"type"`
	TargetPlan     string  `json:"targetPlan"`
	TargetInterval *string `json:"targetInterval"`
	EffectiveAt    string  `json:"effectiveAt"`
}

// PaymentAttemptError is the payment-failure banner of the billing page.
type PaymentAttemptError struct {
	Type            string `json:"type"`
	PaymentIntentID string `json:"paymentIntentId"`
	Message         string `json:"message"`
	CreatedAt       string `json:"createdAt"`
}

// Payment attempt error types.
const (
	PaymentErrorRequiresAction = "requires_action"
	PaymentErrorFailedInvoice  = "failed_invoice"
)

// Record is an organization_billing row, forma2's TOrganizationBilling.
type Record struct {
	OrganizationID   uuid.UUID  `json:"-"`
	StripeCustomerID *string    `json:"stripeCustomerId"`
	Limits           Limits     `json:"limits"`
	UsageCycleAnchor *time.Time `json:"usageCycleAnchor"`
	Stripe           *Snapshot  `json:"stripe,omitempty"`
}

// plan is the snapshot's plan, "unknown" when nothing was synced yet (forma2's resolveCurrentCloudPlan).
func (r *Record) plan() string {
	if r.Stripe == nil || r.Stripe.Plan == "" {
		return PlanUnknown
	}
	return r.Stripe.Plan
}

func ptr[T any](v T) *T { return &v }

// parseISOTime reads a snapshot timestamp; a missing or malformed one is nil, as forma2's getDateFromBilling.
func parseISOTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		return nil
	}
	return &t
}

// isoUnix formats a Stripe timestamp (seconds) as forma2's toISOString did.
func isoUnix(seconds int64) string {
	return jsvalue.ISOString(time.Unix(seconds, 0))
}
