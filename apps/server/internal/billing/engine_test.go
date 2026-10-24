package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func i64(v int64) *int64 { return &v }

// forma2's ensureStripeCustomerForOrganization tests: nothing for a missing organization, the stored customer reused, and a new one created for the owner and stored without touching an existing snapshot.
func TestEnsureCustomer(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if id, err := e.engine.EnsureCustomer(ctx, uuid.New()); err != nil || id != "" {
		t.Fatalf("missing organization: %q %v", id, err)
	}
	existing := e.customerOrganization(`{"plan":"pro"}`)
	if id, err := e.engine.EnsureCustomer(ctx, existing); err != nil || id != "cus_123" || e.stripe.count() != 0 {
		t.Fatalf("existing customer: %q %v, %d Stripe calls", id, err, e.stripe.count())
	}

	org, owner := e.organization()
	var email string
	if err := e.db.Raw(`SELECT email FROM users WHERE id = ?`, owner).Row().Scan(&email); err != nil {
		t.Fatal(err)
	}
	created := 0
	e.stripe.on("POST /v1/customers", func(*http.Request) any {
		created++
		return map[string]any{"id": "cus_new" + strings.Repeat("x", created-1), "object": "customer"}
	})
	if id, err := e.engine.EnsureCustomer(ctx, org); err != nil || id != "cus_new" {
		t.Fatalf("new customer: %q %v", id, err)
	}
	call := e.stripe.only(http.MethodPost, "/v1/customers")
	if call.Form.Get("email") != email || call.Form.Get("name") != "Ada Owner" || call.Form.Get("metadata[organizationId]") != org.String() || call.Form.Get("metadata[organizationName]") != "Acme" || call.IdempotencyKey != "ensure-customer-"+org.String() {
		t.Fatalf("customer params %v %s", call.Form, call.IdempotencyKey)
	}
	rec := e.record(org)
	if rec.StripeCustomerID == nil || *rec.StripeCustomerID != "cus_new" || rec.Stripe == nil || rec.Stripe.Plan != PlanHobby || rec.Stripe.LastSyncedAt == nil {
		t.Fatalf("stored %+v", rec)
	}
	if !reflect.DeepEqual(rec.Limits, DefaultLimits(true)) {
		t.Fatalf("limits %+v", rec.Limits)
	}

	// A row that exists keeps its snapshot; only the customer is filled in.
	kept, _ := e.organization()
	e.exec(`INSERT INTO organization_billing (organization_id, limits, stripe) VALUES (?, '{"workspaces":1,"monthly":{"responses":250}}', '{"plan":"scale","subscriptionId":"sub_9"}')`, kept)
	if id, err := e.engine.EnsureCustomer(ctx, kept); err != nil || id != "cus_newx" {
		t.Fatalf("customer for an existing row: %q %v", id, err)
	}
	if rec := e.record(kept); rec.Stripe.Plan != PlanScale || *rec.Stripe.SubscriptionID != "sub_9" {
		t.Fatalf("the snapshot was overwritten: %+v", rec.Stripe)
	}

	// An organization without an owner gets no customer.
	orphan := e.insert(`INSERT INTO organizations (name) VALUES ('Orphan') RETURNING id`)
	if id, err := e.engine.EnsureCustomer(ctx, orphan); err != nil || id != "" {
		t.Fatalf("ownerless organization: %q %v", id, err)
	}
}

