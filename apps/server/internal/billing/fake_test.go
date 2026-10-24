package billing

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stripe/stripe-go/v84"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// stripeCall is one request the client sent to the fake Stripe API, with its decoded form.
type stripeCall struct {
	Method         string
	Path           string
	Form           url.Values
	IdempotencyKey string
}

// fakeStripe is an httptest server answering the part of Stripe's REST API billing calls, the way stripe-mock does: each test registers the answers of the endpoints it expects, as Stripe documents their objects, and every request is recorded. A request no test expected fails the test.
type fakeStripe struct {
	t     *testing.T
	mux   *http.ServeMux
	url   string
	mu    sync.Mutex
	calls []stripeCall
}

func newFakeStripe(t *testing.T) *fakeStripe {
	f := &fakeStripe{t: t, mux: http.NewServeMux()}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

func (f *fakeStripe) serve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("parse Stripe request form: %v", err)
	}
	f.mu.Lock()
	f.calls = append(f.calls, stripeCall{Method: r.Method, Path: r.URL.Path, Form: r.Form, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	f.mu.Unlock()
	if _, pattern := f.mux.Handler(r); pattern == "" {
		f.t.Errorf("unexpected Stripe call %s %s %v", r.Method, r.URL.Path, r.Form)
		writeJSON(w, http.StatusNotFound, stripeErrorBody("invalid_request_error", "resource_missing", "", "Unrecognized request URL"))
		return
	}
	// ServeHTTP, unlike the handler Handler returns, fills in the pattern's path values.
	f.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func stripeErrorBody(kind, code, declineCode, message string) map[string]any {
	e := map[string]any{"type": kind, "message": message}
	if code != "" {
		e["code"] = code
	}
	if declineCode != "" {
		e["decline_code"] = declineCode
	}
	return map[string]any{"error": e}
}

// on answers pattern (a ServeMux pattern such as "GET /v1/products/{id}") with fn's object.
func (f *fakeStripe) on(pattern string, fn func(r *http.Request) any) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, fn(r))
	})
}

// fail answers pattern with a Stripe error.
func (f *fakeStripe) fail(pattern string, status int, code, declineCode string) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
		kind := "invalid_request_error"
		if declineCode != "" || status == http.StatusPaymentRequired {
			kind = "card_error"
		}
		writeJSON(w, status, stripeErrorBody(kind, code, declineCode, "Stripe refused the request"))
	})
}

