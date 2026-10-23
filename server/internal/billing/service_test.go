package billing

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// billedOrganization is an organization with a customer, a fresh Pro snapshot, an owner and members of every other role.
func (a *app) billedOrganization() (org, owner, manager, member, billingUser uuid.UUID) {
	org = a.customerOrganization(`{"plan":"pro","interval":"monthly","subscriptionStatus":"trialing","subscriptionId":"sub_1","hasPaymentMethod":false,"trialEnd":"2026-03-11T13:00:00.000Z","lastSyncedAt":"2026-03-09T11:59:00.000Z"}`)
	if err := a.db.Raw(`SELECT user_id FROM memberships WHERE organization_id = ?`, org).Row().Scan(&owner); err != nil {
		a.t.Fatal(err)
	}
	manager, member, billingUser = a.user("Manager", uuid.NewString()+"@example.com"), a.user("Member", uuid.NewString()+"@example.com"), a.user("Billing", uuid.NewString()+"@example.com")
	a.member(org, manager, "manager")
	a.member(org, member, "member")
	a.member(org, billingUser, "billing")
	return org, owner, manager, member, billingUser
}

// forma2's billing page: any member may view it, only roles other than member have billing rights, it shows the plan, the trial's remaining days (rounded up), the usage cycle's counts and the catalog, and it does not exist off Forma Cloud.
func TestBillingPage(t *testing.T) {
	a := newApp(t, func(c *config.Config) { c.Stripe.PublishableKey = "pk_test_123" })
	org, owner, _, member, _ := a.billedOrganization()
	a.stripe.catalogOn()
	ws := a.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'W') RETURNING id`, org)
	survey := a.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'S') RETURNING id`, ws)
	for _, at := range []string{"2026-03-01T00:00:00Z", "2026-03-09T11:00:00Z", "2026-02-28T23:59:59Z", "2026-04-01T00:00:00Z"} {
		a.exec(`INSERT INTO responses (survey_id, created_at) VALUES (?, ?)`, survey, at)
	}
	workflow := a.insert(`INSERT INTO workflows (workspace_id, name, definition) VALUES (?, 'W', '{}') RETURNING id`, ws)
	a.exec(`INSERT INTO workflow_runs (workflow_id, workspace_id, trigger_type, trigger_payload, is_dry_run, created_at) VALUES (?, ?, 'response.completed', '{}', false, '2026-03-05Z'), (?, ?, 'response.completed', '{}', true, '2026-03-05Z')`, workflow, ws, workflow, ws)

	var page Page
	a.ok(a.do(owner, http.MethodGet, billingPath(org, ""), nil), &page)
	if page.CurrentCloudPlan != PlanPro || *page.CurrentBillingInterval != IntervalMonthly || *page.CurrentSubscriptionStatus != "trialing" || page.TrialDaysRemaining == nil || *page.TrialDaysRemaining != 3 {
		t.Errorf("plan %+v", page)
	}
	if page.ResponseCount != 2 || page.WorkflowRunCount != 1 || page.WorkspaceCount != 1 || page.UsageCycleStart.Format("2006-01-02") != "2026-03-01" || page.UsageCycleEnd.Format("2006-01-02") != "2026-04-01" {
		t.Errorf("usage %+v", page)
	}
	if !page.HasBillingRights || page.IsStripeSetupIncomplete || page.IsPlanComparison || page.StripePublishableKey == nil || *page.StripePublishableKey != "pk_test_123" || page.Catalog.Scale.Yearly.UnitAmount == nil {
		t.Errorf("page %+v", page)
	}
	if *page.Billing.StripeCustomerID != "cus_123" || page.Billing.Stripe.Plan != PlanPro {
		t.Errorf("billing %+v", page.Billing)
	}
	a.ok(a.do(member, http.MethodGet, billingPath(org, ""), nil), &page)
	if page.HasBillingRights {
		t.Error("a member has billing rights")
	}
	a.fails(a.do(a.user("Stranger", "s@example.com"), http.MethodGet, billingPath(org, ""), nil), http.StatusNotFound, apperr.CodeNotFound)
	if len(a.stripe.requests(http.MethodGet, "/v1/subscriptions")) != 0 {
		t.Error("a fresh snapshot was synced")
	}
}