// forma2's "stores normalized stripe snapshot": plan, interval, status, subscription, payment method, deduplicated features, event bookkeeping, usage cycle anchor and entitlement limits.
func TestSyncStoresTheSnapshot(t *testing.T) {
	e := newEnv(t)
	org := e.customerOrganization(`{"lastSyncedEventId":null}`)
	sub := subscription("sub_1", "active", 1739923200, subscriptionItem("si_1", price("price_pro_yearly", PlanPro, kindBase, IntervalYearly), 1771459200))
	sub["trial_end"] = 1740000000
	e.subscriptionsOn([]any{sub}, []string{"workspace-limit-5", "responses-included-2000", "workflow-runs-included-1000", "custom-links-in-surveys", "custom-links-in-surveys"},
		map[string]any{"id": "cus_123", "object": "customer", "invoice_settings": map[string]any{"default_payment_method": "pm_1"}})

	rec, err := e.engine.Sync(context.Background(), org, &eventRef{ID: "evt_new", Created: 1739923300})
	if err != nil {
		t.Fatal(err)
	}
	s := e.record(org)
	if !reflect.DeepEqual(rec, s) {
		t.Fatalf("returned %+v, stored %+v", rec, s)
	}
	snap := s.Stripe
	if snap.Plan != PlanPro || *snap.Interval != IntervalYearly || *snap.SubscriptionStatus != "active" || *snap.SubscriptionID != "sub_1" || !*snap.HasPaymentMethod {
		t.Errorf("snapshot %+v", snap)
	}
	if !reflect.DeepEqual(snap.Features, []string{"workspace-limit-5", "responses-included-2000", "workflow-runs-included-1000", "custom-links-in-surveys"}) {
		t.Errorf("features %v", snap.Features)
	}
	if *snap.LastSyncedEventID != "evt_new" || *snap.LastStripeEventCreatedAt != "2025-02-19T00:01:40.000Z" || *snap.LastSyncedAt != "2026-03-09T12:00:00.000Z" || *snap.TrialEnd != "2025-02-19T21:20:00.000Z" {
		t.Errorf("bookkeeping %+v", snap)
	}
	if snap.PendingChange != nil || snap.PaymentAttemptError != nil {
		t.Errorf("pending state %+v", snap)
	}
	if !s.UsageCycleAnchor.Equal(time.Unix(1739923200, 0)) {
		t.Errorf("anchor %v", s.UsageCycleAnchor)
	}
	if !reflect.DeepEqual(s.Limits, Limits{Workspaces: i64(5), Monthly: MonthlyLimits{Responses: i64(2000), WorkflowRuns: i64(1000)}}) {
		t.Errorf("limits %s", jsonString(t, s.Limits))
	}
	list := e.stripe.only(http.MethodGet, "/v1/subscriptions").Form
	if list.Get("customer") != "cus_123" || list.Get("status") != "all" || list.Get("limit") != "20" || list.Get("expand[0]") != "data.schedule" {
		t.Errorf("subscription list %v", list)
	}

	// A redelivered event, and an event older than the last one synced, change nothing and read nothing.
	calls := e.stripe.count()
	for _, ev := range []*eventRef{{ID: "evt_new", Created: 1739923300}, {ID: "evt_old", Created: 1739923299}} {
		if _, err := e.engine.Sync(context.Background(), org, ev); err != nil {
			t.Fatal(err)
		}
	}
	if e.stripe.count() != calls || !reflect.DeepEqual(e.record(org), s) {
		t.Fatal("a duplicate or older event was synced")
	}
}

// forma2's limit rules: a missing workspace or responses entitlement keeps the previous limit, a missing workflow-runs one clears it, unlimited wins over numbers, and several numbers resolve to the largest.
func TestSyncEntitlementLimits(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name         string
		previous     string
		entitlements []string
		want         string
	}{
		{"absent workflow runs clear the previous allowance", `{"workspaces":3,"monthly":{"responses":1500,"workflowRuns":1000}}`, []string{"workspace-limit-3", "responses-included-1500"}, `{"workspaces":3,"monthly":{"responses":1500,"workflowRuns":null}}`},
		{"unlimited responses", `{"workspaces":1,"monthly":{"responses":250}}`, []string{"workspace-limit-unlimited", "responses-included-unlimited"}, `{"workspaces":null,"monthly":{"responses":null,"workflowRuns":null}}`},
		{"unlimited wins over numbers, and the largest number over others", `{"workspaces":1,"monthly":{"responses":250}}`, []string{"responses-included-5000", "responses-included-unlimited", "workspace-limit-2", "workspace-limit-10", "workspace-limit-x"}, `{"workspaces":10,"monthly":{"responses":null,"workflowRuns":null}}`},
		{"missing workspace and responses entitlements keep the previous limits", `{"workspaces":4,"monthly":{"responses":777}}`, []string{"custom-links-in-surveys"}, `{"workspaces":4,"monthly":{"responses":777,"workflowRuns":null}}`},
	}
	var ents []string
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any { return list() })
	e.stripe.on("GET /v1/entitlements/active_entitlements", func(*http.Request) any {
		var data []any
		for _, k := range ents {
			data = append(data, map[string]any{"id": "ent_" + k, "lookup_key": k})
		}
		return list(data...)
	})
	for _, tc := range cases {
		org := e.customerOrganization(`{}`)
		e.exec(`UPDATE organization_billing SET limits = ?::jsonb WHERE organization_id = ?`, tc.previous, org)
		ents = tc.entitlements
		if _, err := e.engine.Sync(context.Background(), org, nil); err != nil {
			t.Fatal(err)
		}
		if got := jsonString(t, e.record(org).Limits); got != tc.want {
			t.Errorf("%s: limits %s, want %s", tc.name, got, tc.want)
		}
	}
}

