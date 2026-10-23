package billing

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v84/webhook"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// post sends a webhook delivery: the payload signed with secret now, or with the given header when it is set.
func (e *env) post(payload []byte, secret, header string) *httptest.ResponseRecorder {
	e.t.Helper()
	r := gin.New()
	NewWebhookHandler(e.engine, e.log).Register(r)
	req := httptest.NewRequest(http.MethodPost, WebhookPath, bytes.NewReader(payload))
	if header == "" && secret != "" {
		header = webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: payload, Secret: secret, Timestamp: time.Now()}).Header
	}
	if header != "" {
		req.Header.Set("Stripe-Signature", header)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func answers(t *testing.T, rec *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if rec.Code != status || strings.TrimSpace(rec.Body.String()) != body {
		t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body.String(), status, body)
	}
}

const received200 = `{"received":true}`

// forma2's route answers before any event is processed: 413 over 2 MiB, 400 without a signature (before the configuration check), 503 unconfigured, 400 with "Webhook Error: ..." for a bad or stale signature, and 200 for an event type it does not handle.
func TestWebhookRequestHandling(t *testing.T) {
	e := newEnv(t)
	event := eventJSON(t, "evt_1", "invoice.paid", 1739923200, map[string]any{"id": "in_1", "customer": "cus_123"})

	answers(t, e.post(bytes.Repeat([]byte("x"), webhookBodyLimit+1), "whsec_test", ""), http.StatusRequestEntityTooLarge, `{"message":"Payload Too Large"}`)
	answers(t, e.post(event, "", ""), http.StatusBadRequest, `{"message":"Stripe signature missing"}`)

	rec := e.post(event, "whsec_other", "")
	if rec.Code != http.StatusBadRequest || !strings.HasPrefix(rec.Body.String(), `{"message":"Webhook Error: `) {
		t.Fatalf("bad signature: %d %s", rec.Code, rec.Body.String())
	}
	stale := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: event, Secret: "whsec_test", Timestamp: time.Now().Add(-6 * time.Minute)}).Header
	if rec := e.post(event, "", stale); rec.Code != http.StatusBadRequest {
		t.Fatalf("a signature older than five minutes: %d %s", rec.Code, rec.Body.String())
	}
	answers(t, e.post(event, "whsec_test", ""), http.StatusOK, received200)
	// An event pinned to another API version than the client's is still accepted, as forma2's SDK did.
	other := []byte(strings.Replace(string(event), `"api_version":"`, `"api_version":"2024-06-20&`, 1))
	answers(t, e.post(other, "whsec_test", ""), http.StatusOK, received200)
	if e.stripe.count() != 0 {
		t.Fatal("an unhandled event reached Stripe")
	}
}

func TestWebhookUnconfigured(t *testing.T) {
	for name, configure := range map[string]func(*config.Config){
		"no webhook secret": func(c *config.Config) { c.Stripe.WebhookSecret = "" },
		"no secret key":     func(c *config.Config) { c.Stripe.SecretKey = "" },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, configure)
			event := eventJSON(t, "evt_1", "customer.subscription.updated", 1, map[string]any{"id": "sub_1"})
			answers(t, e.post(event, "", ""), http.StatusBadRequest, `{"message":"Stripe signature missing"}`)
			answers(t, e.post(event, "whsec_test", ""), http.StatusServiceUnavailable, `{"message":"Stripe webhook is not configured"}`)
		})
	}
}

