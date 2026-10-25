package billing

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v84"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
)

// Trial lengths, forma2's DEFAULT_PRO_TRIAL_DAYS. forma2 shortened it to seven days for a PostHog experiment's test variant; without the experiment every trial is the default.
const proTrialDays = 14

// shortenedProTrialDays is the trial of forma2's a-b_billing_shorten-trial-days experiment's "test" variant.
const shortenedProTrialDays = 7

// trialDays is forma2's getProTrialDays: the Pro trial's length, shortened for the organizations PostHog puts in the experiment's test variant.
func trialDays(organizationID uuid.UUID) int64 {
	if analytics.FeatureFlag(organizationID.String(), "a-b_billing_shorten-trial-days", analytics.Groups{OrganizationID: organizationID.String()}) == "test" {
		return shortenedProTrialDays
	}
	return proTrialDays
}

// isPlanComparison is forma2's billing page flag: the users PostHog puts in the a-b_billing_plan-comparison-table experiment's "test" variant see the plan comparison table. forma2 evaluated it for the user alone, without groups.
func isPlanComparison(userID uuid.UUID) bool {
	return analytics.FeatureFlag(userID.String(), "a-b_billing_plan-comparison-table", analytics.Groups{}) == "test"
}

func errNotFound(resource string, organizationID uuid.UUID) *apperr.Error {
	return apperr.NotFound(resource, organizationID.String())
}

// errNotAllowed is forma2's OperationNotAllowedError with a code the billing page maps to its message.
func errNotAllowed(code string) *apperr.Error {
	return &apperr.Error{Status: http.StatusForbidden, Code: code, Message: code}
}

// billingURL is where Stripe sends the browser back to, the organization's billing settings.
func (e *Engine) billingURL(organizationID uuid.UUID) string {
	return e.cfg.AppURL().JoinPath("organizations", organizationID.String(), "settings", "billing").String()
}

// PortalURL is forma2's createCustomerPortalSession: a Stripe customer portal session returning to the billing page.
func (e *Engine) PortalURL(ctx context.Context, organizationID uuid.UUID, customerID string) (string, error) {
	if !e.stripe.Configured() {
		return "", errStripeNotConfigured
	}
	session, err := e.api().V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{Customer: stripe.String(customerID), ReturnURL: stripe.String(e.billingURL(organizationID))})
	if err != nil {
		return "", fmt.Errorf("create Stripe customer portal session: %w", err)
	}
	return session.URL, nil
}

// checkoutCustomerSettings are the address, tax id and name collection forma2's checkouts shared.
func checkoutCustomerSettings(p *stripe.CheckoutSessionCreateParams) {
	p.BillingAddressCollection = stripe.String("required")
	p.TaxIDCollection = &stripe.CheckoutSessionCreateTaxIDCollectionParams{Enabled: stripe.Bool(true), Required: stripe.String("if_supported")}
	p.CustomerUpdate = &stripe.CheckoutSessionCreateCustomerUpdateParams{Address: stripe.String("auto"), Name: stripe.String("auto")}
}

