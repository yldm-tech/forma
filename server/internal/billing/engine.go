package billing

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stripe/stripe-go/v84"
	"golang.org/x/sync/errgroup"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// The read-through sync of a stale snapshot, forma2's BILLING_SYNC_* constants: a snapshot older than syncStaleAfter is refreshed by the one caller that takes the lock, which expires by itself after syncLockTTL, and that caller waits at most syncDeadline before serving what it has.
const (
	syncStaleAfter = 5 * time.Minute
	syncLockTTL    = 30 * time.Second
	syncDeadline   = 20 * time.Second
)

// subscriptionPageSize is how many of a customer's subscriptions forma2 looked at when resolving the current one.
const subscriptionPageSize = 20

// eventRef identifies the Stripe event a sync is for: its id makes a redelivery a no-op and its creation time keeps an older event from overwriting a newer one.
type eventRef struct {
	ID      string
	Created int64
}

// Engine is billing's Stripe side, forma2's organization-billing library: it keeps organization_billing in step with Stripe and makes the subscription changes the billing page asks for. It authorizes nothing; Service and the webhook do.
type Engine struct {
	cfg    config.Config
	stripe *Stripe
	repo   *Repository
	locks  *redis.Client
	log    *slog.Logger
	now    func() time.Time
}

func NewEngine(cfg config.Config, s *Stripe, repo *Repository, rdb *redis.Client, log *slog.Logger) *Engine {
	return &Engine{cfg: cfg, stripe: s, repo: repo, locks: rdb, log: log, now: time.Now}
}

// Enabled is whether billing runs at all: on Forma Cloud with a Stripe client.
func (e *Engine) Enabled() bool {
	return e.cfg.IsFormaCloud && e.stripe.Configured()
}

func (e *Engine) defaults() Limits { return DefaultLimits(e.cfg.IsFormaCloud) }

func (e *Engine) api() *stripe.Client { return e.stripe.api }

// Record is the organization's billing row from the database, created with the defaults when missing; nil when the organization does not exist.
func (e *Engine) Record(ctx context.Context, organizationID uuid.UUID) (*Record, error) {
	return e.repo.ensure(ctx, organizationID, e.defaults())
}

// EnsureCustomer is forma2's ensureStripeCustomerForOrganization: the organization's Stripe customer, created for its owner on first use. "" when billing is off, the organization does not exist or has no owner.
func (e *Engine) EnsureCustomer(ctx context.Context, organizationID uuid.UUID) (string, error) {
	if !e.Enabled() {
		return "", nil
	}
	rec, err := e.repo.load(ctx, organizationID)
	if err != nil {
		return "", err
	}
	if rec != nil && rec.StripeCustomerID != nil {
		return *rec.StripeCustomerID, nil
	}
	name, exists, err := e.repo.organizationName(ctx, organizationID)
	if err != nil || !exists {
		return "", err
	}
	o, err := e.repo.owner(ctx, organizationID)
	if err != nil {
		return "", err
	}
	if o == nil {
		e.log.ErrorContext(ctx, "Cannot set up Stripe customer: organization has no owner", "organizationId", organizationID.String())
		return "", nil
	}
	params := &stripe.CustomerCreateParams{Email: stripe.String(o.Email), Metadata: map[string]string{"organizationId": organizationID.String(), "organizationName": name}}
	if o.Name != "" {
		params.Name = stripe.String(o.Name)
	}
	params.SetIdempotencyKey("ensure-customer-" + organizationID.String())
	customer, err := e.api().V1Customers.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create Stripe customer: %w", err)
	}
	// The row gets the customer id only; the snapshot below is written just when the row is new, and the sync that follows overwrites it from Stripe.
	return e.repo.setCustomer(ctx, organizationID, customer.ID, e.defaults(), Snapshot{Plan: PlanHobby, LastSyncedAt: ptr(jsvalue.ISOString(e.now()))})
}

