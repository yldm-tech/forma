package billing

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// The bounded resync of the wait actions, forma2's BILLING_PLAN_SYNC_*: Stripe applies a paid upgrade, and the webhook attaches a card, a moment after the browser returns.
const (
	pollAttempts = 5
	pollDelay    = 1200 * time.Millisecond
)

// Service is the billing page's server side, forma2's billing actions: every method authorizes the acting user on the organization, manage_billing for everything that touches the subscription and manage for the onboarding plan step, and emits forma2's subscriptionAccessed audit events.
type Service struct {
	engine    *Engine
	authz     *authz.Authorizer
	limiter   *ratelimit.Limiter
	audit     *audit.Emitter
	pollDelay time.Duration
}

func NewService(engine *Engine, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter) *Service {
	return &Service{engine: engine, authz: authorizer, limiter: limiter, audit: emitter, pollDelay: pollDelay}
}

// record emits subscriptionAccessed for the organization, a failure included, as forma2's withAuditLogging did.
func (s *Service) record(ctx context.Context, organizationID uuid.UUID, err error, newObject any) {
	e := audit.Event{Action: audit.ActionSubscriptionAccessed, TargetType: audit.TargetOrganization, TargetID: organizationID.String(), OrganizationID: organizationID.String(), ActorType: audit.ActorUser, Status: audit.StatusSuccess, NewObject: newObject}
	if err != nil {
		e.Status, e.NewObject = audit.StatusFailure, nil
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

func (s *Service) require(ctx context.Context, userID, organizationID uuid.UUID, action authz.Action) error {
	return s.authz.Require(ctx, authz.User(userID), action, authz.Organization(organizationID))
}

// customer is the organization's Stripe customer, a not-found when it has none (forma2's actions read it off getOrganization).
func (s *Service) customer(ctx context.Context, organizationID uuid.UUID) (string, *Record, error) {
	rec, err := s.engine.Record(ctx, organizationID)
	if err != nil {
		return "", nil, err
	}
	if rec == nil {
		return "", nil, errNotFound("organization", organizationID)
	}
	if rec.StripeCustomerID == nil {
		return "", nil, errNotFound("OrganizationBilling", organizationID)
	}
	return *rec.StripeCustomerID, rec, nil
}

// member returns the caller's role when they may read the organization, the organization's refusal to a member it refuses (authz.Refusal), and a not-found otherwise, so an organization's existence never leaks (forma2's getOrganizationAuth).
func (s *Service) member(ctx context.Context, userID, organizationID uuid.UUID) (string, error) {
	allowed, err := s.authz.Can(ctx, authz.User(userID), authz.OrganizationRead, authz.Organization(organizationID))
	if err != nil {
		return "", err
	}
	if !allowed {
		// A member the organization refuses (suspended, or held to its two-factor requirement) is told why, and the browser shows them the way out; anyone else learns nothing.
		refusal, err := s.authz.Refusal(ctx, userID, organizationID)
		if err != nil {
			return "", err
		}
		if refusal != nil {
			return "", refusal
		}
		return "", errNotFound("Organization", organizationID)
	}
	role, err := s.engine.repo.role(ctx, userID, organizationID)
	if err != nil {
		return "", err
	}
	if role == "" {
		return "", errNotFound("Organization", organizationID)
	}
	return role, nil
}

// Page is what the billing settings page renders, forma2's PricingPage props.
type Page struct {
	Billing                   *Record        `json:"billing"`
	CurrentCloudPlan          string         `json:"currentCloudPlan"`
	CurrentBillingInterval    *string        `json:"currentBillingInterval"`
	CurrentSubscriptionStatus *string        `json:"currentSubscriptionStatus"`
	PendingChange             *PendingChange `json:"pendingChange"`
	TrialDaysRemaining        *int64         `json:"trialDaysRemaining"`
	UsageCycleStart           time.Time      `json:"usageCycleStart"`
	UsageCycleEnd             time.Time      `json:"usageCycleEnd"`
	Catalog                   CatalogDisplay `json:"billingCatalog"`
	ResponseCount             int64          `json:"responseCount"`
	WorkspaceCount            int64          `json:"workspaceCount"`
	WorkflowRunCount          int64          `json:"workflowRunCount"`
	// IsPlanComparison is forma2's PostHog experiment of a comparison table, off without the experiment.
	IsPlanComparison        bool    `json:"isPlanComparison"`
	HasBillingRights        bool    `json:"hasBillingRights"`
	IsStripeSetupIncomplete bool    `json:"isStripeSetupIncomplete"`
	StripePublishableKey    *string `json:"stripePublishableKey"`
}

// trialDaysRemaining is forma2's resolveTrialDaysRemaining: whole days left in a trial, rounded up.
func trialDaysRemaining(rec *Record, now time.Time) *int64 {
	if rec.Stripe == nil || rec.Stripe.SubscriptionStatus == nil || *rec.Stripe.SubscriptionStatus != "trialing" {
		return nil
	}
	end := parseISOTime(rec.Stripe.TrialEnd)
	if end == nil {
		return nil
	}
	days := int64(math.Ceil(float64(end.Sub(now).Milliseconds()) / float64(24*time.Hour/time.Millisecond)))
	return &days
}

// Page is forma2's billing page loader: any member may view it (members without billing rights read-only); it exists only on Forma Cloud.
func (s *Service) Page(ctx context.Context, userID, organizationID uuid.UUID) (*Page, error) {
	role, err := s.member(ctx, userID, organizationID)
	if err != nil {
		return nil, err
	}
	if !s.engine.cfg.IsFormaCloud {
		return nil, errNotFound("Billing", organizationID)
	}
	rec, err := s.engine.ReadThrough(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, errNotFound("OrganizationBilling", organizationID)
	}
	catalog, err := s.engine.stripe.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	now := s.engine.now()
	cycle := MonthlyUsageCycle(rec.UsageCycleAnchor, now)
	usage, err := s.engine.repo.usage(ctx, organizationID, cycle.Start, cycle.End)
	if err != nil {
		return nil, err
	}
	p := &Page{
		Billing: rec, CurrentCloudPlan: rec.plan(), TrialDaysRemaining: trialDaysRemaining(rec, now), UsageCycleStart: cycle.Start, UsageCycleEnd: cycle.End,
		Catalog: catalog.Display(), ResponseCount: usage.Responses, WorkspaceCount: usage.Workspaces, WorkflowRunCount: usage.WorkflowRuns,
		IsPlanComparison: isPlanComparison(userID), HasBillingRights: role != "member", IsStripeSetupIncomplete: rec.StripeCustomerID == nil, StripePublishableKey: optional(s.engine.stripe.publishableKey),
	}
	if rec.Stripe != nil {
		p.CurrentBillingInterval, p.CurrentSubscriptionStatus, p.PendingChange = rec.Stripe.Interval, rec.Stripe.SubscriptionStatus, rec.Stripe.PendingChange
	}
	return p, nil
}

// Summary is what the rest of the app shows about billing: the limits banner and trial notices of the workspace and settings layouts, and whether the onboarding plan step applies.
type Summary struct {
	IsFormaCloud       bool    `json:"isFormaCloud"`
	Plan               string  `json:"plan"`
	SubscriptionStatus *string `json:"subscriptionStatus"`
	TrialEnd           *string `json:"trialEnd"`
	TrialDaysRemaining *int64  `json:"trialDaysRemaining"`
	// ProTrialDays is how long a Pro trial started now would run (forma2's getProTrialDays), which the onboarding plan step promises.
	ProTrialDays int64  `json:"proTrialDays"`
	Limits       Limits `json:"limits"`
	// Usage is counted on Forma Cloud only, where forma2 rendered the limits banner.
	Usage      *Usage      `json:"usage"`
	UsageCycle *UsageCycle `json:"usageCycle"`
}

// Summary reads the stored billing row of an organization the caller belongs to.
func (s *Service) Summary(ctx context.Context, userID, organizationID uuid.UUID) (*Summary, error) {
	if _, err := s.member(ctx, userID, organizationID); err != nil {
		return nil, err
	}
	rec, err := s.engine.Record(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, errNotFound("Organization", organizationID)
	}
	now := s.engine.now()
	out := &Summary{IsFormaCloud: s.engine.cfg.IsFormaCloud, Plan: rec.plan(), Limits: rec.Limits, TrialDaysRemaining: trialDaysRemaining(rec, now), ProTrialDays: trialDays(organizationID)}
	if rec.Stripe != nil {
		out.SubscriptionStatus, out.TrialEnd = rec.Stripe.SubscriptionStatus, rec.Stripe.TrialEnd
	}
	if s.engine.cfg.IsFormaCloud {
		cycle := MonthlyUsageCycle(rec.UsageCycleAnchor, now)
		usage, err := s.engine.repo.usage(ctx, organizationID, cycle.Start, cycle.End)
		if err != nil {
			return nil, err
		}
		out.Usage, out.UsageCycle = &usage, &cycle
	}
	return out, nil
}

// PortalURL is forma2's manageSubscriptionAction.
func (s *Service) PortalURL(ctx context.Context, userID, organizationID uuid.UUID) (string, error) {
	url, err := s.portalURL(ctx, userID, organizationID)
	s.record(ctx, organizationID, err, map[string]any{"portalSessionCreated": true})
	return url, err
}

func (s *Service) portalURL(ctx context.Context, userID, organizationID uuid.UUID) (string, error) {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling); err != nil {
		return "", err
	}
	customer, _, err := s.customer(ctx, organizationID)
	if err != nil {
		return "", err
	}
	return s.engine.PortalURL(ctx, organizationID, customer)
}

// CheckoutURL is forma2's createPlanCheckoutAction: a Checkout for an organization that has no subscription at all.
func (s *Service) CheckoutURL(ctx context.Context, userID, organizationID uuid.UUID, plan, interval string) (string, error) {
	url, err := s.checkoutURL(ctx, userID, organizationID, plan, interval)
	s.record(ctx, organizationID, err, map[string]any{"checkoutCreated": true, "targetPlan": plan, "targetInterval": interval})
	return url, err
}

func (s *Service) checkoutURL(ctx context.Context, userID, organizationID uuid.UUID, plan, interval string) (string, error) {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling); err != nil {
		return "", err
	}
	customer, rec, err := s.customer(ctx, organizationID)
	if err != nil {
		return "", err
	}
	if rec.Stripe != nil && rec.Stripe.SubscriptionID != nil && *rec.Stripe.SubscriptionID != "" {
		return "", errNotAllowed(CodePaidCheckoutRequiresNoSubscription)
	}
	return s.engine.PlanCheckoutURL(ctx, organizationID, customer, plan, interval)
}