// Without a live subscription the organization is on Hobby; with several, the highest plan wins, then the newest.
func TestSyncPlanResolution(t *testing.T) {
	e := newEnv(t)
	hobby := subscription("sub_hobby", "active", 300, subscriptionItem("si_h", price("price_hobby_monthly", PlanHobby, kindBase, IntervalMonthly), 0))
	scale := subscription("sub_scale", "trialing", 100, subscriptionItem("si_s", price("price_scale_monthly", PlanScale, kindBase, IntervalMonthly), 0))
	canceled := subscription("sub_custom", "canceled", 400, subscriptionItem("si_c", price("price_custom", PlanCustom, kindBase, IntervalMonthly), 0))
	subs := []any{}
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any { return list(subs...) })
	e.stripe.on("GET /v1/products/{id}", func(r *http.Request) any { return product(strings.TrimPrefix(r.PathValue("id"), "prod_")) })
	e.stripe.on("GET /v1/entitlements/active_entitlements", func(*http.Request) any { return list() })
	e.stripe.on("GET /v1/customers/{id}", func(*http.Request) any { return map[string]any{"id": "cus_123", "deleted": true} })

	org := e.customerOrganization(`{}`)
	if _, err := e.engine.Sync(context.Background(), org, nil); err != nil {
		t.Fatal(err)
	}
	if s := e.record(org).Stripe; s.Plan != PlanHobby || s.SubscriptionID != nil || s.SubscriptionStatus != nil || s.Interval != nil || *s.HasPaymentMethod {
		t.Fatalf("no subscription %+v", s)
	}
	subs = []any{hobby, scale, canceled}
	if _, err := e.engine.Sync(context.Background(), org, nil); err != nil {
		t.Fatal(err)
	}
	if s := e.record(org).Stripe; s.Plan != PlanScale || *s.SubscriptionID != "sub_scale" || *s.SubscriptionStatus != "trialing" || *s.HasPaymentMethod {
		t.Fatalf("highest plan %+v", s)
	}
	if n := len(e.stripe.requests(http.MethodGet, "/v1/products/prod_hobby")); n != 1 {
		t.Errorf("product read %d times in one sync", n)
	}
}