// requests returns the recorded calls of one method and path.
func (f *fakeStripe) requests(method, path string) []stripeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []stripeCall
	for _, c := range f.calls {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

// only is the single call of one method and path; anything else fails the test.
func (f *fakeStripe) only(method, path string) stripeCall {
	f.t.Helper()
	calls := f.requests(method, path)
	if len(calls) != 1 {
		f.t.Fatalf("%d calls of %s %s, want 1", len(calls), method, path)
	}
	return calls[0]
}

func (f *fakeStripe) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// stripeFor is a Stripe account whose client talks to the fake.
func stripeFor(t *testing.T, cfg config.Config, f *fakeStripe, log *slog.Logger) *Stripe {
	t.Helper()
	if cfg.Stripe.SecretKey == "" {
		cfg.Stripe.SecretKey = "sk_test_123"
	}
	backends := stripe.NewBackendsWithConfig(&stripe.BackendConfig{URL: stripe.String(f.url), MaxNetworkRetries: stripe.Int64(0), LeveledLogger: &stripe.LeveledLogger{Level: stripe.LevelNull}})
	return newStripe(cfg, backends, log)
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// list is a Stripe list object.
func list(data ...any) map[string]any {
	if data == nil {
		data = []any{}
	}
	return map[string]any{"object": "list", "data": data, "has_more": false, "url": "/v1/list"}
}

// responseTiers are forma2's catalog test tiers: 2000 free, then 8 and 2 cents.
func responseTiers() []any {
	return []any{
		map[string]any{"flat_amount": nil, "flat_amount_decimal": nil, "unit_amount": 0, "unit_amount_decimal": "0", "up_to": 2000},
		map[string]any{"flat_amount": nil, "flat_amount_decimal": nil, "unit_amount": 8, "unit_amount_decimal": "8", "up_to": 5000},
		map[string]any{"flat_amount": nil, "flat_amount_decimal": nil, "unit_amount": 2, "unit_amount_decimal": "2", "up_to": nil},
	}
}

func product(plan string) map[string]any {
	return map[string]any{"id": "prod_" + plan, "object": "product", "active": true, "metadata": map[string]any{"forma_plan": plan}}
}

// price is forma2's createPrice: a catalog price tagged with its plan, kind and interval, with its product expanded.
func price(id, plan, kind, interval string) map[string]any {
	recurring := map[string]any{"usage_type": "licensed", "interval": "month"}
	if interval == IntervalYearly {
		recurring["interval"] = "year"
	}
	unit := 1000
	if interval == IntervalYearly {
		unit = 10000
	}
	p := map[string]any{
		"id": id, "object": "price", "active": true, "currency": "usd", "billing_scheme": "per_unit", "unit_amount": unit,
		"metadata":  map[string]any{"forma_plan": plan, "forma_price_kind": kind, "forma_interval": interval},
		"recurring": recurring, "product": product(plan),
	}
	if kind != kindBase {
		recurring["usage_type"] = "metered"
		p["unit_amount"], p["billing_scheme"], p["tiers"], p["tiers_mode"] = nil, "tiered", responseTiers(), "graduated"
	}
	return p
}

func standardPrices() []any {
	return []any{
		price("price_hobby_monthly", PlanHobby, kindBase, IntervalMonthly),
		price("price_pro_monthly", PlanPro, kindBase, IntervalMonthly),
		price("price_pro_yearly", PlanPro, kindBase, IntervalYearly),
		price("price_pro_responses", PlanPro, kindResponses, IntervalMonthly),
		price("price_scale_monthly", PlanScale, kindBase, IntervalMonthly),
		price("price_scale_yearly", PlanScale, kindBase, IntervalYearly),
		price("price_scale_responses", PlanScale, kindResponses, IntervalMonthly),
	}
}

// subscriptionItem is a subscription item whose price names its product by id, as a list returns it.
func subscriptionItem(id string, p map[string]any, periodEnd int64) map[string]any {
	ref := map[string]any{}
	for k, v := range p {
		ref[k] = v
	}
	ref["product"] = p["product"].(map[string]any)["id"]
	item := map[string]any{"id": id, "object": "subscription_item", "price": ref, "current_period_end": periodEnd}
	if p["recurring"].(map[string]any)["usage_type"] == "licensed" {
		item["quantity"] = 1
	}
	return item
}

// subscription is a subscription with the given status and items, created at created.
func subscription(id, status string, created int64, items ...any) map[string]any {
	return map[string]any{
		"id": id, "object": "subscription", "status": status, "created": created, "currency": "usd", "customer": "cus_123",
		"billing_cycle_anchor": created, "items": list(items...), "metadata": map[string]any{},
	}
}

// catalogOn answers the price list with prices, and every product by id.
func (f *fakeStripe) catalogOn(prices ...any) {
	if prices == nil {
		prices = standardPrices()
	}
	f.on("GET /v1/prices", func(*http.Request) any { return list(prices...) })
	f.on("GET /v1/products/{id}", func(r *http.Request) any {
		return product(strings.TrimPrefix(r.PathValue("id"), "prod_"))
	})
}

// eventJSON is a Stripe event of the type for object, created at created.
func eventJSON(t *testing.T, id, kind string, created int64, object map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"id": id, "object": "event", "type": kind, "created": created, "api_version": stripe.APIVersion, "data": map[string]any{"object": object}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