// PlanCheckoutURL is forma2's createPaidPlanCheckoutSession: a subscription Checkout for an organization without a subscription. A plan whose prices bill on different intervals cannot be bought in one Checkout. The success page carries the plan so it can wait for the webhook's sync.
func (e *Engine) PlanCheckoutURL(ctx context.Context, organizationID uuid.UUID, customerID, plan, interval string) (string, error) {
	catalog, err := e.stripe.Catalog(ctx)
	if err != nil {
		return "", err
	}
	item, err := catalog.Item(plan, interval)
	if err != nil {
		return "", err
	}
	intervals := map[stripe.PriceRecurringInterval]bool{}
	for _, p := range []*stripe.Price{item.BasePrice, item.ResponsePrice, item.WorkflowRunsPrice} {
		if p != nil && p.Recurring != nil {
			intervals[p.Recurring.Interval] = true
		}
	}
	if len(intervals) > 1 {
		return "", errNotAllowed(CodeMixedIntervalCheckout)
	}
	lines := make([]*stripe.CheckoutSessionCreateLineItemParams, 0, 3)
	for _, it := range item.lineItems() {
		lines = append(lines, &stripe.CheckoutSessionCreateLineItemParams{Price: stripe.String(it.Price), Quantity: it.Quantity})
	}
	success := e.cfg.AppURL().JoinPath("billing-confirmation")
	success.RawQuery = "organizationId=" + organizationID.String() + "&checkout_success=1&plan=" + plan
	params := &stripe.CheckoutSessionCreateParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)), Customer: stripe.String(customerID), LineItems: lines,
		ClientReferenceID: stripe.String(organizationID.String()), SuccessURL: stripe.String(success.String()), CancelURL: stripe.String(e.billingURL(organizationID)),
		Metadata:         map[string]string{"organizationId": organizationID.String(), "targetPlan": plan, "targetInterval": interval},
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{Metadata: map[string]string{"organizationId": organizationID.String()}},
	}
	checkoutCustomerSettings(params)
	session, err := e.api().V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create Stripe checkout session: %w", err)
	}
	if session.URL == "" {
		return "", errors.New("stripe did not return a Checkout Session URL")
	}
	return session.URL, nil
}

// UpgradeIntent is the plan a setup checkout is for, when it is one.
type UpgradeIntent struct {
	Plan     string
	Interval string
}

// SetupCheckoutURL is forma2's createSetupCheckoutSession: a setup-mode Checkout that collects a card, address and tax id for the existing subscription without creating another. With an upgrade intent the success URL carries the session id, so the billing page can finish the upgrade on-session.
func (e *Engine) SetupCheckoutURL(ctx context.Context, organizationID uuid.UUID, customerID, subscriptionID string, intent *UpgradeIntent) (string, error) {
	if !e.stripe.Configured() {
		return "", errStripeNotConfigured
	}
	sub, err := e.api().V1Subscriptions.Retrieve(ctx, subscriptionID, nil)
	if err != nil {
		return "", fmt.Errorf("retrieve Stripe subscription: %w", err)
	}
	currency := string(sub.Currency)
	if currency == "" {
		currency = "usd"
	}
	returnURL := e.billingURL(organizationID)
	success := returnURL + "?checkout_success=1"
	metadata := map[string]string{"organizationId": organizationID.String(), "subscriptionId": subscriptionID}
	if intent != nil {
		// Stripe substitutes {CHECKOUT_SESSION_ID} itself.
		success += "&upgrade_pending=1&session_id={CHECKOUT_SESSION_ID}"
		metadata["targetPlan"], metadata["targetInterval"] = intent.Plan, intent.Interval
	}
	params := &stripe.CheckoutSessionCreateParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSetup)), Customer: stripe.String(customerID), Currency: stripe.String(currency),
		SuccessURL: stripe.String(success), CancelURL: stripe.String(returnURL), Metadata: metadata,
	}
	checkoutCustomerSettings(params)
	session, err := e.api().V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create Stripe setup checkout session: %w", err)
	}
	if session.URL == "" {
		return "", errors.New("stripe did not return a Checkout Session URL")
	}
	return session.URL, nil
}