// forma2's payment-failure banner rules: an event-driven sync or an observed plan change clears it, a staleness refresh with no plan change keeps it.
func TestSyncPaymentAttemptError(t *testing.T) {
	e := newEnv(t)
	e.subscriptionsOn(nil, nil, nil)
	banner := `"paymentAttemptError":{"type":"requires_action","paymentIntentId":"pi_1","message":"m","createdAt":"2026-01-01T00:00:00.000Z"}`
	cases := []struct {
		name     string
		snapshot string
		event    *eventRef
		cleared  bool
	}{
		{"plan change", `{"plan":"pro",` + banner + `}`, nil, true},
		{"event-driven sync", `{"plan":"hobby",` + banner + `}`, &eventRef{ID: "evt_1", Created: 100}, true},
		{"read-through sync without a plan change", `{"plan":"hobby",` + banner + `}`, nil, false},
	}
	for _, tc := range cases {
		org := e.customerOrganization(tc.snapshot)
		if _, err := e.engine.Sync(context.Background(), org, tc.event); err != nil {
			t.Fatal(err)
		}
		if got := e.record(org).Stripe.PaymentAttemptError == nil; got != tc.cleared {
			t.Errorf("%s: cleared = %v", tc.name, got)
		}
	}

	// setOrganizationPaymentAttemptError: set and cleared, and an event older than the last synced one ignored.
	org := e.customerOrganization(`{"plan":"pro","lastStripeEventCreatedAt":"2026-01-01T00:00:10.000Z"}`)
	pe := &PaymentAttemptError{Type: PaymentErrorFailedInvoice, PaymentIntentID: "pi_2", Message: "x", CreatedAt: "2026-01-01T00:00:00.000Z"}
	if err := e.engine.SetPaymentAttemptError(context.Background(), org, pe, &eventRef{ID: "evt_old", Created: time.Date(2026, 1, 1, 0, 0, 9, 0, time.UTC).Unix()}); err != nil {
		t.Fatal(err)
	}
	if e.record(org).Stripe.PaymentAttemptError != nil {
		t.Fatal("an older event set the banner")
	}
	if err := e.engine.SetPaymentAttemptError(context.Background(), org, pe, nil); err != nil {
		t.Fatal(err)
	}
	if s := e.record(org).Stripe; !reflect.DeepEqual(s.PaymentAttemptError, pe) || s.Plan != PlanPro || *s.LastSyncedAt != "2026-03-09T12:00:00.000Z" {
		t.Fatalf("banner %+v", s)
	}
	if err := e.engine.SetPaymentAttemptError(context.Background(), org, nil, nil); err != nil {
		t.Fatal(err)
	}
	if e.record(org).Stripe.PaymentAttemptError != nil {
		t.Fatal("the banner was not cleared")
	}
}

// forma2's "mirrors a pending downgrade from subscription schedule", and a cancellation at period end read as a move to Hobby.
func TestSyncPendingChange(t *testing.T) {
	e := newEnv(t)
	scaleSub := subscription("sub_1", "active", 100, subscriptionItem("si_1", price("price_scale_monthly", PlanScale, kindBase, IntervalMonthly), 2000), subscriptionItem("si_2", price("price_scale_responses", PlanScale, kindResponses, IntervalMonthly), 2500))
	scaleSub["schedule"] = map[string]any{"id": "sub_sched_1", "object": "subscription_schedule"}
	cancelling := subscription("sub_2", "active", 100, subscriptionItem("si_3", price("price_pro_monthly", PlanPro, kindBase, IntervalMonthly), 2000), subscriptionItem("si_4", price("price_pro_responses", PlanPro, kindResponses, IntervalMonthly), 2600))
	cancelling["cancel_at_period_end"] = true
	current := scaleSub
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any { return list(current) })
	e.stripe.on("GET /v1/products/{id}", func(r *http.Request) any { return product(strings.TrimPrefix(r.PathValue("id"), "prod_")) })
	e.stripe.on("GET /v1/entitlements/active_entitlements", func(*http.Request) any { return list() })
	e.stripe.on("GET /v1/customers/{id}", func(*http.Request) any { return map[string]any{"id": "cus_123"} })
	e.stripe.on("GET /v1/subscription_schedules/sub_sched_1", func(*http.Request) any {
		return map[string]any{"id": "sub_sched_1", "object": "subscription_schedule", "current_phase": map[string]any{"start_date": 100, "end_date": 2000}, "phases": []any{
			map[string]any{"start_date": 100, "end_date": 2000, "items": []any{map[string]any{"price": "price_scale_monthly", "quantity": 1}}},
			map[string]any{"start_date": 2000, "end_date": 4000, "items": []any{map[string]any{"price": "price_pro_yearly", "quantity": 1}, map[string]any{"price": "price_pro_responses"}}},
		}}
	})
	e.stripe.on("GET /v1/prices/{id}", func(r *http.Request) any {
		for _, p := range standardPrices() {
			if p.(map[string]any)["id"] == r.PathValue("id") {
				return p
			}
		}
		return nil
	})
	org := e.customerOrganization(`{}`)
	if _, err := e.engine.Sync(context.Background(), org, nil); err != nil {
		t.Fatal(err)
	}
	if got := jsonString(t, e.record(org).Stripe.PendingChange); got != `{"type":"plan_change","targetPlan":"pro","targetInterval":"yearly","effectiveAt":"1970-01-01T00:33:20.000Z"}` {
		t.Fatalf("scheduled change %s", got)
	}
	if call := e.stripe.only(http.MethodGet, "/v1/prices/price_pro_yearly"); call.Form.Get("expand[0]") != "product" {
		t.Errorf("phase price read without its product: %v", call.Form)
	}

	current = cancelling
	if _, err := e.engine.Sync(context.Background(), org, nil); err != nil {
		t.Fatal(err)
	}
	if got := jsonString(t, e.record(org).Stripe.PendingChange); got != `{"type":"plan_change","targetPlan":"hobby","targetInterval":"monthly","effectiveAt":"1970-01-01T00:43:20.000Z"}` {
		t.Fatalf("cancellation at period end %s", got)
	}
}