// SetupCheckoutURL is forma2's createTrialPaymentCheckoutAction: a card for the existing subscription, optionally on the way to an upgrade (monthly unless an interval is given).
func (s *Service) SetupCheckoutURL(ctx context.Context, userID, organizationID uuid.UUID, intent *UpgradeIntent) (string, error) {
	url, err := s.setupCheckoutURL(ctx, userID, organizationID, intent)
	newObject := map[string]any{"setupCheckoutCreated": true}
	if intent != nil {
		newObject["targetPlan"], newObject["targetInterval"] = intent.Plan, intent.Interval
	}
	s.record(ctx, organizationID, err, newObject)
	return url, err
}

func (s *Service) setupCheckoutURL(ctx context.Context, userID, organizationID uuid.UUID, intent *UpgradeIntent) (string, error) {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling); err != nil {
		return "", err
	}
	customer, rec, err := s.customer(ctx, organizationID)
	if err != nil {
		return "", err
	}
	if rec.Stripe == nil || rec.Stripe.SubscriptionID == nil || *rec.Stripe.SubscriptionID == "" {
		return "", errNotFound("subscription", organizationID)
	}
	return s.engine.SetupCheckoutURL(ctx, organizationID, customer, *rec.Stripe.SubscriptionID, intent)
}

