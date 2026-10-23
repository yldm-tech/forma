package billing

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/analytics/analyticstest"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// proSubscription is an active Pro monthly subscription whose period runs from 1739923200 to 1742515200, forma2's plan-change fixture.
func proSubscription(status string) map[string]any {
	sub := subscription("sub_1", status, 1739923200,
		subscriptionItem("si_pro_base", price("price_pro_monthly", PlanPro, kindBase, IntervalMonthly), 1742515200),
		subscriptionItem("si_pro_responses", price("price_pro_responses", PlanPro, kindResponses, IntervalMonthly), 1742515200))
	sub["cancel_at_period_end"], sub["schedule"] = false, nil
	return sub
}

// planEnv answers the current subscription, the catalog and the customer (with the given default card).
func planEnv(t *testing.T, sub map[string]any, customerCard any) (*env, uuid.UUID) {
	e := newEnv(t)
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any { return list(sub) })
	e.stripe.catalogOn()
	e.stripe.on("GET /v1/customers/{id}", func(*http.Request) any {
		return map[string]any{"id": "cus_123", "object": "customer", "invoice_settings": map[string]any{"default_payment_method": customerCard}}
	})
	return e, e.customerOrganization(`{"subscriptionId":"sub_1","plan":"pro","interval":"monthly","hasPaymentMethod":true}`)
}

func notAllowed(t *testing.T, err error, code string) {
	t.Helper()
	if ae, ok := apperr.As(err); !ok || ae.Code != code || ae.Status != http.StatusForbidden {
		t.Fatalf("error %v, want %s", err, code)
	}
}