// Billing off (self-hosted, or no Stripe key) syncs, reconciles and sets up nothing.
func TestDisabledBillingDoesNothing(t *testing.T) {
	for name, configure := range map[string]func(*config.Config){
		"self-hosted":   func(c *config.Config) { c.IsFormaCloud = false },
		"no Stripe key": func(c *config.Config) { c.Stripe.SecretKey = "" },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, configure)
			org := e.customerOrganization(`{"plan":"pro"}`)
			ctx := context.Background()
			if rec, err := e.engine.Sync(ctx, org, nil); err != nil || rec != nil {
				t.Fatalf("sync %+v %v", rec, err)
			}
			if err := e.engine.EnsureSetup(ctx, org); err != nil {
				t.Fatal(err)
			}
			if err := e.engine.Reconcile(ctx, org); err != nil {
				t.Fatal(err)
			}
			if id, err := e.engine.EnsureCustomer(ctx, org); err != nil || id != "" {
				t.Fatalf("customer %q %v", id, err)
			}
			rec, err := e.engine.ReadThrough(ctx, org)
			if err != nil || rec.Stripe.Plan != PlanPro {
				t.Fatalf("read %+v %v", rec, err)
			}
			if e.stripe.count() != 0 {
				t.Fatalf("%d Stripe calls", e.stripe.count())
			}
		})
	}
}

// forma2's getOrganizationBillingWithReadThroughSync tests: the stored row when there is no customer or the snapshot is fresh; a stale one synced by the lock holder only; the stored row when Stripe fails; self-hosted reads the database.
func TestReadThrough(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	noCustomer, _ := e.organization()
	if rec, err := e.engine.ReadThrough(ctx, noCustomer); err != nil || rec.StripeCustomerID != nil || !reflect.DeepEqual(rec.Limits, DefaultLimits(true)) {
		t.Fatalf("no customer %+v %v", rec, err)
	}
	fresh := e.customerOrganization(`{"plan":"pro","lastSyncedAt":"2026-03-09T11:56:00.000Z"}`)
	if rec, err := e.engine.ReadThrough(ctx, fresh); err != nil || rec.Stripe.Plan != PlanPro || e.stripe.count() != 0 {
		t.Fatalf("fresh snapshot %+v %v, %d Stripe calls", rec, err, e.stripe.count())
	}

	fail := true
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any {
		if fail {
			return map[string]any{"object": "list", "data": "not a list"}
		}
		return list()
	})
	e.stripe.on("GET /v1/entitlements/active_entitlements", func(*http.Request) any { return list() })
	stale := e.customerOrganization(`{"plan":"pro","lastSyncedAt":"2026-03-09T11:54:00.000Z"}`)
	if rec, err := e.engine.ReadThrough(ctx, stale); err != nil || rec.Stripe.Plan != PlanPro {
		t.Fatalf("failed sync %+v %v", rec, err)
	}
	if !strings.Contains(e.logs.String(), "Failed to refresh billing snapshot from Stripe") {
		t.Error("the failed refresh was not logged")
	}
	// The failed attempt holds the lock until it expires, so the next caller serves the stored row without asking Stripe.
	calls := e.stripe.count()
	fail = false
	if rec, err := e.engine.ReadThrough(ctx, stale); err != nil || rec.Stripe.Plan != PlanPro || e.stripe.count() != calls {
		t.Fatalf("locked refresh %+v %v", rec, err)
	}
	if err := e.rdb.Del(ctx, syncLockKey(stale)).Err(); err != nil {
		t.Fatal(err)
	}
	if rec, err := e.engine.ReadThrough(ctx, stale); err != nil || rec.Stripe.Plan != PlanHobby || *rec.Stripe.LastSyncedAt != "2026-03-09T12:00:00.000Z" {
		t.Fatalf("refreshed %+v %v", rec, err)
	}
	if ttl := e.rdb.PTTL(ctx, syncLockKey(stale)).Val(); ttl <= 0 || ttl > syncLockTTL {
		t.Fatalf("lock ttl %v", ttl)
	}
}