func TestBillingPageIsCloudOnly(t *testing.T) {
	a := newApp(t, func(c *config.Config) { c.IsFormaCloud = false })
	org, owner, _, _, _ := a.billedOrganization()
	a.fails(a.do(owner, http.MethodGet, billingPath(org, ""), nil), http.StatusNotFound, apperr.CodeNotFound)
	var s Summary
	a.ok(a.do(owner, http.MethodGet, billingPath(org, "/summary"), nil), &s)
	if s.IsFormaCloud || s.Usage != nil || s.Plan != PlanPro || *s.Limits.Workspaces != 1 {
		t.Fatalf("self-hosted summary %+v", s)
	}
}

// Every subscription action needs manage_billing (owners, managers and the billing role) and is audited as subscriptionAccessed; the onboarding plan step needs manage, which the billing role lacks.
func TestBillingActionsAuthorization(t *testing.T) {
	a := newApp(t)
	org, owner, manager, member, billingUser := a.billedOrganization()
	a.stripe.on("POST /v1/billing_portal/sessions", func(*http.Request) any { return map[string]any{"url": "https://billing.stripe.test/p"} })

	a.fails(a.do(member, http.MethodPost, billingPath(org, "/portal"), nil), http.StatusForbidden, apperr.CodeForbidden)
	for _, user := range []uuid.UUID{owner, manager, billingUser} {
		var res urlResponse
		a.ok(a.do(user, http.MethodPost, billingPath(org, "/portal"), nil), &res)
		if res.URL != "https://billing.stripe.test/p" {
			t.Fatalf("portal %+v", res)
		}
	}
	events := a.auditEvents()
	last := events[len(events)-1]
	if last["action"] != "subscriptionAccessed" || jsonString(t, last["target"]) != `{"id":"`+org.String()+`","type":"organization"}` || last["status"] != "success" || !strings.Contains(jsonString(t, last), "portalSessionCreated") {
		t.Fatalf("audit %v", last)
	}
	if events[0]["status"] != "failure" {
		t.Fatalf("the refused call was not audited: %v", events[0])
	}

	a.fails(a.do(billingUser, http.MethodPost, billingPath(org, "/trial"), nil), http.StatusForbidden, apperr.CodeForbidden)
	a.fails(a.do(billingUser, http.MethodPost, billingPath(org, "/hobby"), nil), http.StatusForbidden, apperr.CodeForbidden)
	a.fails(a.do(member, http.MethodPost, billingPath(org, "/plan"), map[string]string{"targetPlan": "hobby", "targetInterval": "monthly"}), http.StatusForbidden, apperr.CodeForbidden)
	a.fails(a.do(member, http.MethodGet, billingPath(org, "/upgrade-preview?targetPlan=pro&targetInterval=monthly"), nil), http.StatusForbidden, apperr.CodeForbidden)
}

// forma2's action preconditions: a paid checkout only without a subscription, a paid plan change only with a card, Hobby only monthly, and a customer for anything that touches Stripe.
func TestBillingActionPreconditions(t *testing.T) {
	a := newApp(t)
	org, owner, _, _, _ := a.billedOrganization()
	a.fails(a.do(owner, http.MethodPost, billingPath(org, "/checkout"), map[string]string{"targetPlan": "pro", "targetInterval": "monthly"}), http.StatusForbidden, CodePaidCheckoutRequiresNoSubscription)
	a.fails(a.do(owner, http.MethodPost, billingPath(org, "/checkout"), map[string]string{"targetPlan": "hobby", "targetInterval": "monthly"}), http.StatusBadRequest, apperr.CodeValidation)
	a.fails(a.do(owner, http.MethodPost, billingPath(org, "/plan"), map[string]string{"targetPlan": "scale", "targetInterval": "yearly"}), http.StatusForbidden, CodePaymentMethodRequired)
	a.fails(a.do(owner, http.MethodPost, billingPath(org, "/plan"), map[string]string{"targetPlan": "hobby", "targetInterval": "yearly"}), http.StatusBadRequest, apperr.CodeValidation)

	bare, bareOwner := a.organization()
	a.fails(a.do(bareOwner, http.MethodPost, billingPath(bare, "/portal"), nil), http.StatusNotFound, apperr.CodeNotFound)
	a.fails(a.do(bareOwner, http.MethodPost, billingPath(bare, "/setup-checkout"), map[string]string{}), http.StatusNotFound, apperr.CodeNotFound)
	if a.stripe.count() != 0 {
		t.Fatal("a refused action reached Stripe")
	}
}