// emailUsedProTrial is forma2's hasEmailUsedProTrial: whether any Stripe customer with this email ever trialled the Pro product.
func (e *Engine) emailUsedProTrial(ctx context.Context, email, proProductID string) (bool, error) {
	params := &stripe.CustomerListParams{Email: stripe.String(email)}
	params.Limit = stripe.Int64(100)
	for c, err := range e.api().V1Customers.List(ctx, params) {
		if err != nil {
			return false, fmt.Errorf("list Stripe customers: %w", err)
		}
		subs, err := e.listSubscriptions(ctx, c.ID, 100, false)
		if err != nil {
			return false, err
		}
		for _, sub := range subs {
			if sub.TrialStart == 0 {
				continue
			}
			for _, item := range subscriptionItems(sub) {
				if item.Price != nil && item.Price.Product != nil && item.Price.Product.ID == proProductID {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// StartProTrial is forma2's createProTrialSubscription: a Pro monthly subscription with a trial that cancels itself when no card was added, once per email address.
func (e *Engine) StartProTrial(ctx context.Context, organizationID uuid.UUID, customerID string, trialDays int64) error {
	catalog, err := e.stripe.Catalog(ctx)
	if err != nil {
		return err
	}
	item, err := catalog.Item(PlanPro, IntervalMonthly)
	if err != nil {
		return err
	}
	customer, err := e.api().V1Customers.Retrieve(ctx, customerID, nil)
	if err != nil {
		return fmt.Errorf("retrieve Stripe customer: %w", err)
	}
	if !customer.Deleted && customer.Email != "" && item.BasePrice.Product != nil {
		used, err := e.emailUsedProTrial(ctx, customer.Email, item.BasePrice.Product.ID)
		if err != nil {
			return err
		}
		if used {
			return errNotAllowed(CodeTrialAlreadyUsed)
		}
	}
	params := &stripe.SubscriptionCreateParams{
		Customer: stripe.String(customerID), Items: createItems(item.lineItems()), TrialPeriodDays: stripe.Int64(trialDays),
		TrialSettings:   &stripe.SubscriptionCreateTrialSettingsParams{EndBehavior: &stripe.SubscriptionCreateTrialSettingsEndBehaviorParams{MissingPaymentMethod: stripe.String("cancel")}},
		PaymentSettings: &stripe.SubscriptionCreatePaymentSettingsParams{SaveDefaultPaymentMethod: stripe.String("on_subscription")},
		Metadata:        map[string]string{"organizationId": organizationID.String()},
	}
	params.SetIdempotencyKey("create-pro-trial-" + organizationID.String())
	if _, err := e.api().V1Subscriptions.Create(ctx, params); err != nil {
		return fmt.Errorf("create Stripe Pro trial: %w", err)
	}
	return nil
}

// PlanChange is the outcome of a plan change, forma2's switchOrganizationToCloudPlan result. ClientSecret is set when the upgrade invoice needs 3D Secure, which the browser confirms with Stripe.js.
type PlanChange struct {
	Mode           string         `json:"mode"`
	PendingChange  *PendingChange `json:"pendingChange"`
	ClientSecret   *string        `json:"clientSecret"`
	RequiresAction bool           `json:"requiresAction"`
}

// Plan change modes.
const (
	ModeImmediate = "immediate"
	ModeScheduled = "scheduled"
)

// SwitchPlan is forma2's switchOrganizationToCloudPlan. The same plan and interval is a no-op, except converting a trial, which ends it and bills now. A trial switched to Hobby goes there at once and free of charge. A trial needs a card before it can become paid. Upgrades, moves off a custom or unrecognised plan, and trial conversions apply immediately; downgrades are scheduled for the end of the current period.
func (e *Engine) SwitchPlan(ctx context.Context, organizationID uuid.UUID, customerID, plan, interval string) (*PlanChange, error) {
	if !e.stripe.Configured() {
		return nil, errStripeNotConfigured
	}
	sub, err := e.requiredSubscription(ctx, organizationID, customerID)
	if err != nil {
		return nil, err
	}
	current := subscriptionPlan(sub)
	nonStandard := current == PlanCustom || current == PlanUnknown
	upgrade := nonStandard || planLevel[plan] > planLevel[current]
	same := current == plan && subscriptionInterval(sub) == interval
	trialing := sub.Status == stripe.SubscriptionStatusTrialing
	conversion := trialing && plan != PlanHobby
	if same && !conversion {
		return &PlanChange{Mode: ModeImmediate}, nil
	}
	if trialing && plan == PlanHobby {
		if err := e.trialToHobby(ctx, organizationID, sub); err != nil {
			return nil, err
		}
		return &PlanChange{Mode: ModeImmediate}, nil
	}
	if trialing {
		hasCard, err := e.hasPaymentMethod(ctx, sub, customerID)
		if err != nil {
			return nil, err
		}
		if !hasCard {
			return nil, errNotAllowed(CodePaymentMethodRequired)
		}
	}
	if upgrade || conversion {
		secret, err := e.updateItemsNow(ctx, sub, plan, interval)
		if err != nil {
			return nil, err
		}
		// Supersedes any pending downgrade, including a trial's cancel-at-period-end without a schedule.
		if err := e.clearPendingState(ctx, organizationID, sub); err != nil {
			return nil, err
		}
		return &PlanChange{Mode: ModeImmediate, ClientSecret: secret, RequiresAction: secret != nil}, nil
	}
	change, err := e.scheduleChange(ctx, organizationID, sub, plan, interval)
	if err != nil {
		return nil, err
	}
	return &PlanChange{Mode: ModeScheduled, PendingChange: change}, nil
}

// targetItems are the line items of the plan to switch to, after deleting every current item.
func (e *Engine) targetItems(ctx context.Context, sub *stripe.Subscription, plan, interval string) ([]*stripe.SubscriptionUpdateItemParams, error) {
	catalog, err := e.stripe.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	item, err := catalog.Item(plan, interval)
	if err != nil {
		return nil, err
	}
	var out []*stripe.SubscriptionUpdateItemParams
	for _, existing := range subscriptionItems(sub) {
		out = append(out, &stripe.SubscriptionUpdateItemParams{ID: stripe.String(existing.ID), Deleted: stripe.Bool(true)})
	}
	for _, it := range item.lineItems() {
		out = append(out, &stripe.SubscriptionUpdateItemParams{Price: stripe.String(it.Price), Quantity: it.Quantity})
	}
	return out, nil
}

// cardAuthenticationCodes are the decline and error codes of a card that needs the cardholder to authenticate, which an off-session update cannot do.
var cardAuthenticationCodes = map[string]bool{"authentication_required": true, "subscription_payment_intent_requires_action": true}

// updateItemsNow is forma2's updateSubscriptionItemsImmediately. A trial ends and switches plan in one update that charges synchronously, so the card is billed once and a declined card blocks the upgrade; a card that needs 3D Secure cannot pass off-session and becomes card_authentication_required. Any other upgrade is a pending update, granted only once its invoice is paid; when that invoice needs 3D Secure its PaymentIntent's client secret is returned for the browser to confirm.
func (e *Engine) updateItemsNow(ctx context.Context, sub *stripe.Subscription, plan, interval string) (*string, error) {
	items, err := e.targetItems(ctx, sub, plan, interval)
	if err != nil {
		return nil, err
	}
	// cancel_at_period_end cannot be part of a pending update.
	if sub.CancelAtPeriodEnd {
		if _, err := e.api().V1Subscriptions.Update(ctx, sub.ID, &stripe.SubscriptionUpdateParams{CancelAtPeriodEnd: stripe.Bool(false)}); err != nil {
			return nil, fmt.Errorf("clear Stripe cancel at period end: %w", err)
		}
	}
	if sub.Status == stripe.SubscriptionStatusTrialing {
		_, err := e.api().V1Subscriptions.Update(ctx, sub.ID, &stripe.SubscriptionUpdateParams{
			Items: items, TrialEndNow: stripe.Bool(true), ProrationBehavior: stripe.String("always_invoice"), PaymentBehavior: stripe.String("error_if_incomplete"),
		})
		if err != nil {
			if se, ok := stripeError(err); ok && (cardAuthenticationCodes[string(se.Code)] || cardAuthenticationCodes[string(se.DeclineCode)]) {
				return nil, errNotAllowed(CodeCardAuthenticationRequired)
			}
			return nil, fmt.Errorf("convert Stripe trial: %w", err)
		}
		return nil, nil
	}
	updated, err := e.api().V1Subscriptions.Update(ctx, sub.ID, &stripe.SubscriptionUpdateParams{
		Items: items, ProrationBehavior: stripe.String("always_invoice"), PaymentBehavior: stripe.String("pending_if_incomplete"),
	})
	if err != nil {
		return nil, fmt.Errorf("upgrade Stripe subscription: %w", err)
	}
	if updated.LatestInvoice == nil || updated.LatestInvoice.ID == "" {
		return nil, nil
	}
	params := &stripe.InvoiceRetrieveParams{}
	params.AddExpand("confirmation_secret")
	invoice, err := e.api().V1Invoices.Retrieve(ctx, updated.LatestInvoice.ID, params)
	if err != nil {
		return nil, fmt.Errorf("retrieve Stripe invoice: %w", err)
	}
	if invoice.ConfirmationSecret == nil || invoice.ConfirmationSecret.ClientSecret == "" {
		return nil, nil
	}
	return ptr(invoice.ConfirmationSecret.ClientSecret), nil
}

// trialToHobby is forma2's switchTrialToHobbyImmediately: the trial ends now on the free Hobby plan with no charge. A stray schedule or cancellation is cleared first so it cannot bring Pro back, and the trial's cancel-without-card behaviour is overridden, since ending it without a card would otherwise cancel the subscription instead of leaving it on Hobby.
func (e *Engine) trialToHobby(ctx context.Context, organizationID uuid.UUID, sub *stripe.Subscription) error {
	if sub.Schedule != nil {
		if err := e.releaseSchedule(ctx, sub.Schedule.ID); err != nil {
			return err
		}
	}
	if sub.CancelAtPeriodEnd {
		if _, err := e.api().V1Subscriptions.Update(ctx, sub.ID, &stripe.SubscriptionUpdateParams{CancelAtPeriodEnd: stripe.Bool(false)}); err != nil {
			return fmt.Errorf("clear Stripe cancel at period end: %w", err)
		}
	}
	items, err := e.targetItems(ctx, sub, PlanHobby, IntervalMonthly)
	if err != nil {
		return err
	}
	if _, err := e.api().V1Subscriptions.Update(ctx, sub.ID, &stripe.SubscriptionUpdateParams{
		Items: items, TrialEndNow: stripe.Bool(true), ProrationBehavior: stripe.String("none"),
		TrialSettings: &stripe.SubscriptionUpdateTrialSettingsParams{EndBehavior: &stripe.SubscriptionUpdateTrialSettingsEndBehaviorParams{MissingPaymentMethod: stripe.String("create_invoice")}},
	}); err != nil {
		return fmt.Errorf("switch Stripe trial to Hobby: %w", err)
	}
	return e.setPendingChange(ctx, organizationID, nil)
}

func (e *Engine) releaseSchedule(ctx context.Context, scheduleID string) error {
	if _, err := e.api().V1SubscriptionSchedules.Release(ctx, scheduleID, &stripe.SubscriptionScheduleReleaseParams{PreserveCancelDate: stripe.Bool(false)}); err != nil {
		return fmt.Errorf("release Stripe subscription schedule: %w", err)
	}
	return nil
}

// clearPendingState is forma2's clearPendingPlanState: the cancellation at period end and the schedule are undone, and the pending change cleared.
func (e *Engine) clearPendingState(ctx context.Context, organizationID uuid.UUID, sub *stripe.Subscription) error {
	if sub.CancelAtPeriodEnd {
		if _, err := e.api().V1Subscriptions.Update(ctx, sub.ID, &stripe.SubscriptionUpdateParams{CancelAtPeriodEnd: stripe.Bool(false)}); err != nil {
			return fmt.Errorf("clear Stripe cancel at period end: %w", err)
		}
	}
	if sub.Schedule != nil {
		if err := e.releaseSchedule(ctx, sub.Schedule.ID); err != nil {
			return err
		}
	}
	return e.setPendingChange(ctx, organizationID, nil)
}

// scheduleItems is forma2's mapSubscriptionItemsToScheduleItems: metered prices carry no quantity.
func scheduleItem(p *stripe.Price, quantity int64) *stripe.SubscriptionScheduleUpdatePhaseItemParams {
	item := &stripe.SubscriptionScheduleUpdatePhaseItemParams{Price: stripe.String(p.ID)}
	if p.Recurring == nil || p.Recurring.UsageType != stripe.PriceRecurringUsageTypeMetered {
		if quantity == 0 {
			quantity = 1
		}
		item.Quantity = stripe.Int64(quantity)
	}
	return item
}

// scheduleChange is forma2's scheduleSubscriptionPlanChange: the subscription's schedule (created from it when it has none) keeps the current items until the current phase ends and switches to the target plan then, releasing the subscription afterwards. When the schedule update fails, a schedule created here is released and a cancellation at period end that was cleared is restored.
func (e *Engine) scheduleChange(ctx context.Context, organizationID uuid.UUID, sub *stripe.Subscription, plan, interval string) (*PendingChange, error) {
	hadCancel := sub.CancelAtPeriodEnd
	if hadCancel {
		if _, err := e.api().V1Subscriptions.Update(ctx, sub.ID, &stripe.SubscriptionUpdateParams{CancelAtPeriodEnd: stripe.Bool(false)}); err != nil {
			return nil, fmt.Errorf("clear Stripe cancel at period end: %w", err)
		}
	}
	var current []*stripe.SubscriptionScheduleUpdatePhaseItemParams
	for _, item := range subscriptionItems(sub) {
		current = append(current, scheduleItem(item.Price, item.Quantity))
	}
	catalog, err := e.stripe.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	target, err := catalog.Item(plan, interval)
	if err != nil {
		return nil, err
	}
	targetItems := []*stripe.SubscriptionScheduleUpdatePhaseItemParams{scheduleItem(target.BasePrice, 1)}
	for _, p := range []*stripe.Price{target.ResponsePrice, target.WorkflowRunsPrice} {
		if p != nil {
			targetItems = append(targetItems, scheduleItem(p, 0))
		}
	}

	var schedule *stripe.SubscriptionSchedule
	created := false
	if sub.Schedule != nil {
		if schedule, err = e.api().V1SubscriptionSchedules.Retrieve(ctx, sub.Schedule.ID, nil); err != nil {
			return nil, fmt.Errorf("retrieve Stripe subscription schedule: %w", err)
		}
	} else {
		// Stripe refuses metadata on a schedule created from a subscription.
		if schedule, err = e.api().V1SubscriptionSchedules.Create(ctx, &stripe.SubscriptionScheduleCreateParams{FromSubscription: stripe.String(sub.ID)}); err != nil {
			return nil, fmt.Errorf("create Stripe subscription schedule: %w", err)
		}
		created = true
	}
	if schedule.CurrentPhase == nil {
		return nil, fmt.Errorf("stripe subscription schedule %s has no current phase", schedule.ID)
	}
	if schedule.CurrentPhase.EndDate == 0 {
		return nil, fmt.Errorf("stripe subscription schedule %s current phase has no end date; cannot schedule a plan change", schedule.ID)
	}
	phase := schedule.CurrentPhase
	updated, err := e.api().V1SubscriptionSchedules.Update(ctx, schedule.ID, &stripe.SubscriptionScheduleUpdateParams{
		EndBehavior: stripe.String("release"), Metadata: map[string]string{"organizationId": organizationID.String()}, ProrationBehavior: stripe.String("none"),
		Phases: []*stripe.SubscriptionScheduleUpdatePhaseParams{
			{StartDate: stripe.Int64(phase.StartDate), EndDate: stripe.Int64(phase.EndDate), Items: current},
			{StartDate: stripe.Int64(phase.EndDate), Items: targetItems, Metadata: map[string]string{"organizationId": organizationID.String(), "targetPlan": plan, "targetInterval": interval}},
		},
	})
	if err != nil {
		e.rollbackSchedule(ctx, organizationID, sub.ID, schedule.ID, created, hadCancel)
		return nil, fmt.Errorf("update Stripe subscription schedule: %w", err)
	}
	next := nextPhase(updated, phase.EndDate)
	if next == nil {
		return nil, fmt.Errorf("stripe subscription schedule %s has no next phase", updated.ID)
	}
	if plan == PlanHobby {
		interval = IntervalMonthly
	}
	change := &PendingChange{Type: "plan_change", TargetPlan: plan, TargetInterval: ptr(interval), EffectiveAt: isoUnix(next.StartDate)}
	if err := e.setPendingChange(ctx, organizationID, change); err != nil {
		return nil, err
	}
	return change, nil
}

// rollbackSchedule is forma2's rollbackFailedPlanChangeScheduleUpdate; its own failures are logged, since the schedule update's error is the one the caller sees.
func (e *Engine) rollbackSchedule(ctx context.Context, organizationID uuid.UUID, subscriptionID, scheduleID string, created, hadCancel bool) {
	if created {
		if err := e.releaseSchedule(ctx, scheduleID); err != nil {
			e.log.ErrorContext(ctx, "Failed to release newly created Stripe schedule after plan change update error", "err", err, "organizationId", organizationID.String(), "scheduleId", scheduleID)
		}
	}
	if !hadCancel {
		return
	}
	if _, err := e.api().V1Subscriptions.Update(ctx, subscriptionID, &stripe.SubscriptionUpdateParams{CancelAtPeriodEnd: stripe.Bool(true)}); err != nil {
		e.log.ErrorContext(ctx, "Failed to restore Stripe cancel_at_period_end after plan change scheduling error", "err", err, "organizationId", organizationID.String(), "subscriptionId", subscriptionID)
	}
}

// ChargePreview is the amount an upgrade would invoice now.
type ChargePreview struct {
	AmountDue int64  `json:"amountDue"`
	Currency  string `json:"currency"`
}

// PreviewUpgrade is forma2's previewImmediateUpgradeCharge: the invoice the immediate upgrade would create, nil when Stripe cannot price it (usage-based lines can make it fail), so the confirmation shows no amount rather than a made-up one. Only a trial sends trial_end: ending it resets the cycle and bills a full period, while on any other subscription it would turn a mid-cycle proration into a full charge.
func (e *Engine) PreviewUpgrade(ctx context.Context, organizationID uuid.UUID, customerID, plan, interval string) (*ChargePreview, error) {
	if !e.stripe.Configured() {
		return nil, nil
	}
	sub, err := e.requiredSubscription(ctx, organizationID, customerID)
	if err != nil {
		return nil, err
	}
	preview, err := e.previewInvoice(ctx, sub, customerID, plan, interval)
	if err != nil {
		e.log.WarnContext(ctx, "Upgrade invoice preview failed; the confirmation modal falls back to amount-less copy", "err", err, "organizationId", organizationID.String(), "targetPlan", plan)
		return nil, nil
	}
	return preview, nil
}

func (e *Engine) previewInvoice(ctx context.Context, sub *stripe.Subscription, customerID, plan, interval string) (*ChargePreview, error) {
	catalog, err := e.stripe.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	item, err := catalog.Item(plan, interval)
	if err != nil {
		return nil, err
	}
	var items []*stripe.InvoiceCreatePreviewSubscriptionDetailsItemParams
	for _, existing := range subscriptionItems(sub) {
		items = append(items, &stripe.InvoiceCreatePreviewSubscriptionDetailsItemParams{ID: stripe.String(existing.ID), Deleted: stripe.Bool(true)})
	}
	for _, it := range item.lineItems() {
		items = append(items, &stripe.InvoiceCreatePreviewSubscriptionDetailsItemParams{Price: stripe.String(it.Price), Quantity: it.Quantity})
	}
	details := &stripe.InvoiceCreatePreviewSubscriptionDetailsParams{Items: items, ProrationBehavior: stripe.String("always_invoice")}
	if sub.Status == stripe.SubscriptionStatusTrialing {
		details.TrialEndNow = stripe.Bool(true)
	}
	invoice, err := e.api().V1Invoices.CreatePreview(ctx, &stripe.InvoiceCreatePreviewParams{Customer: stripe.String(customerID), Subscription: stripe.String(sub.ID), SubscriptionDetails: details})
	if err != nil {
		return nil, err
	}
	return &ChargePreview{AmountDue: invoice.AmountDue, Currency: string(invoice.Currency)}, nil
}

// UndoPendingChange is forma2's undoPendingOrganizationPlanChange.
func (e *Engine) UndoPendingChange(ctx context.Context, organizationID uuid.UUID, customerID string) error {
	if !e.stripe.Configured() {
		return errStripeNotConfigured
	}
	sub, err := e.requiredSubscription(ctx, organizationID, customerID)
	if err != nil {
		return err
	}
	return e.clearPendingState(ctx, organizationID, sub)
}

// SetupUpgrade is the outcome of finishing a setup checkout, forma2's TSetupCheckoutUpgradeResult.
type SetupUpgrade struct {
	Mode           string  `json:"mode"`
	ClientSecret   *string `json:"clientSecret"`
	RequiresAction bool    `json:"requiresAction"`
	TargetPlan     *string `json:"targetPlan"`
}

// ApplySetupCheckoutUpgrade is forma2's applySetupCheckoutUpgrade: right after a setup checkout with an upgrade intent, the saved card becomes the customer's and the subscription's default and the upgrade is applied, without waiting for the webhook. A session of another organization is refused; a session that is not a completed setup with a paid target changes nothing.
func (e *Engine) ApplySetupCheckoutUpgrade(ctx context.Context, organizationID uuid.UUID, sessionID string) (*SetupUpgrade, error) {
	none := &SetupUpgrade{Mode: ModeImmediate}
	if !e.stripe.Configured() {
		return none, nil
	}
	params := &stripe.CheckoutSessionRetrieveParams{}
	params.AddExpand("setup_intent")
	session, err := e.api().V1CheckoutSessions.Retrieve(ctx, sessionID, params)
	if err != nil {
		return nil, fmt.Errorf("retrieve Stripe checkout session: %w", err)
	}
	if !rowid.Matches(session.Metadata["organizationId"], organizationID) {
		return nil, errNotAllowed(CodeCheckoutSessionMismatch)
	}
	if session.Mode != stripe.CheckoutSessionModeSetup || session.Status != stripe.CheckoutSessionStatusComplete {
		return none, nil
	}
	plan := session.Metadata["targetPlan"]
	if plan != PlanPro && plan != PlanScale {
		return none, nil
	}
	interval := IntervalMonthly
	if session.Metadata["targetInterval"] == IntervalYearly {
		interval = IntervalYearly
	}
	if session.Customer == nil || session.Customer.ID == "" {
		return nil, errNotFound("stripeCustomer", organizationID)
	}
	if session.SetupIntent != nil && session.SetupIntent.PaymentMethod != nil {
		if err := e.attachPaymentMethod(ctx, session.Customer.ID, session.Metadata["subscriptionId"], session.SetupIntent.PaymentMethod.ID); err != nil {
			return nil, err
		}
	}
	change, err := e.SwitchPlan(ctx, organizationID, session.Customer.ID, plan, interval)
	if err != nil {
		return nil, err
	}
	return &SetupUpgrade{Mode: change.Mode, ClientSecret: change.ClientSecret, RequiresAction: change.RequiresAction, TargetPlan: &plan}, nil
}

// attachPaymentMethod makes the card the customer's default for invoices and, when given, the subscription's default.
func (e *Engine) attachPaymentMethod(ctx context.Context, customerID, subscriptionID, paymentMethodID string) error {
	if customerID != "" {
		if _, err := e.api().V1Customers.Update(ctx, customerID, &stripe.CustomerUpdateParams{InvoiceSettings: &stripe.CustomerUpdateInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(paymentMethodID)}}); err != nil {
			return fmt.Errorf("set the Stripe customer's default payment method: %w", err)
		}
	}
	if subscriptionID != "" {
		if _, err := e.api().V1Subscriptions.Update(ctx, subscriptionID, &stripe.SubscriptionUpdateParams{DefaultPaymentMethod: stripe.String(paymentMethodID)}); err != nil {
			return fmt.Errorf("set the Stripe subscription's default payment method: %w", err)
		}
	}
	return nil
}