// forma2's reconcile tests: a paid or trial subscription cancels the Hobby ones without proration (one already gone is fine); no live subscription creates Hobby, keyed so concurrent calls make one.
func TestReconcile(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	org := e.customerOrganization(`{}`)
	subs := []any{
		subscription("sub_hobby", "active", 1, subscriptionItem("si_1", price("price_hobby_monthly", PlanHobby, kindBase, IntervalMonthly), 0)),
		subscription("sub_hobby_gone", "active", 2, subscriptionItem("si_2", price("price_hobby_monthly", PlanHobby, kindBase, IntervalMonthly), 0)),
		subscription("sub_pro", "trialing", 3, subscriptionItem("si_3", price("price_pro_monthly", PlanPro, kindBase, IntervalMonthly), 0)),
	}
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any { return list(subs...) })
	e.stripe.catalogOn()
	e.stripe.on("DELETE /v1/subscriptions/sub_hobby", func(*http.Request) any { return map[string]any{"id": "sub_hobby", "status": "canceled"} })
	e.stripe.fail("DELETE /v1/subscriptions/sub_hobby_gone", http.StatusNotFound, "resource_missing", "")
	if err := e.engine.Reconcile(ctx, org); err != nil {
		t.Fatal(err)
	}
	if call := e.stripe.only(http.MethodDelete, "/v1/subscriptions/sub_hobby"); call.Form.Get("prorate") != "false" {
		t.Errorf("cancel params %v", call.Form)
	}
	if len(e.stripe.requests(http.MethodDelete, "/v1/subscriptions/sub_pro")) != 0 {
		t.Error("the trial was cancelled")
	}

	subs = []any{subscription("sub_old", "canceled", 1)}
	e.stripe.on("POST /v1/subscriptions", func(*http.Request) any { return map[string]any{"id": "sub_new"} })
	if err := e.engine.Reconcile(ctx, org); err != nil {
		t.Fatal(err)
	}
	call := e.stripe.only(http.MethodPost, "/v1/subscriptions")
	if call.Form.Get("customer") != "cus_123" || call.Form.Get("items[0][price]") != "price_hobby_monthly" || call.Form.Get("items[0][quantity]") != "1" || call.Form.Get("metadata[organizationId]") != org.String() || call.IdempotencyKey != "ensure-hobby-subscription-"+org.String()+"-1" {
		t.Fatalf("hobby params %v %s", call.Form, call.IdempotencyKey)
	}
}