// PreviewUpgrade is forma2's getUpgradeChargePreviewAction; it changes nothing and is not audited.
func (s *Service) PreviewUpgrade(ctx context.Context, userID, organizationID uuid.UUID, plan, interval string) (*ChargePreview, error) {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling); err != nil {
		return nil, err
	}
	customer, _, err := s.customer(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	return s.engine.PreviewUpgrade(ctx, organizationID, customer, plan, interval)
}

// RetrySetup is forma2's retryStripeSetupAction, the billing page's way out of a failed setup.
func (s *Service) RetrySetup(ctx context.Context, userID, organizationID uuid.UUID) error {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling); err != nil {
		return err
	}
	return s.engine.EnsureSetup(ctx, organizationID)
}

// ensureCustomer is the organization's customer, created when it has none yet.
func (s *Service) ensureCustomer(ctx context.Context, organizationID uuid.UUID) (string, error) {
	rec, err := s.engine.Record(ctx, organizationID)
	if err != nil {
		return "", err
	}
	if rec == nil {
		return "", errNotFound("organization", organizationID)
	}
	if rec.StripeCustomerID != nil {
		return *rec.StripeCustomerID, nil
	}
	customer, err := s.engine.EnsureCustomer(ctx, organizationID)
	if err != nil {
		return "", err
	}
	if customer == "" {
		return "", errNotFound("OrganizationBilling", organizationID)
	}
	return customer, nil
}