// listSubscriptions reads up to limit of the customer's subscriptions, every status, newest first.
func (e *Engine) listSubscriptions(ctx context.Context, customerID string, limit int, expandSchedule bool) ([]*stripe.Subscription, error) {
	params := &stripe.SubscriptionListParams{Customer: stripe.String(customerID), Status: stripe.String("all")}
	params.Limit = stripe.Int64(int64(limit))
	if expandSchedule {
		params.AddExpand("data.schedule")
	}
	var out []*stripe.Subscription
	for sub, err := range e.api().V1Subscriptions.List(ctx, params) {
		if err != nil {
			return nil, fmt.Errorf("list Stripe subscriptions: %w", err)
		}
		out = append(out, sub)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// hydrateProducts is forma2's hydrateSubscriptionProducts: a list expands at most four levels deep, so the products of the subscriptions' prices are read one by one, each once.
func (e *Engine) hydrateProducts(ctx context.Context, subs []*stripe.Subscription) error {
	products := map[string]*stripe.Product{}
	for _, sub := range subs {
		if sub.Items == nil {
			continue
		}
		for _, item := range sub.Items.Data {
			if item.Price == nil || item.Price.Product == nil || item.Price.Product.Object != "" {
				continue
			}
			id := item.Price.Product.ID
			p, ok := products[id]
			if !ok {
				var err error
				if p, err = e.api().V1Products.Retrieve(ctx, id, nil); err != nil {
					return fmt.Errorf("retrieve Stripe product: %w", err)
				}
				products[id] = p
			}
			item.Price.Product = p
		}
	}
	return nil
}

func subscriptionItems(sub *stripe.Subscription) []*stripe.SubscriptionItem {
	if sub == nil || sub.Items == nil {
		return nil
	}
	return sub.Items.Data
}

// topPlanLevel is forma2's getSubscriptionTopPlanLevel.
func topPlanLevel(sub *stripe.Subscription) int {
	level := planLevel[PlanUnknown]
	for _, item := range subscriptionItems(sub) {
		if item.Price != nil {
			level = max(level, planLevel[productPlan(item.Price.Product)])
		}
	}
	return level
}

// subscriptionPlan is forma2's resolveCloudPlanFromSubscription: Hobby without a subscription, else the highest plan among its products.
func subscriptionPlan(sub *stripe.Subscription) string {
	if sub == nil {
		return PlanHobby
	}
	plan := PlanUnknown
	for _, item := range subscriptionItems(sub) {
		if item.Price == nil {
			continue
		}
		if p := productPlan(item.Price.Product); planLevel[p] > planLevel[plan] {
			plan = p
		}
	}
	return plan
}

// basePrice is the subscription's base price, forma2's getBasePriceFromSubscription.
func basePrice(sub *stripe.Subscription) *stripe.Price {
	for _, item := range subscriptionItems(sub) {
		if pricePlan(item.Price) != "" && priceKind(item.Price) == kindBase {
			return item.Price
		}
	}
	return nil
}

func subscriptionInterval(sub *stripe.Subscription) string {
	return priceInterval(basePrice(sub))
}

// currentSubscription is forma2's resolveCurrentSubscription: of the customer's live subscriptions, the one on the highest plan, the newest among equals; nil when there is none.
func (e *Engine) currentSubscription(ctx context.Context, customerID string) (*stripe.Subscription, error) {
	subs, err := e.listSubscriptions(ctx, customerID, subscriptionPageSize, true)
	if err != nil {
		return nil, err
	}
	if err := e.hydrateProducts(ctx, subs); err != nil {
		return nil, err
	}
	var live []*stripe.Subscription
	for _, sub := range subs {
		if activeStatuses[string(sub.Status)] {
			live = append(live, sub)
		}
	}
	if len(live) == 0 {
		return nil, nil
	}
	slices.SortStableFunc(live, func(a, b *stripe.Subscription) int {
		if la, lb := topPlanLevel(a), topPlanLevel(b); la != lb {
			return lb - la
		}
		return int(b.Created - a.Created)
	})
	return live[0], nil
}

// requiredSubscription is the current subscription, a not-found when there is none.
func (e *Engine) requiredSubscription(ctx context.Context, organizationID uuid.UUID, customerID string) (*stripe.Subscription, error) {
	sub, err := e.currentSubscription(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, errNotFound("subscription", organizationID)
	}
	return sub, nil
}

// entitlements is forma2's listAllActiveEntitlements: the lookup keys of the customer's active entitlements, each once.
func (e *Engine) entitlements(ctx context.Context, customerID string) ([]string, error) {
	params := &stripe.EntitlementsActiveEntitlementListParams{Customer: stripe.String(customerID)}
	params.Limit = stripe.Int64(100)
	keys := []string{}
	for ent, err := range e.api().V1EntitlementsActiveEntitlements.List(ctx, params) {
		if err != nil {
			return nil, fmt.Errorf("list Stripe entitlements: %w", err)
		}
		if ent.LookupKey != "" && !slices.Contains(keys, ent.LookupKey) {
			keys = append(keys, ent.LookupKey)
		}
	}
	return keys, nil
}

// entitlementLimit is forma2's parseEntitlementLimit: found is false when no key has the prefix; "unlimited" is nil; several numbers resolve to the largest.
func entitlementLimit(keys []string, prefix string) (limit *int64, found bool) {
	for _, key := range keys {
		raw, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}
		if raw == "unlimited" {
			return nil, true
		}
		if raw == "" || strings.Trim(raw, "0123456789") != "" {
			continue
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		if !found || n > *limit {
			limit = &n
		}
		found = true
	}
	return limit, found
}

// entitlementLimits is forma2's resolveEntitlementDrivenLimits. Every plan carries a workspace and a responses entitlement, so a missing one is a bad read and the previous limit is kept; the workflow-runs entitlement exists only on plans with workflows, so a missing one clears the limit instead of keeping a stale allowance after a downgrade.
func (e *Engine) entitlementLimits(ctx context.Context, organizationID uuid.UUID, keys []string, previous Limits) Limits {
	out := previous
	if v, ok := entitlementLimit(keys, "workspace-limit-"); ok {
		out.Workspaces = v
	} else if previous.Workspaces == nil {
		e.log.WarnContext(ctx, "No workspace limit entitlement found in Stripe entitlements; preserving previous workspaces limit", "organizationId", organizationID.String(), "featureLookupKeys", keys)
	}
	if v, ok := entitlementLimit(keys, "responses-included-"); ok {
		out.Monthly.Responses = v
	} else if previous.Monthly.Responses == nil {
		e.log.WarnContext(ctx, "No responses included entitlement found in Stripe entitlements; preserving previous responses limit", "organizationId", organizationID.String(), "featureLookupKeys", keys)
	}
	out.Monthly.WorkflowRuns, _ = entitlementLimit(keys, "workflow-runs-included-")
	return out
}

// hasPaymentMethod is forma2's hasCollectedPaymentMethod: the subscription's default card, else the customer's, so a card saved on the customer but not yet on the subscription counts.
func (e *Engine) hasPaymentMethod(ctx context.Context, sub *stripe.Subscription, customerID string) (bool, error) {
	if sub.DefaultPaymentMethod != nil {
		return true, nil
	}
	c, err := e.api().V1Customers.Retrieve(ctx, customerID, nil)
	if err != nil {
		return false, fmt.Errorf("retrieve Stripe customer: %w", err)
	}
	return !c.Deleted && c.InvoiceSettings != nil && c.InvoiceSettings.DefaultPaymentMethod != nil, nil
}

// hydratePrices reads the prices a schedule phase names only by id, with their products.
func (e *Engine) hydratePrices(ctx context.Context, prices []*stripe.Price) ([]*stripe.Price, error) {
	out := make([]*stripe.Price, 0, len(prices))
	for _, p := range prices {
		if p == nil || p.Deleted {
			continue
		}
		if p.Object == "" {
			params := &stripe.PriceRetrieveParams{}
			params.AddExpand("product")
			var err error
			if p, err = e.api().V1Prices.Retrieve(ctx, p.ID, params); err != nil {
				return nil, fmt.Errorf("retrieve Stripe price: %w", err)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// schedulePendingChange is forma2's getPendingPlanChangeFromSchedule: the plan the subscription's schedule moves to when its current phase ends.
func (e *Engine) schedulePendingChange(ctx context.Context, sub *stripe.Subscription) (*PendingChange, error) {
	if sub.Schedule == nil {
		return nil, nil
	}
	schedule, err := e.api().V1SubscriptionSchedules.Retrieve(ctx, sub.Schedule.ID, nil)
	if err != nil {
		return nil, fmt.Errorf("retrieve Stripe subscription schedule: %w", err)
	}
	if schedule.CurrentPhase == nil || schedule.CurrentPhase.EndDate == 0 {
		return nil, nil
	}
	next := nextPhase(schedule, schedule.CurrentPhase.EndDate)
	if next == nil {
		return nil, nil
	}
	var refs []*stripe.Price
	for _, item := range next.Items {
		refs = append(refs, item.Price)
	}
	prices, err := e.hydratePrices(ctx, refs)
	if err != nil {
		return nil, err
	}
	var base *stripe.Price
	for _, p := range prices {
		if priceKind(p) == kindBase {
			base = p
			break
		}
	}
	plan := pricePlan(base)
	if plan == "" {
		return nil, nil
	}
	return &PendingChange{Type: "plan_change", TargetPlan: plan, TargetInterval: optional(priceInterval(base)), EffectiveAt: isoUnix(next.StartDate)}, nil
}

func nextPhase(schedule *stripe.SubscriptionSchedule, currentEnd int64) *stripe.SubscriptionSchedulePhase {
	for _, phase := range schedule.Phases {
		if phase.StartDate >= currentEnd {
			return phase
		}
	}
	return nil
}

// pendingChange is forma2's resolvePendingPlanChange: a scheduled plan change, else a cancellation at period end, which leaves the organization on Hobby when the subscription's period (or its cancel_at) ends.
func (e *Engine) pendingChange(ctx context.Context, sub *stripe.Subscription) (*PendingChange, error) {
	if sub == nil {
		return nil, nil
	}
	scheduled, err := e.schedulePendingChange(ctx, sub)
	if err != nil || scheduled != nil {
		return scheduled, err
	}
	if !sub.CancelAtPeriodEnd {
		return nil, nil
	}
	effective := sub.CancelAt
	if effective == 0 {
		for _, item := range subscriptionItems(sub) {
			effective = max(effective, item.CurrentPeriodEnd)
		}
	}
	if effective == 0 {
		return nil, nil
	}
	return &PendingChange{Type: "plan_change", TargetPlan: PlanHobby, TargetInterval: ptr(IntervalMonthly), EffectiveAt: isoUnix(effective)}, nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Sync is forma2's syncOrganizationBillingFromStripe: the organization's current subscription and entitlements read from Stripe and written to its billing row as the plan, interval, status, payment method, features, pending change, trial end, usage cycle anchor and limits. A sync for an event that was already synced, or that is older than the last one synced, changes nothing. Nil when billing is off or the organization does not exist; the row unchanged when it has no Stripe customer yet.
func (e *Engine) Sync(ctx context.Context, organizationID uuid.UUID, ev *eventRef) (*Record, error) {
	if !e.Enabled() {
		return nil, nil
	}
	rec, err := e.Record(ctx, organizationID)
	if err != nil || rec == nil || rec.StripeCustomerID == nil {
		return rec, err
	}
	customerID := *rec.StripeCustomerID
	existing := Snapshot{}
	if rec.Stripe != nil {
		existing = *rec.Stripe
	}
	previousEvent := parseISOTime(existing.LastStripeEventCreatedAt)
	var incomingEvent *time.Time
	if ev != nil {
		if existing.LastSyncedEventID != nil && *existing.LastSyncedEventID == ev.ID {
			return rec, nil
		}
		t := time.Unix(ev.Created, 0)
		if previousEvent != nil && t.Before(*previousEvent) {
			return rec, nil
		}
		incomingEvent = &t
	}

	var sub *stripe.Subscription
	var features []string
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		sub, err = e.currentSubscription(gctx, customerID)
		return err
	})
	g.Go(func() error {
		var err error
		features, err = e.entitlements(gctx, customerID)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	plan := subscriptionPlan(sub)
	pending, err := e.pendingChange(ctx, sub)
	if err != nil {
		return nil, err
	}
	hasCard := false
	if sub != nil {
		if hasCard, err = e.hasPaymentMethod(ctx, sub, customerID); err != nil {
			return nil, err
		}
	}

	next := existing
	next.Plan = plan
	next.Interval = optional(subscriptionInterval(sub))
	next.SubscriptionStatus, next.SubscriptionID = nil, nil
	if sub != nil {
		next.SubscriptionStatus, next.SubscriptionID = ptr(string(sub.Status)), ptr(sub.ID)
	}
	next.HasPaymentMethod = &hasCard
	next.Features = features
	next.PendingChange = pending
	// Only a real settlement (a webhook event, or a plan change the sync observes) clears the payment-failure banner; a staleness refresh must not dismiss it silently.
	if ev != nil || plan != existing.Plan {
		next.PaymentAttemptError = nil
	}
	next.LastStripeEventCreatedAt = nil
	if incomingEvent != nil {
		next.LastStripeEventCreatedAt = ptr(jsvalue.ISOString(*incomingEvent))
	} else if previousEvent != nil {
		next.LastStripeEventCreatedAt = ptr(jsvalue.ISOString(*previousEvent))
	}
	next.LastSyncedAt = ptr(jsvalue.ISOString(e.now()))
	if ev != nil {
		next.LastSyncedEventID = ptr(ev.ID)
	}
	if sub != nil && sub.TrialEnd != 0 {
		next.TrialEnd = ptr(isoUnix(sub.TrialEnd))
	}

	updated := &Record{OrganizationID: organizationID, StripeCustomerID: &customerID, Stripe: &next, Limits: e.entitlementLimits(ctx, organizationID, features, rec.Limits)}
	if sub != nil && sub.BillingCycleAnchor != 0 {
		updated.UsageCycleAnchor = ptr(time.Unix(sub.BillingCycleAnchor, 0).UTC())
	}
	if err := e.repo.writeSync(ctx, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// syncLockKey is forma2's billingSyncLock cache key.
func syncLockKey(organizationID uuid.UUID) string {
	return "fb:org:" + organizationID.String() + ":billing-sync-lock"
}

// ReadThrough is forma2's getOrganizationBillingWithReadThroughSync, what the billing page reads. On Forma Cloud a snapshot older than five minutes is refreshed from Stripe by the one caller that takes the lock, waiting at most syncDeadline; everyone else, and that caller when Stripe fails or is slow, gets the stored row.
func (e *Engine) ReadThrough(ctx context.Context, organizationID uuid.UUID) (*Record, error) {
	rec, err := e.Record(ctx, organizationID)
	if err != nil || rec == nil || !e.cfg.IsFormaCloud || rec.StripeCustomerID == nil {
		return rec, err
	}
	if last := parseISOTime(lastSyncedAt(rec)); last != nil && e.now().Sub(*last) <= syncStaleAfter {
		return rec, nil
	}
	if e.locks == nil {
		return rec, nil
	}
	acquired, err := e.locks.SetNX(ctx, syncLockKey(organizationID), "1", syncLockTTL).Result()
	if err != nil || !acquired {
		return rec, nil
	}
	sctx, cancel := context.WithTimeout(ctx, syncDeadline)
	defer cancel()
	synced, err := e.Sync(sctx, organizationID, nil)
	if err != nil {
		e.log.WarnContext(ctx, "Failed to refresh billing snapshot from Stripe", "err", err, "organizationId", organizationID.String())
		return rec, nil
	}
	if synced == nil {
		return rec, nil
	}
	return synced, nil
}

func lastSyncedAt(rec *Record) *string {
	if rec.Stripe == nil {
		return nil
	}
	return rec.Stripe.LastSyncedAt
}

// SetPaymentAttemptError is forma2's setOrganizationPaymentAttemptError: it sets or clears the payment-failure banner. An event older than the last one synced is ignored, so a stale payment_intent event cannot bring back a banner a newer sync resolved.
func (e *Engine) SetPaymentAttemptError(ctx context.Context, organizationID uuid.UUID, paymentErr *PaymentAttemptError, ev *eventRef) error {
	if _, err := e.Record(ctx, organizationID); err != nil {
		return err
	}
	return e.repo.updateSnapshot(ctx, organizationID, func(rec *Record) bool {
		if rec.Stripe == nil {
			rec.Stripe = &Snapshot{}
		}
		if ev != nil {
			if last := parseISOTime(rec.Stripe.LastStripeEventCreatedAt); last != nil && time.Unix(ev.Created, 0).Before(*last) {
				return false
			}
		}
		rec.Stripe.PaymentAttemptError = paymentErr
		rec.Stripe.LastSyncedAt = ptr(jsvalue.ISOString(e.now()))
		return true
	})
}

// setPendingChange is forma2's updatePendingPlanChangeSnapshot.
func (e *Engine) setPendingChange(ctx context.Context, organizationID uuid.UUID, change *PendingChange) error {
	return e.repo.updateSnapshot(ctx, organizationID, func(rec *Record) bool {
		if rec.Stripe == nil {
			rec.Stripe = &Snapshot{}
		}
		rec.Stripe.PendingChange = change
		rec.Stripe.LastSyncedAt = ptr(jsvalue.ISOString(e.now()))
		return true
	})
}

// addOptimisticFeature is forma2's addOptimisticBillingFeature: a feature granted by a subscription change, visible before Stripe's entitlements catch up; the webhook's sync converges on the same list.
func (e *Engine) addOptimisticFeature(ctx context.Context, organizationID uuid.UUID, feature string) error {
	return e.repo.updateSnapshot(ctx, organizationID, func(rec *Record) bool {
		if rec.Stripe == nil || slices.Contains(rec.Stripe.Features, feature) {
			return false
		}
		rec.Stripe.Features = append(rec.Stripe.Features, feature)
		return true
	})
}

// Reconcile is forma2's reconcileCloudStripeSubscriptionsForOrganization: an organization with a paid, trial or unrecognised subscription has its Hobby subscriptions cancelled, and one with no live subscription gets a Hobby one.
func (e *Engine) Reconcile(ctx context.Context, organizationID uuid.UUID) error {
	if !e.Enabled() {
		return nil
	}
	rec, err := e.Record(ctx, organizationID)
	if err != nil || rec == nil || rec.StripeCustomerID == nil {
		return err
	}
	customerID := *rec.StripeCustomerID
	subs, err := e.listSubscriptions(ctx, customerID, subscriptionPageSize, false)
	if err != nil {
		return err
	}
	if err := e.hydrateProducts(ctx, subs); err != nil {
		return err
	}
	var live []*stripe.Subscription
	var unknown []string
	paidOrTrial := false
	for _, sub := range subs {
		if !activeStatuses[string(sub.Status)] {
			continue
		}
		live = append(live, sub)
		level := topPlanLevel(sub)
		if level == planLevel[PlanUnknown] {
			unknown = append(unknown, sub.ID)
		}
		if level > planLevel[PlanHobby] || level == planLevel[PlanUnknown] {
			paidOrTrial = true
		}
	}
	if len(unknown) > 0 {
		e.log.WarnContext(ctx, "Found subscriptions with unknown plan level during reconciliation", "organizationId", organizationID.String(), "subscriptionIds", unknown)
	}
	if paidOrTrial {
		for _, sub := range live {
			if topPlanLevel(sub) != planLevel[PlanHobby] {
				continue
			}
			if err := e.cancelSubscription(ctx, sub.ID); err != nil {
				return err
			}
		}
		return nil
	}
	if len(live) > 0 {
		return nil
	}
	// Read again: a webhook and the bootstrap reconciling at once would otherwise both see nothing and both create a Hobby subscription.
	fresh, err := e.listSubscriptions(ctx, customerID, subscriptionPageSize, false)
	if err != nil {
		return err
	}
	for _, sub := range fresh {
		if activeStatuses[string(sub.Status)] {
			return nil
		}
	}
	return e.createHobbySubscription(ctx, organizationID, customerID, len(fresh))
}

// cancelSubscription cancels without proration; a subscription that is already gone is not an error.
func (e *Engine) cancelSubscription(ctx context.Context, id string) error {
	_, err := e.api().V1Subscriptions.Cancel(ctx, id, &stripe.SubscriptionCancelParams{Prorate: stripe.Bool(false)})
	if err != nil && isResourceMissing(err) {
		e.log.WarnContext(ctx, "Subscription already deleted, skipping cancel", "subscriptionId", id)
		return nil
	}
	if err != nil {
		return fmt.Errorf("cancel Stripe subscription: %w", err)
	}
	return nil
}

// createHobbySubscription is forma2's ensureHobbySubscription. The subscription count in the idempotency key deduplicates concurrent calls yet lets a Hobby subscription be created again after the previous one was cancelled.
func (e *Engine) createHobbySubscription(ctx context.Context, organizationID uuid.UUID, customerID string, subscriptionCount int) error {
	catalog, err := e.stripe.Catalog(ctx)
	if err != nil {
		return err
	}
	item, err := catalog.Item(PlanHobby, IntervalMonthly)
	if err != nil {
		return err
	}
	params := &stripe.SubscriptionCreateParams{Customer: stripe.String(customerID), Items: createItems(item.lineItems()), Metadata: map[string]string{"organizationId": organizationID.String()}}
	params.SetIdempotencyKey(fmt.Sprintf("ensure-hobby-subscription-%s-%d", organizationID, subscriptionCount))
	if _, err := e.api().V1Subscriptions.Create(ctx, params); err != nil {
		return fmt.Errorf("create Stripe Hobby subscription: %w", err)
	}
	return nil
}

func createItems(items []lineItem) []*stripe.SubscriptionCreateItemParams {
	out := make([]*stripe.SubscriptionCreateItemParams, len(items))
	for i, it := range items {
		out[i] = &stripe.SubscriptionCreateItemParams{Price: stripe.String(it.Price), Quantity: it.Quantity}
	}
	return out
}

// EnsureSetup is forma2's ensureCloudStripeSetupForOrganization, run for every new organization: its customer, a Hobby subscription, and the first sync.
func (e *Engine) EnsureSetup(ctx context.Context, organizationID uuid.UUID) error {
	if !e.Enabled() {
		return nil
	}
	if _, err := e.EnsureCustomer(ctx, organizationID); err != nil {
		return err
	}
	if err := e.Reconcile(ctx, organizationID); err != nil {
		return err
	}
	_, err := e.Sync(ctx, organizationID, nil)
	return err
}

// CleanupCustomer is forma2's cleanupStripeCustomer for a deleted organization: its live subscriptions are cancelled without proration, and the customer is kept so its trial history still counts against the same email.
func (e *Engine) CleanupCustomer(ctx context.Context, customerID string) error {
	if !e.stripe.Configured() {
		return nil
	}
	subs, err := e.listSubscriptions(ctx, customerID, 100, false)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if !activeStatuses[string(sub.Status)] {
			continue
		}
		if err := e.cancelSubscription(ctx, sub.ID); err != nil {
			return err
		}
	}
	return nil
}