// forma2's "creates customer, provisions hobby subscription, and syncs billing".
func TestEnsureSetup(t *testing.T) {
	e := newEnv(t)
	org, _ := e.organization()
	created := false
	e.stripe.on("POST /v1/customers", func(*http.Request) any { return map[string]any{"id": "cus_123"} })
	e.stripe.catalogOn()
	hobby := subscription("sub_hobby", "active", 5, subscriptionItem("si_1", price("price_hobby_monthly", PlanHobby, kindBase, IntervalMonthly), 0))
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any {
		if created {
			return list(hobby)
		}
		return list()
	})
	e.stripe.on("POST /v1/subscriptions", func(*http.Request) any { created = true; return hobby })
	e.stripe.on("GET /v1/entitlements/active_entitlements", func(*http.Request) any {
		return list(map[string]any{"id": "ent_1", "lookup_key": "workspace-limit-1"}, map[string]any{"id": "ent_2", "lookup_key": "responses-included-250"})
	})
	e.stripe.on("GET /v1/customers/{id}", func(*http.Request) any { return map[string]any{"id": "cus_123"} })
	if err := e.engine.EnsureSetup(context.Background(), org); err != nil {
		t.Fatal(err)
	}
	rec := e.record(org)
	if *rec.StripeCustomerID != "cus_123" || rec.Stripe.Plan != PlanHobby || *rec.Stripe.SubscriptionID != "sub_hobby" || *rec.Stripe.Interval != IntervalMonthly {
		t.Fatalf("after setup %+v %+v", rec, rec.Stripe)
	}
}

// forma2's cleanupStripeCustomer: the live subscriptions are cancelled without proration, the customer stays.
func TestCleanupCustomer(t *testing.T) {
	e := newEnv(t)
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any {
		return list(subscription("sub_live", "past_due", 1), subscription("sub_done", "canceled", 2))
	})
	e.stripe.on("DELETE /v1/subscriptions/sub_live", func(*http.Request) any { return map[string]any{"id": "sub_live"} })
	if err := e.engine.CleanupCustomer(context.Background(), "cus_123"); err != nil {
		t.Fatal(err)
	}
	if e.stripe.only(http.MethodGet, "/v1/subscriptions").Form.Get("limit") != "100" || e.stripe.only(http.MethodDelete, "/v1/subscriptions/sub_live").Form.Get("prorate") != "false" {
		t.Fatal("cleanup params")
	}
}

// forma2's addOptimisticBillingFeature: added once, every other field kept, nothing without a snapshot.
func TestAddOptimisticFeature(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	org := e.customerOrganization(`{"plan":"pro","subscriptionId":"sub_1","features":["rbac"]}`)
	for range 2 {
		if err := e.engine.addOptimisticFeature(ctx, org, FeatureAISmartTools); err != nil {
			t.Fatal(err)
		}
	}
	if s := e.record(org).Stripe; !reflect.DeepEqual(s.Features, []string{"rbac", FeatureAISmartTools}) || s.Plan != PlanPro || *s.SubscriptionID != "sub_1" {
		t.Fatalf("snapshot %+v", s)
	}
	bare, _ := e.organization()
	e.exec(`INSERT INTO organization_billing (organization_id, limits) VALUES (?, '{"workspaces":1,"monthly":{"responses":250}}')`, bare)
	if err := e.engine.addOptimisticFeature(ctx, bare, FeatureAISmartTools); err != nil {
		t.Fatal(err)
	}
	if e.record(bare).Stripe != nil {
		t.Fatal("a snapshot was created")
	}
}

// The snapshot round-trips forma2's JSON: the fields it wrote, nulls as nulls.
func TestSnapshotJSON(t *testing.T) {
	in := `{"plan":"pro","interval":null,"subscriptionStatus":"active","subscriptionId":"sub_1","hasPaymentMethod":true,"features":["a"],"lastStripeEventCreatedAt":null,"lastSyncedAt":"2026-01-01T00:00:00.000Z","lastSyncedEventId":"evt_1","trialEnd":null,"pendingChange":{"type":"plan_change","targetPlan":"hobby","targetInterval":"monthly","effectiveAt":"2026-02-01T00:00:00.000Z"},"paymentAttemptError":null}`
	var s Snapshot
	if err := json.Unmarshal([]byte(in), &s); err != nil {
		t.Fatal(err)
	}
	if got := jsonString(t, s); got != in {
		t.Fatalf("round trip\n got %s\nwant %s", got, in)
	}
}