// StartHobby is forma2's startHobbyAction, the onboarding plan step's "stay on Hobby".
func (s *Service) StartHobby(ctx context.Context, userID, organizationID uuid.UUID) error {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManage); err != nil {
		return err
	}
	if _, err := s.ensureCustomer(ctx, organizationID); err != nil {
		return err
	}
	if err := s.engine.Reconcile(ctx, organizationID); err != nil {
		return err
	}
	if _, err := s.engine.Sync(ctx, organizationID, nil); err != nil {
		return err
	}
	analytics.Capture(userID.String(), "stayed_on_hobby_plan", analytics.Properties{"organization_id": organizationID.String()}, analytics.Groups{OrganizationID: organizationID.String()})
	return nil
}

// StartProTrial is forma2's startProTrialAction, the onboarding plan step's trial. AI smart tools are granted at once, before Stripe's entitlements catch up, so the next page already offers them.
func (s *Service) StartProTrial(ctx context.Context, userID, organizationID uuid.UUID) error {
	days := trialDays(organizationID)
	err := s.startProTrial(ctx, userID, organizationID, days)
	s.record(ctx, organizationID, err, map[string]any{"plan": PlanPro, "trialDurationDays": days})
	if err == nil {
		org := organizationID.String()
		groups := analytics.Groups{OrganizationID: org}
		analytics.Capture(userID.String(), "free_trial_started", analytics.Properties{"plan": PlanPro, "organization_id": org, "trial_duration_days": days}, groups)
		analytics.Capture(userID.String(), "reverse_trial_started", analytics.Properties{"organization_id": org}, groups)
	}
	return err
}

func (s *Service) startProTrial(ctx context.Context, userID, organizationID uuid.UUID, days int64) error {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManage); err != nil {
		return err
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, organizationID.String()); err != nil {
		return err
	}
	customer, err := s.ensureCustomer(ctx, organizationID)
	if err != nil {
		return err
	}
	if err := s.engine.StartProTrial(ctx, organizationID, customer, days); err != nil {
		return err
	}
	if err := s.engine.Reconcile(ctx, organizationID); err != nil {
		return err
	}
	if _, err := s.engine.Sync(ctx, organizationID, nil); err != nil {
		return err
	}
	return s.engine.addOptimisticFeature(ctx, organizationID, FeatureAISmartTools)
}

// ChangePlan is forma2's changeBillingPlanAction. A paid plan needs a card on file, which the server checks too so a direct call cannot turn a trial without a card into a paid plan. An immediate change that needs no 3D Secure is synced at once; one awaiting 3D Secure is synced by the wait after the browser confirms, and a scheduled one by the webhook.
func (s *Service) ChangePlan(ctx context.Context, userID, organizationID uuid.UUID, plan, interval string) (*PlanChange, error) {
	change, err := s.changePlan(ctx, userID, organizationID, plan, interval)
	newObject := map[string]any{"targetPlan": plan, "targetInterval": interval}
	if change != nil {
		newObject["mode"] = change.Mode
	}
	s.record(ctx, organizationID, err, newObject)
	return change, err
}

func (s *Service) changePlan(ctx context.Context, userID, organizationID uuid.UUID, plan, interval string) (*PlanChange, error) {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling); err != nil {
		return nil, err
	}
	customer, rec, err := s.customer(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if plan != PlanHobby && (rec.Stripe == nil || rec.Stripe.HasPaymentMethod == nil || !*rec.Stripe.HasPaymentMethod) {
		return nil, errNotAllowed(CodePaymentMethodRequired)
	}
	change, err := s.engine.SwitchPlan(ctx, organizationID, customer, plan, interval)
	if err != nil {
		return nil, err
	}
	if change.Mode == ModeImmediate && !change.RequiresAction {
		if _, err := s.engine.Sync(ctx, organizationID, nil); err != nil {
			return nil, err
		}
	}
	return change, nil
}

// ReportPaymentIssue is forma2's reportUpgradePaymentIssueAction: the browser abandoned or failed 3D Secure, which Stripe does not report promptly.
func (s *Service) ReportPaymentIssue(ctx context.Context, userID, organizationID uuid.UUID, paymentIntentID string) error {
	err := s.reportPaymentIssue(ctx, userID, organizationID, paymentIntentID)
	s.record(ctx, organizationID, err, map[string]any{"paymentAttemptError": PaymentErrorRequiresAction})
	return err
}