// forma2's "persists pending downgrade snapshot immediately": a downgrade becomes a schedule from the subscription, whose next phase starts when the current period ends.
func TestScheduledDowngrade(t *testing.T) {
	e, org := planEnv(t, proSubscription("active"), nil)
	e.stripe.on("POST /v1/subscription_schedules", func(*http.Request) any {
		return map[string]any{"id": "sched_new", "object": "subscription_schedule", "current_phase": map[string]any{"start_date": 1739923200, "end_date": 1742515200}}
	})
	e.stripe.on("POST /v1/subscription_schedules/sched_new", func(*http.Request) any {
		return map[string]any{"id": "sched_new", "phases": []any{map[string]any{"start_date": 1739923200, "end_date": 1742515200}, map[string]any{"start_date": 1742515200, "end_date": 1745193600}}}
	})
	change, err := e.engine.SwitchPlan(context.Background(), org, "cus_123", PlanHobby, IntervalMonthly)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"mode":"scheduled","pendingChange":{"type":"plan_change","targetPlan":"hobby","targetInterval":"monthly","effectiveAt":"2025-03-21T00:00:00.000Z"},"clientSecret":null,"requiresAction":false}`
	if got := jsonString(t, change); got != want {
		t.Fatalf("result %s", got)
	}
	if f := e.stripe.only(http.MethodPost, "/v1/subscription_schedules").Form; f.Get("from_subscription") != "sub_1" || len(f) != 1 {
		t.Errorf("schedule create %v", f)
	}
	f := e.stripe.only(http.MethodPost, "/v1/subscription_schedules/sched_new").Form
	for k, v := range map[string]string{
		"end_behavior": "release", "proration_behavior": "none", "metadata[organizationId]": org.String(),
		"phases[0][start_date]": "1739923200", "phases[0][end_date]": "1742515200",
		"phases[0][items][0][price]": "price_pro_monthly", "phases[0][items][0][quantity]": "1", "phases[0][items][1][price]": "price_pro_responses",
		"phases[1][start_date]": "1742515200", "phases[1][items][0][price]": "price_hobby_monthly", "phases[1][items][0][quantity]": "1",
		"phases[1][metadata][organizationId]": org.String(), "phases[1][metadata][targetPlan]": "hobby", "phases[1][metadata][targetInterval]": "monthly",
	} {
		if f.Get(k) != v {
			t.Errorf("schedule update %s = %q, want %q", k, f.Get(k), v)
		}
	}
	if f.Has("phases[0][items][1][quantity]") || f.Has("phases[1][items][1][price]") {
		t.Errorf("schedule update items %v", f)
	}
	if s := e.record(org).Stripe; jsonString(t, s.PendingChange) != jsonString(t, change.PendingChange) || s.Plan != PlanPro || !*s.HasPaymentMethod {
		t.Fatalf("snapshot %+v", s)
	}
}

// forma2's schedule failure tests: a schedule created for the change is released and a cleared cancellation at period end restored, and the update's error is the one returned; an existing schedule is updated in place and kept.
func TestScheduledDowngradeFailure(t *testing.T) {
	t.Run("a created schedule", func(t *testing.T) {
		sub := proSubscription("active")
		sub["cancel_at_period_end"] = true
		e, org := planEnv(t, sub, nil)
		e.stripe.on("POST /v1/subscriptions/sub_1", func(*http.Request) any { return sub })
		e.stripe.on("POST /v1/subscription_schedules", func(*http.Request) any {
			return map[string]any{"id": "sched_new", "current_phase": map[string]any{"start_date": 1739923200, "end_date": 1742515200}}
		})
		e.stripe.fail("POST /v1/subscription_schedules/sched_new", http.StatusBadRequest, "parameter_invalid", "")
		e.stripe.on("POST /v1/subscription_schedules/sched_new/release", func(*http.Request) any { return map[string]any{"id": "sched_new"} })
		if _, err := e.engine.SwitchPlan(context.Background(), org, "cus_123", PlanHobby, IntervalMonthly); err == nil {
			t.Fatal("the failed schedule update succeeded")
		}
		if f := e.stripe.only(http.MethodPost, "/v1/subscription_schedules/sched_new/release").Form; f.Get("preserve_cancel_date") != "false" {
			t.Errorf("release %v", f)
		}
		updates := e.stripe.requests(http.MethodPost, "/v1/subscriptions/sub_1")
		if len(updates) != 2 || updates[0].Form.Get("cancel_at_period_end") != "false" || updates[1].Form.Get("cancel_at_period_end") != "true" {
			t.Fatalf("cancel at period end updates %v", updates)
		}
		if e.record(org).Stripe.PendingChange != nil {
			t.Fatal("a failed change was recorded")
		}
	})

	// A schedule the subscription already had is updated in place, and kept when the update fails.
	scheduled := proSubscription("active")
	scheduled["schedule"] = "sched_old"
	t.Run("an existing schedule", func(t *testing.T) {
		e2, org2 := planEnv(t, scheduled, nil)
		e2.stripe.on("GET /v1/subscription_schedules/sched_old", func(*http.Request) any {
			return map[string]any{"id": "sched_old", "current_phase": map[string]any{"start_date": 1739923200, "end_date": 1742515200}}
		})
		e2.stripe.fail("POST /v1/subscription_schedules/sched_old", http.StatusBadRequest, "parameter_invalid", "")
		if _, err := e2.engine.SwitchPlan(context.Background(), org2, "cus_123", PlanHobby, IntervalMonthly); err == nil {
			t.Fatal("the failed schedule update succeeded")
		}
		if len(e2.stripe.requests(http.MethodPost, "/v1/subscription_schedules")) != 0 || len(e2.stripe.requests(http.MethodPost, "/v1/subscription_schedules/sched_old/release")) != 0 {
			t.Fatal("the existing schedule was replaced or released")
		}
	})

	// A schedule whose current phase has no end cannot carry a change.
	t.Run("a phase without an end", func(t *testing.T) {
		e3, org3 := planEnv(t, scheduled, nil)
		e3.stripe.on("GET /v1/subscription_schedules/sched_old", func(*http.Request) any {
			return map[string]any{"id": "sched_old", "current_phase": map[string]any{"start_date": 1739923200, "end_date": nil}}
		})
		if _, err := e3.engine.SwitchPlan(context.Background(), org3, "cus_123", PlanHobby, IntervalMonthly); err == nil || len(e3.stripe.requests(http.MethodPost, "/v1/subscription_schedules/sched_old")) != 0 {
			t.Fatalf("a schedule without a phase end: %v", err)
		}
	})
}

// forma2's trial-to-Hobby tests: immediate and free, card or no card; a stray schedule is released and a cancellation cleared first, and ending the trial without a card must not cancel the subscription.
func TestTrialToHobby(t *testing.T) {
	sub := proSubscription("trialing")
	sub["schedule"], sub["cancel_at_period_end"] = "sched_stray", true
	e, org := planEnv(t, sub, nil)
	e.exec(`UPDATE organization_billing SET stripe = stripe || '{"pendingChange":{"type":"plan_change","targetPlan":"hobby","targetInterval":"monthly","effectiveAt":"x"}}' WHERE organization_id = ?`, org)
	e.stripe.on("POST /v1/subscription_schedules/sched_stray/release", func(*http.Request) any { return map[string]any{"id": "sched_stray"} })
	e.stripe.on("POST /v1/subscriptions/sub_1", func(*http.Request) any { return sub })
	change, err := e.engine.SwitchPlan(context.Background(), org, "cus_123", PlanHobby, IntervalMonthly)
	if err != nil || change.Mode != ModeImmediate || change.PendingChange != nil {
		t.Fatalf("%+v %v", change, err)
	}
	e.stripe.only(http.MethodPost, "/v1/subscription_schedules/sched_stray/release")
	updates := e.stripe.requests(http.MethodPost, "/v1/subscriptions/sub_1")
	if len(updates) != 2 || updates[0].Form.Get("cancel_at_period_end") != "false" {
		t.Fatalf("updates %v", updates)
	}
	f := updates[1].Form
	for k, v := range map[string]string{
		"items[0][id]": "si_pro_base", "items[0][deleted]": "true", "items[1][id]": "si_pro_responses", "items[1][deleted]": "true",
		"items[2][price]": "price_hobby_monthly", "items[2][quantity]": "1", "trial_end": "now", "proration_behavior": "none",
		"trial_settings[end_behavior][missing_payment_method]": "create_invoice",
	} {
		if f.Get(k) != v {
			t.Errorf("switch %s = %q, want %q", k, f.Get(k), v)
		}
	}
	if len(e.stripe.requests(http.MethodGet, "/v1/customers/cus_123")) != 0 {
		t.Error("a card was required to leave a trial for Hobby")
	}
	if e.record(org).Stripe.PendingChange != nil {
		t.Fatal("the pending change survived")
	}
}

// forma2's trial conversion tests: no card on file refuses, a card only on the customer counts, converting to the plan being trialled still charges, and the single update ends the trial and invoices synchronously.
func TestTrialConversion(t *testing.T) {
	t.Run("no card", func(t *testing.T) {
		e, org := planEnv(t, proSubscription("trialing"), nil)
		_, err := e.engine.SwitchPlan(context.Background(), org, "cus_123", PlanScale, IntervalMonthly)
		notAllowed(t, err, CodePaymentMethodRequired)
		if len(e.stripe.requests(http.MethodPost, "/v1/subscriptions/sub_1")) != 0 {
			t.Fatal("a trial without a card was converted")
		}
	})

	t.Run("a card on the customer", func(t *testing.T) {
		sub := proSubscription("trialing")
		sub["cancel_at_period_end"] = true
		e2, org2 := planEnv(t, sub, "pm_on_customer")
		e2.exec(`UPDATE organization_billing SET stripe = stripe || '{"pendingChange":{"type":"plan_change","targetPlan":"hobby","targetInterval":"monthly","effectiveAt":"x"}}' WHERE organization_id = ?`, org2)
		e2.stripe.on("POST /v1/subscriptions/sub_1", func(*http.Request) any { return sub })
		change, err := e2.engine.SwitchPlan(context.Background(), org2, "cus_123", PlanPro, IntervalMonthly)
		if err != nil || change.Mode != ModeImmediate || change.RequiresAction || change.ClientSecret != nil {
			t.Fatalf("%+v %v", change, err)
		}
		updates := e2.stripe.requests(http.MethodPost, "/v1/subscriptions/sub_1")
		if len(updates) != 3 || updates[0].Form.Get("cancel_at_period_end") != "false" || updates[2].Form.Get("cancel_at_period_end") != "false" {
			t.Fatalf("updates %v", updates)
		}
		f := updates[1].Form
		for k, v := range map[string]string{
			"items[0][id]": "si_pro_base", "items[0][deleted]": "true", "items[2][price]": "price_pro_monthly", "items[2][quantity]": "1", "items[3][price]": "price_pro_responses",
			"trial_end": "now", "proration_behavior": "always_invoice", "payment_behavior": "error_if_incomplete",
		} {
			if f.Get(k) != v {
				t.Errorf("conversion %s = %q, want %q", k, f.Get(k), v)
			}
		}
		if e2.record(org2).Stripe.PendingChange != nil {
			t.Fatal("the pending Hobby move survived the conversion")
		}
	})

	// A card that needs 3D Secure cannot pass off-session; the update rolls back and the caller is told to use another card.
	card := proSubscription("trialing")
	card["default_payment_method"] = "pm_1"
	t.Run("3D Secure", func(t *testing.T) {
		e3, org3 := planEnv(t, card, nil)
		e3.stripe.fail("POST /v1/subscriptions/sub_1", http.StatusPaymentRequired, "card_declined", "authentication_required")
		_, err := e3.engine.SwitchPlan(context.Background(), org3, "cus_123", PlanScale, IntervalYearly)
		notAllowed(t, err, CodeCardAuthenticationRequired)
	})

	// Any other decline propagates and leaves the trial as it was.
	t.Run("a decline", func(t *testing.T) {
		e4, org4 := planEnv(t, card, nil)
		e4.stripe.fail("POST /v1/subscriptions/sub_1", http.StatusPaymentRequired, "card_declined", "insufficient_funds")
		if _, err := e4.engine.SwitchPlan(context.Background(), org4, "cus_123", PlanScale, IntervalYearly); err == nil || errors.As(err, new(*apperr.Error)) {
			t.Fatalf("a declined card: %v", err)
		}
	})
}

// forma2's "uses pending_if_incomplete for immediate upgrades": the plan is granted once the invoice is paid, and an invoice needing 3D Secure hands its client secret to the browser.
func TestImmediateUpgrade(t *testing.T) {
	e, org := planEnv(t, proSubscription("active"), nil)
	e.stripe.on("POST /v1/subscriptions/sub_1", func(*http.Request) any {
		sub := proSubscription("active")
		sub["latest_invoice"] = "in_1"
		return sub
	})
	e.stripe.on("GET /v1/invoices/in_1", func(*http.Request) any {
		return map[string]any{"id": "in_1", "object": "invoice", "confirmation_secret": map[string]any{"client_secret": "pi_1_secret", "type": "payment_intent"}}
	})
	change, err := e.engine.SwitchPlan(context.Background(), org, "cus_123", PlanScale, IntervalYearly)
	if err != nil || change.Mode != ModeImmediate || !change.RequiresAction || change.ClientSecret == nil || *change.ClientSecret != "pi_1_secret" {
		t.Fatalf("%+v %v", change, err)
	}
	f := e.stripe.only(http.MethodPost, "/v1/subscriptions/sub_1").Form
	if f.Get("payment_behavior") != "pending_if_incomplete" || f.Get("proration_behavior") != "always_invoice" || f.Has("trial_end") || f.Get("items[2][price]") != "price_scale_yearly" || f.Get("items[3][price]") != "price_scale_responses" {
		t.Errorf("upgrade %v", f)
	}
	if e.stripe.only(http.MethodGet, "/v1/invoices/in_1").Form.Get("expand[0]") != "confirmation_secret" {
		t.Error("the invoice was read without its confirmation secret")
	}

	// forma2's "returns early for the current selection without disturbing pending state".
	calls := e.stripe.count()
	if change, err := e.engine.SwitchPlan(context.Background(), org, "cus_123", PlanPro, IntervalMonthly); err != nil || change.Mode != ModeImmediate || e.stripe.count() != calls+2 {
		t.Fatalf("same selection %+v %v, %d calls", change, err, e.stripe.count()-calls)
	}
}

// forma2's previewImmediateUpgradeCharge tests: Stripe's invoice total, trial_end only for a trial, nothing when Stripe cannot price it.
func TestPreviewUpgrade(t *testing.T) {
	for _, status := range []string{"active", "trialing"} {
		t.Run(status, func(t *testing.T) {
			e, org := planEnv(t, proSubscription(status), nil)
			e.stripe.on("POST /v1/invoices/create_preview", func(*http.Request) any { return map[string]any{"amount_due": 4900, "currency": "usd"} })
			preview, err := e.engine.PreviewUpgrade(context.Background(), org, "cus_123", PlanScale, IntervalMonthly)
			if err != nil || preview == nil || preview.AmountDue != 4900 || preview.Currency != "usd" {
				t.Fatalf("%s preview %+v %v", status, preview, err)
			}
			f := e.stripe.only(http.MethodPost, "/v1/invoices/create_preview").Form
			if f.Get("customer") != "cus_123" || f.Get("subscription") != "sub_1" || f.Get("subscription_details[proration_behavior]") != "always_invoice" || f.Get("subscription_details[items][0][deleted]") != "true" || f.Get("subscription_details[items][2][price]") != "price_scale_monthly" {
				t.Errorf("%s preview params %v", status, f)
			}
			if got := f.Get("subscription_details[trial_end]"); (status == "trialing") != (got == "now") {
				t.Errorf("%s trial_end = %q", status, got)
			}
		})
	}
	t.Run("unpriceable", func(t *testing.T) {
		e, org := planEnv(t, proSubscription("active"), nil)
		e.stripe.fail("POST /v1/invoices/create_preview", http.StatusBadRequest, "invoice_upcoming_none", "")
		if preview, err := e.engine.PreviewUpgrade(context.Background(), org, "cus_123", PlanScale, IntervalMonthly); err != nil || preview != nil {
			t.Fatalf("failed preview %+v %v", preview, err)
		}
	})
}

// forma2's "undoPendingOrganizationPlanChange clears the pending snapshot".
func TestUndoPendingChange(t *testing.T) {
	sub := proSubscription("active")
	sub["schedule"] = map[string]any{"id": "sched_1", "object": "subscription_schedule"}
	e, org := planEnv(t, sub, nil)
	e.exec(`UPDATE organization_billing SET stripe = stripe || '{"pendingChange":{"type":"plan_change","targetPlan":"hobby","targetInterval":"monthly","effectiveAt":"x"}}' WHERE organization_id = ?`, org)
	e.stripe.on("POST /v1/subscription_schedules/sched_1/release", func(*http.Request) any { return map[string]any{"id": "sched_1"} })
	if err := e.engine.UndoPendingChange(context.Background(), org, "cus_123"); err != nil {
		t.Fatal(err)
	}
	if e.stripe.only(http.MethodPost, "/v1/subscription_schedules/sched_1/release").Form.Get("preserve_cancel_date") != "false" || e.record(org).Stripe.PendingChange != nil {
		t.Fatal("the pending change was not undone")
	}
}

// forma2's applySetupCheckoutUpgrade tests: the saved card is attached and the upgrade applied; a session of another organization is refused; a session without a paid target changes nothing. Sessions forma2 created name the organization by its forma2 id.
func TestApplySetupCheckoutUpgrade(t *testing.T) {
	e := newEnv(t)
	org := e.customerOrganization(`{"subscriptionId":"sub_1","plan":"hobby"}`)
	hobby := subscription("sub_1", "active", 1739923200, subscriptionItem("si_hobby", price("price_hobby_monthly", PlanHobby, kindBase, IntervalMonthly), 1742515200))
	hobby["default_payment_method"] = "pm_saved"
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any { return list(hobby) })
	e.stripe.catalogOn()
	session := map[string]any{
		"id": "cs_1", "object": "checkout.session", "mode": "setup", "status": "complete", "customer": "cus_123",
		"setup_intent": map[string]any{"id": "seti_1", "object": "setup_intent", "payment_method": "pm_saved"},
		"metadata":     map[string]any{"organizationId": org.String(), "subscriptionId": "sub_1", "targetPlan": "pro", "targetInterval": "yearly"},
	}
	e.stripe.on("GET /v1/checkout/sessions/{id}", func(*http.Request) any { return session })
	e.stripe.on("POST /v1/customers/cus_123", func(*http.Request) any { return map[string]any{"id": "cus_123"} })
	e.stripe.on("POST /v1/subscriptions/sub_1", func(*http.Request) any { return hobby })
	result, err := e.engine.ApplySetupCheckoutUpgrade(context.Background(), org, "cs_1")
	if err != nil || jsonString(t, result) != `{"mode":"immediate","clientSecret":null,"requiresAction":false,"targetPlan":"pro"}` {
		t.Fatalf("%s %v", jsonString(t, result), err)
	}
	if e.stripe.only(http.MethodGet, "/v1/checkout/sessions/cs_1").Form.Get("expand[0]") != "setup_intent" {
		t.Error("the session was read without its setup intent")
	}
	if e.stripe.only(http.MethodPost, "/v1/customers/cus_123").Form.Get("invoice_settings[default_payment_method]") != "pm_saved" {
		t.Error("the card did not become the customer's default")
	}
	updates := e.stripe.requests(http.MethodPost, "/v1/subscriptions/sub_1")
	if len(updates) != 2 || updates[0].Form.Get("default_payment_method") != "pm_saved" || updates[1].Form.Get("items[1][price]") != "price_pro_yearly" {
		t.Fatalf("updates %v", updates)
	}

	session["metadata"].(map[string]any)["organizationId"] = uuid.NewString()
	_, err = e.engine.ApplySetupCheckoutUpgrade(context.Background(), org, "cs_1")
	notAllowed(t, err, CodeCheckoutSessionMismatch)

	session["metadata"] = map[string]any{"organizationId": "clxlegacyorg0000000000000"}
	if result, err := e.engine.ApplySetupCheckoutUpgrade(context.Background(), legacyid.ID("clxlegacyorg0000000000000"), "cs_1"); err != nil || result.TargetPlan != nil {
		t.Fatalf("a forma2 session without a target: %+v %v", result, err)
	}
	if len(e.stripe.requests(http.MethodPost, "/v1/subscriptions/sub_1")) != 2 {
		t.Fatal("a session without a target changed the subscription")
	}
}

// forma2's createProTrialSubscription tests: a fourteen-day Pro monthly trial that cancels without a card, once per email address.
func TestStartProTrial(t *testing.T) {
	e := newEnv(t)
	org := e.customerOrganization(`{}`)
	e.stripe.catalogOn()
	e.stripe.on("GET /v1/customers/{id}", func(*http.Request) any { return map[string]any{"id": "cus_123", "email": "ada@example.com"} })
	e.stripe.on("GET /v1/customers", func(*http.Request) any { return list(map[string]any{"id": "cus_123"}, map[string]any{"id": "cus_old"}) })
	trialled := false
	e.stripe.on("GET /v1/subscriptions", func(r *http.Request) any {
		if r.Form.Get("customer") == "cus_old" && trialled {
			s := subscription("sub_old", "canceled", 1, subscriptionItem("si", price("price_pro_monthly", PlanPro, kindBase, IntervalMonthly), 0))
			s["trial_start"] = 1
			return list(s)
		}
		return list()
	})
	e.stripe.on("POST /v1/subscriptions", func(*http.Request) any { return map[string]any{"id": "sub_trial"} })
	if err := e.engine.StartProTrial(context.Background(), org, "cus_123", proTrialDays); err != nil {
		t.Fatal(err)
	}
	if f := e.stripe.only(http.MethodGet, "/v1/customers").Form; f.Get("email") != "ada@example.com" {
		t.Errorf("customer lookup %v", f)
	}
	call := e.stripe.only(http.MethodPost, "/v1/subscriptions")
	for k, v := range map[string]string{
		"customer": "cus_123", "items[0][price]": "price_pro_monthly", "items[0][quantity]": "1", "items[1][price]": "price_pro_responses", "trial_period_days": "14",
		"trial_settings[end_behavior][missing_payment_method]": "cancel", "payment_settings[save_default_payment_method]": "on_subscription", "metadata[organizationId]": org.String(),
	} {
		if call.Form.Get(k) != v {
			t.Errorf("trial %s = %q, want %q", k, call.Form.Get(k), v)
		}
	}
	if call.IdempotencyKey != "create-pro-trial-"+org.String() {
		t.Errorf("idempotency key %s", call.IdempotencyKey)
	}

	trialled = true
	notAllowed(t, e.engine.StartProTrial(context.Background(), org, "cus_123", proTrialDays), CodeTrialAlreadyUsed)
	if len(e.stripe.requests(http.MethodPost, "/v1/subscriptions")) != 1 {
		t.Fatal("a second trial was created for the same email")
	}
}

// forma2's checkout sessions: a subscription Checkout for a plan, a setup Checkout for a card with or without an upgrade intent, and the customer portal, each returning to the billing page.
func TestCheckoutSessions(t *testing.T) {
	e := newEnv(t)
	org := e.customerOrganization(`{}`)
	ctx := context.Background()
	e.stripe.catalogOn()
	e.stripe.on("POST /v1/checkout/sessions", func(*http.Request) any {
		return map[string]any{"id": "cs_1", "url": "https://checkout.stripe.test/cs_1"}
	})
	e.stripe.on("GET /v1/subscriptions/sub_1", func(*http.Request) any { return map[string]any{"id": "sub_1", "currency": "eur"} })
	e.stripe.on("POST /v1/billing_portal/sessions", func(*http.Request) any { return map[string]any{"id": "bps_1", "url": "https://billing.stripe.test/p"} })
	billing := webApp + "/organizations/" + org.String() + "/settings/billing"

	if url, err := e.engine.PlanCheckoutURL(ctx, org, "cus_123", PlanScale, IntervalMonthly); err != nil || url != "https://checkout.stripe.test/cs_1" {
		t.Fatalf("checkout %q %v", url, err)
	}
	f := e.stripe.requests(http.MethodPost, "/v1/checkout/sessions")[0].Form
	for k, v := range map[string]string{
		"mode": "subscription", "customer": "cus_123", "line_items[0][price]": "price_scale_monthly", "line_items[0][quantity]": "1", "line_items[1][price]": "price_scale_responses",
		"client_reference_id": org.String(), "billing_address_collection": "required", "tax_id_collection[enabled]": "true", "tax_id_collection[required]": "if_supported",
		"customer_update[address]": "auto", "customer_update[name]": "auto", "cancel_url": billing,
		"success_url":          webApp + "/billing-confirmation?organizationId=" + org.String() + "&checkout_success=1&plan=scale",
		"metadata[targetPlan]": "scale", "metadata[targetInterval]": "monthly", "metadata[organizationId]": org.String(), "subscription_data[metadata][organizationId]": org.String(),
	} {
		if f.Get(k) != v {
			t.Errorf("checkout %s = %q, want %q", k, f.Get(k), v)
		}
	}
	if f.Has("line_items[1][quantity]") {
		t.Error("the metered line item has a quantity")
	}

	if _, err := e.engine.SetupCheckoutURL(ctx, org, "cus_123", "sub_1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.engine.SetupCheckoutURL(ctx, org, "cus_123", "sub_1", &UpgradeIntent{Plan: PlanPro, Interval: IntervalMonthly}); err != nil {
		t.Fatal(err)
	}
	setups := e.stripe.requests(http.MethodPost, "/v1/checkout/sessions")[1:]
	if f := setups[0].Form; f.Get("mode") != "setup" || f.Get("currency") != "eur" || f.Get("success_url") != billing+"?checkout_success=1" || f.Get("cancel_url") != billing || f.Get("metadata[subscriptionId]") != "sub_1" || f.Has("metadata[targetPlan]") || f.Get("tax_id_collection[enabled]") != "true" {
		t.Errorf("payment-only setup %v", f)
	}
	if f := setups[1].Form; f.Get("success_url") != billing+"?checkout_success=1&upgrade_pending=1&session_id={CHECKOUT_SESSION_ID}" || f.Get("metadata[targetPlan]") != "pro" || f.Get("metadata[targetInterval]") != "monthly" {
		t.Errorf("upgrade setup %v", f)
	}

	if url, err := e.engine.PortalURL(ctx, org, "cus_123"); err != nil || url != "https://billing.stripe.test/p" {
		t.Fatalf("portal %q %v", url, err)
	}
	if f := e.stripe.only(http.MethodPost, "/v1/billing_portal/sessions").Form; f.Get("customer") != "cus_123" || f.Get("return_url") != billing {
		t.Errorf("portal %v", f)
	}
}

// forma2's "rejects mixed-interval yearly checkout": a yearly base with a monthly metered price cannot be one Checkout.
func TestCheckoutRejectsMixedIntervals(t *testing.T) {
	e := newEnv(t)
	org := e.customerOrganization(`{}`)
	prices := standardPrices()
	// A yearly price tagged monthly by metadata keeps its yearly recurring interval.
	yearlyAsMonthly := price("price_scale_monthly", PlanScale, kindBase, IntervalMonthly)
	yearlyAsMonthly["recurring"].(map[string]any)["interval"] = "year"
	prices[4] = yearlyAsMonthly
	e.stripe.catalogOn(prices...)
	_, err := e.engine.PlanCheckoutURL(context.Background(), org, "cus_123", PlanScale, IntervalMonthly)
	notAllowed(t, err, CodeMixedIntervalCheckout)
}

// forma2's getProTrialDays: fourteen days, seven for the organizations PostHog puts in the a-b_billing_shorten-trial-days experiment's test variant. Not parallel: the flag is answered by the process-wide PostHog client.
func TestTrialDaysFollowTheExperiment(t *testing.T) {
	org := uuid.New()
	if days := trialDays(org); days != proTrialDays {
		t.Fatalf("trial without PostHog = %d days, want %d", days, proTrialDays)
	}
	rec := analyticstest.Start(t)
	rec.SetFlag("a-b_billing_shorten-trial-days", "control")
	if days := trialDays(org); days != proTrialDays {
		t.Fatalf("control variant = %d days, want %d", days, proTrialDays)
	}
	rec.SetFlag("a-b_billing_shorten-trial-days", "test")
	if days := trialDays(org); days != shortenedProTrialDays {
		t.Fatalf("test variant = %d days, want %d", days, shortenedProTrialDays)
	}
}

// forma2's billing page: the plan comparison table is shown to the users PostHog puts in the a-b_billing_plan-comparison-table experiment's test variant. Not parallel: the flag is answered by the process-wide PostHog client.
func TestPlanComparisonFollowsTheExperiment(t *testing.T) {
	user := uuid.New()
	if isPlanComparison(user) {
		t.Fatal("plan comparison without PostHog")
	}
	rec := analyticstest.Start(t)
	rec.SetFlag("a-b_billing_plan-comparison-table", "control")
	if isPlanComparison(user) {
		t.Fatal("plan comparison for the control variant")
	}
	rec.SetFlag("a-b_billing_plan-comparison-table", "test")
	if !isPlanComparison(user) {
		t.Fatal("no plan comparison for the test variant")
	}
}