// A subscription event resolves its organization (metadata, then client reference, then customer), reconciles and syncs it; a redelivery changes nothing.
func TestWebhookSyncsTheOrganization(t *testing.T) {
	e := newEnv(t)
	org := e.customerOrganization(`{"plan":"hobby"}`)
	pro := subscription("sub_1", "active", 1739923200, subscriptionItem("si_1", price("price_pro_monthly", PlanPro, kindBase, IntervalMonthly), 0))
	e.subscriptionsOn([]any{pro}, []string{"workspace-limit-3", "responses-included-2000"}, nil)

	event := eventJSON(t, "evt_1", "customer.subscription.updated", 1739923300, map[string]any{"id": "sub_1", "object": "subscription", "customer": "cus_123", "metadata": map[string]any{}})
	answers(t, e.post(event, "whsec_test", ""), http.StatusOK, received200)
	rec := e.record(org)
	if rec.Stripe.Plan != PlanPro || *rec.Stripe.LastSyncedEventID != "evt_1" || *rec.Limits.Monthly.Responses != 2000 {
		t.Fatalf("after the event %+v %s", rec.Stripe, jsonString(t, rec.Limits))
	}
	lists := len(e.stripe.requests(http.MethodGet, "/v1/subscriptions"))
	answers(t, e.post(event, "whsec_test", ""), http.StatusOK, received200)
	if got := len(e.stripe.requests(http.MethodGet, "/v1/subscriptions")); got != lists+1 {
		t.Fatalf("a redelivery read the subscriptions %d times; only the reconcile reads them", got-lists)
	}

	// Ids forma2 wrote into Stripe's metadata are its own; they resolve to the migrated organization.
	legacy := legacyid.ID("clxlegacyorganization0000")
	e.exec(`INSERT INTO organizations (id, name) VALUES (?, 'Legacy')`, legacy)
	e.exec(`INSERT INTO organization_billing (organization_id, stripe_customer_id, limits) VALUES (?, 'cus_legacy', '{"workspaces":1,"monthly":{"responses":250}}')`, legacy)
	event = eventJSON(t, "evt_2", "subscription_schedule.updated", 1739923400, map[string]any{"id": "sub_sched_1", "customer": "cus_other", "metadata": map[string]any{"organizationId": "clxlegacyorganization0000"}})
	answers(t, e.post(event, "whsec_test", ""), http.StatusOK, received200)
	if rec := e.record(legacy); rec.Stripe == nil || *rec.Stripe.LastSyncedEventID != "evt_2" {
		t.Fatalf("the legacy organization was not synced: %+v", rec.Stripe)
	}
}

// forma2's unresolved organization answers: a completed checkout without one is an error Stripe retries, any other event is acknowledged.
func TestWebhookUnresolvedOrganization(t *testing.T) {
	e := newEnv(t)
	other := eventJSON(t, "evt_1", "entitlements.active_entitlement_summary.updated", 1, map[string]any{"object": "entitlements.active_entitlement_summary", "customer": "cus_unknown"})
	answers(t, e.post(other, "whsec_test", ""), http.StatusOK, received200)
	checkout := eventJSON(t, "evt_2", "checkout.session.completed", 1, map[string]any{"id": "cs_1", "object": "checkout.session", "mode": "subscription", "customer": "cus_unknown", "client_reference_id": nil, "metadata": map[string]any{}})
	answers(t, e.post(checkout, "whsec_test", ""), http.StatusInternalServerError, `{"message":"Checkout completed but organization could not be resolved."}`)
	if e.stripe.count() != 0 {
		t.Fatal("an unresolved event reached Stripe")
	}
}