func (s *Service) reportPaymentIssue(ctx context.Context, userID, organizationID uuid.UUID, paymentIntentID string) error {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling); err != nil {
		return err
	}
	return s.engine.SetPaymentAttemptError(ctx, organizationID, &PaymentAttemptError{
		Type: PaymentErrorRequiresAction, PaymentIntentID: paymentIntentID,
		Message: "Payment authentication was not completed. Please try again or contact support.", CreatedAt: jsvalue.ISOString(s.engine.now()),
	}, nil)
}

// FinalizeSetupCheckout is forma2's finalizeSetupCheckoutUpgradeAction, called by the billing page when a setup checkout with an upgrade intent returns.
func (s *Service) FinalizeSetupCheckout(ctx context.Context, userID, organizationID uuid.UUID, sessionID string) (*SetupUpgrade, error) {
	result, err := s.finalizeSetupCheckout(ctx, userID, organizationID, sessionID)
	var newObject map[string]any
	if result != nil {
		newObject = map[string]any{"targetPlan": result.TargetPlan, "mode": result.Mode}
	}
	s.record(ctx, organizationID, err, newObject)
	return result, err
}

func (s *Service) finalizeSetupCheckout(ctx context.Context, userID, organizationID uuid.UUID, sessionID string) (*SetupUpgrade, error) {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling); err != nil {
		return nil, err
	}
	result, err := s.engine.ApplySetupCheckoutUpgrade(ctx, organizationID, sessionID)
	if err != nil {
		return nil, err
	}
	if result.Mode == ModeImmediate && !result.RequiresAction {
		if _, err := s.engine.Sync(ctx, organizationID, nil); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// poll is forma2's pollBillingSync: sync until done holds for the synced row or the attempts run out.
func (s *Service) poll(ctx context.Context, organizationID uuid.UUID, done func(*Record) bool) (*Record, error) {
	var last *Record
	for attempt := range pollAttempts {
		rec, err := s.engine.Sync(ctx, organizationID, nil)
		if err != nil {
			return nil, err
		}
		last = rec
		if done(rec) {
			break
		}
		if attempt < pollAttempts-1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(s.pollDelay):
			}
		}
	}
	return last, nil
}

// WaitForPlan is forma2's waitForBillingPlanAction: the plan once it is the target, or as last synced. Nil when nothing was synced.
func (s *Service) WaitForPlan(ctx context.Context, userID, organizationID uuid.UUID, plan string) (*string, error) {
	var result *string
	err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling)
	if err == nil {
		var rec *Record
		rec, err = s.poll(ctx, organizationID, func(r *Record) bool { return r != nil && r.Stripe != nil && r.Stripe.Plan == plan })
		if rec != nil && rec.Stripe != nil && rec.Stripe.Plan != "" {
			result = &rec.Stripe.Plan
		}
	}
	s.record(ctx, organizationID, err, nil)
	return result, err
}

// WaitForPaymentMethod is forma2's waitForBillingPaymentMethodAction: whether the card a setup checkout saved has reached the subscription.
func (s *Service) WaitForPaymentMethod(ctx context.Context, userID, organizationID uuid.UUID) (bool, error) {
	hasCard := func(r *Record) bool {
		return r != nil && r.Stripe != nil && r.Stripe.HasPaymentMethod != nil && *r.Stripe.HasPaymentMethod
	}
	var result bool
	err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling)
	if err == nil {
		var rec *Record
		rec, err = s.poll(ctx, organizationID, hasCard)
		result = hasCard(rec)
	}
	s.record(ctx, organizationID, err, nil)
	return result, err
}

// UndoPendingChange is forma2's undoPendingPlanChangeAction.
func (s *Service) UndoPendingChange(ctx context.Context, userID, organizationID uuid.UUID) error {
	err := s.undoPendingChange(ctx, userID, organizationID)
	s.record(ctx, organizationID, err, nil)
	return err
}

func (s *Service) undoPendingChange(ctx context.Context, userID, organizationID uuid.UUID) error {
	if err := s.require(ctx, userID, organizationID, authz.OrganizationManageBilling); err != nil {
		return err
	}
	customer, _, err := s.customer(ctx, organizationID)
	if err != nil {
		return err
	}
	if err := s.engine.UndoPendingChange(ctx, organizationID, customer); err != nil {
		return err
	}
	_, err = s.engine.Sync(ctx, organizationID, nil)
	return err
}