// forma2's startProTrialAction: the customer created when missing, the trial, reconcile and sync, and AI smart tools granted before Stripe's entitlements say so.
func TestStartProTrialAction(t *testing.T) {
	a := newApp(t)
	org, owner := a.organization()
	a.stripe.on("POST /v1/customers", func(*http.Request) any { return map[string]any{"id": "cus_123"} })
	a.stripe.catalogOn()
	a.stripe.on("GET /v1/customers/{id}", func(*http.Request) any { return map[string]any{"id": "cus_123", "email": ""} })
	trial := subscription("sub_trial", "trialing", 5, subscriptionItem("si_1", price("price_pro_monthly", PlanPro, kindBase, IntervalMonthly), 0))
	created := false
	a.stripe.on("GET /v1/subscriptions", func(*http.Request) any {
		if created {
			return list(trial)
		}
		return list()
	})
	a.stripe.on("POST /v1/subscriptions", func(*http.Request) any { created = true; return trial })
	a.stripe.on("GET /v1/entitlements/active_entitlements", func(*http.Request) any { return list() })

	var res successResponse
	a.ok(a.do(owner, http.MethodPost, billingPath(org, "/trial"), nil), &res)
	rec := a.record(org)
	if rec.Stripe.Plan != PlanPro || *rec.Stripe.SubscriptionStatus != "trialing" || len(rec.Stripe.Features) != 1 || rec.Stripe.Features[0] != FeatureAISmartTools {
		t.Fatalf("after the trial %+v", rec.Stripe)
	}
	if a.stripe.only(http.MethodPost, "/v1/subscriptions").Form.Get("trial_period_days") != "14" {
		t.Fatal("trial length")
	}
	events := a.auditEvents()
	if last := events[len(events)-1]; last["action"] != "subscriptionAccessed" || !strings.Contains(jsonString(t, last), "trialDurationDays") {
		t.Fatalf("audit %v", last)
	}
}

// forma2's wait actions: sync until the plan (or the card) shows, at most five times.
func TestWaitActions(t *testing.T) {
	a := newApp(t)
	org, owner, _, _, _ := a.billedOrganization()
	syncs := 0
	pro := subscription("sub_1", "active", 5, subscriptionItem("si_1", price("price_pro_monthly", PlanPro, kindBase, IntervalMonthly), 0))
	a.stripe.on("GET /v1/subscriptions", func(*http.Request) any {
		syncs++
		if syncs < 3 {
			return list()
		}
		return list(pro)
	})
	a.stripe.on("GET /v1/products/{id}", func(r *http.Request) any { return product(strings.TrimPrefix(r.PathValue("id"), "prod_")) })
	a.stripe.on("GET /v1/entitlements/active_entitlements", func(*http.Request) any { return list() })
	a.stripe.on("GET /v1/customers/{id}", func(*http.Request) any { return map[string]any{"id": "cus_123"} })

	var plan struct{ Plan *string }
	a.ok(a.do(owner, http.MethodPost, billingPath(org, "/wait/plan"), map[string]string{"targetPlan": "pro"}), &plan)
	if plan.Plan == nil || *plan.Plan != PlanPro || syncs != 3 {
		t.Fatalf("plan %v after %d syncs", plan.Plan, syncs)
	}
	var card struct{ HasPaymentMethod bool }
	a.ok(a.do(owner, http.MethodPost, billingPath(org, "/wait/payment-method"), nil), &card)
	if card.HasPaymentMethod || syncs != 8 {
		t.Fatalf("payment method %v after %d syncs", card.HasPaymentMethod, syncs)
	}
}

// The summary the layouts read: limits and the usage cycle's counts on Forma Cloud.
func TestBillingSummary(t *testing.T) {
	a := newApp(t)
	org, _, _, member, _ := a.billedOrganization()
	var s Summary
	a.ok(a.do(member, http.MethodGet, billingPath(org, "/summary"), nil), &s)
	if !s.IsFormaCloud || s.Plan != PlanPro || s.Usage == nil || s.Usage.Responses != 0 || *s.Limits.Monthly.Responses != 250 || s.TrialDaysRemaining == nil || *s.TrialDaysRemaining != 3 || *s.TrialEnd != "2026-03-11T13:00:00.000Z" || s.ProTrialDays != proTrialDays {
		t.Fatalf("summary %+v", s)
	}
}