// forma2's setup checkout tests: the saved card becomes the customer's and the subscription's default, the upgrade is not applied, and a failing sync answers 500 so Stripe retries.
func TestWebhookSetupCheckoutCompleted(t *testing.T) {
	e := newEnv(t)
	org := e.customerOrganization(`{"plan":"hobby"}`)
	e.stripe.on("GET /v1/setup_intents/seti_1", func(*http.Request) any { return map[string]any{"id": "seti_1", "payment_method": "pm_1"} })
	e.stripe.on("POST /v1/customers/cus_123", func(*http.Request) any { return map[string]any{"id": "cus_123"} })
	e.stripe.on("POST /v1/subscriptions/sub_1", func(*http.Request) any { return map[string]any{"id": "sub_1"} })
	failSync := false
	hobby := subscription("sub_1", "active", 1, subscriptionItem("si_1", price("price_hobby_monthly", PlanHobby, kindBase, IntervalMonthly), 0))
	e.stripe.on("GET /v1/subscriptions", func(r *http.Request) any {
		if failSync && r.Form.Get("expand[0]") == "data.schedule" {
			return map[string]any{"object": "list", "data": "unavailable"}
		}
		return list(hobby)
	})
	e.stripe.on("GET /v1/products/{id}", func(r *http.Request) any { return product(strings.TrimPrefix(r.PathValue("id"), "prod_")) })
	e.stripe.on("GET /v1/entitlements/active_entitlements", func(*http.Request) any { return list() })
	e.stripe.on("GET /v1/customers/{id}", func(*http.Request) any { return map[string]any{"id": "cus_123"} })
	session := map[string]any{
		"id": "cs_1", "object": "checkout.session", "mode": "setup", "status": "complete", "customer": "cus_123", "setup_intent": "seti_1",
		"metadata": map[string]any{"organizationId": org.String(), "subscriptionId": "sub_1", "targetPlan": "pro", "targetInterval": "monthly"},
	}
	answers(t, e.post(eventJSON(t, "evt_1", "checkout.session.completed", 1739923200, session), "whsec_test", ""), http.StatusOK, received200)
	if e.stripe.only(http.MethodPost, "/v1/customers/cus_123").Form.Get("invoice_settings[default_payment_method]") != "pm_1" {
		t.Error("the card did not become the customer's default")
	}
	updates := e.stripe.requests(http.MethodPost, "/v1/subscriptions/sub_1")
	if len(updates) != 1 || updates[0].Form.Get("default_payment_method") != "pm_1" || updates[0].Form.Has("items[0][price]") {
		t.Fatalf("subscription updates %v", updates)
	}
	if rec := e.record(org); *rec.Stripe.LastSyncedEventID != "evt_1" {
		t.Fatalf("not synced: %+v", rec.Stripe)
	}

	failSync = true
	answers(t, e.post(eventJSON(t, "evt_2", "checkout.session.completed", 1739923300, session), "whsec_test", ""), http.StatusInternalServerError, `{"message":"Stripe webhook processing failed; please retry."}`)
}

// forma2's payment intent tests: requires_action and a failed-invoice cancellation set the banner without a sync; a benign cancellation, a customer that is not an id, and a customer of no organization are ignored.
func TestWebhookPaymentIntents(t *testing.T) {
	e := newEnv(t)
	org := e.customerOrganization(`{"plan":"pro"}`)
	pi := func(id string, fields map[string]any) map[string]any {
		o := map[string]any{"id": id, "object": "payment_intent", "customer": "cus_123"}
		for k, v := range fields {
			o[k] = v
		}
		return o
	}
	answers(t, e.post(eventJSON(t, "evt_1", "payment_intent.requires_action", 1739923200, pi("pi_1", map[string]any{"status": "requires_action"})), "whsec_test", ""), http.StatusOK, received200)
	s := e.record(org).Stripe
	if s.PaymentAttemptError == nil || s.PaymentAttemptError.Type != PaymentErrorRequiresAction || s.PaymentAttemptError.PaymentIntentID != "pi_1" ||
		s.PaymentAttemptError.Message != "Payment requires additional authentication. Please complete verification or contact support." || s.PaymentAttemptError.CreatedAt != "2026-03-09T12:00:00.000Z" {
		t.Fatalf("banner %+v", s.PaymentAttemptError)
	}

	for _, ignored := range []map[string]any{
		pi("pi_2", map[string]any{"status": "canceled", "cancellation_reason": "abandoned"}),
		pi("pi_3", map[string]any{"cancellation_reason": "failed_invoice", "customer": nil}),
		pi("pi_4", map[string]any{"cancellation_reason": "failed_invoice", "customer": "cus_nobody"}),
	} {
		answers(t, e.post(eventJSON(t, "evt_"+uuid.NewString(), "payment_intent.canceled", 1739923300, ignored), "whsec_test", ""), http.StatusOK, received200)
		if e.record(org).Stripe.PaymentAttemptError.PaymentIntentID != "pi_1" {
			t.Fatalf("%v changed the banner", ignored)
		}
	}

	answers(t, e.post(eventJSON(t, "evt_5", "payment_intent.canceled", 1739923400, pi("pi_5", map[string]any{"status": "canceled", "cancellation_reason": "failed_invoice"})), "whsec_test", ""), http.StatusOK, received200)
	if pe := e.record(org).Stripe.PaymentAttemptError; pe.Type != PaymentErrorFailedInvoice || pe.PaymentIntentID != "pi_5" || pe.Message != "Payment was canceled. Please contact support to complete your upgrade." {
		t.Fatalf("failed invoice banner %+v", pe)
	}
	if e.stripe.count() != 0 {
		t.Fatal("a payment intent event synced the subscriptions")
	}
}
