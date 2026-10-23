package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v84"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// decodePrices reads catalog fixtures the way the client decodes Stripe's JSON.
func decodePrices(t *testing.T, prices ...any) []*stripe.Price {
	t.Helper()
	b, err := json.Marshal(prices)
	if err != nil {
		t.Fatal(err)
	}
	var out []*stripe.Price
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func workflowRunsPrice(id string) map[string]any {
	p := price(id, PlanScale, kindWorkflowRuns, IntervalMonthly)
	return p
}

func mustCatalog(t *testing.T, prices ...any) *Catalog {
	t.Helper()
	c, err := buildCatalog(decodePrices(t, prices...), discardLog())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func itemsOf(t *testing.T, c *Catalog, plan, interval string) []string {
	t.Helper()
	item, err := c.Item(plan, interval)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range item.lineItems() {
		if it.Quantity != nil {
			out = append(out, it.Price+"x"+jsonString(t, *it.Quantity))
		} else {
			out = append(out, it.Price)
		}
	}
	return out
}

func jsonString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// expectedOverage is forma2's EXPECTED_RESPONSE_OVERAGE.
const expectedOverage = `{"currency":"usd","tiers":[{"firstUnit":0,"lastUnit":2000,"perUnitCents":0},{"firstUnit":2001,"lastUnit":5000,"perUnitCents":8},{"firstUnit":5001,"lastUnit":null,"perUnitCents":2}]}`

// forma2's "resolves the metadata-backed billing catalog".
func TestCatalogFromMetadata(t *testing.T) {
	c := mustCatalog(t, standardPrices()...)
	if got := itemsOf(t, c, PlanHobby, IntervalYearly); !reflect.DeepEqual(got, []string{"price_hobby_monthlyx1"}) {
		t.Errorf("hobby items %v", got)
	}
	if got := itemsOf(t, c, PlanPro, IntervalYearly); !reflect.DeepEqual(got, []string{"price_pro_yearlyx1", "price_pro_responses"}) {
		t.Errorf("pro yearly items %v", got)
	}
	item := func(plan, interval string, unit int, overage string) string {
		return `{"plan":"` + plan + `","interval":"` + interval + `","currency":"usd","unitAmount":` + jsonString(t, unit) + `,"responseOverage":` + overage + `,"workflowRunsIncluded":null,"workflowRunsOverage":null}`
	}
	want := `{"hobby":{"monthly":` + item(PlanHobby, IntervalMonthly, 1000, "null") + `},` +
		`"pro":{"monthly":` + item(PlanPro, IntervalMonthly, 1000, expectedOverage) + `,"yearly":` + item(PlanPro, IntervalYearly, 10000, expectedOverage) + `},` +
		`"scale":{"monthly":` + item(PlanScale, IntervalMonthly, 1000, expectedOverage) + `,"yearly":` + item(PlanScale, IntervalYearly, 10000, expectedOverage) + `}}`
	if got := jsonString(t, c.Display()); got != want {
		t.Fatalf("display\n got %s\nwant %s", got, want)
	}
}

// forma2's "omits responseOverage for volume-mode response prices".
func TestCatalogVolumeModeHasNoOverageTable(t *testing.T) {
	prices := standardPrices()
	prices[3].(map[string]any)["tiers_mode"] = "volume"
	d := mustCatalog(t, prices...).Display()
	if d.Pro.Monthly.ResponseOverage != nil || jsonString(t, d.Scale.Monthly.ResponseOverage) != expectedOverage {
		t.Fatalf("overages %v %v", d.Pro.Monthly.ResponseOverage, d.Scale.Monthly.ResponseOverage)
	}
}

// forma2's workflow_runs cases: its own metered line item, classified by its kind rather than read as a second responses price; absent when the plan has none; ambiguous when two exist.
func TestCatalogWorkflowRunsPrice(t *testing.T) {
	c := mustCatalog(t, append(standardPrices(), workflowRunsPrice("price_scale_workflow_runs"))...)
	if got := itemsOf(t, c, PlanScale, IntervalMonthly); !reflect.DeepEqual(got, []string{"price_scale_monthlyx1", "price_scale_responses", "price_scale_workflow_runs"}) {
		t.Errorf("scale items %v", got)
	}
	d := c.Display()
	if d.Scale.Monthly.WorkflowRunsIncluded == nil || *d.Scale.Monthly.WorkflowRunsIncluded != 2000 || d.Scale.Yearly.WorkflowRunsIncluded == nil || *d.Scale.Yearly.WorkflowRunsIncluded != 2000 {
		t.Errorf("included volume %+v", d.Scale)
	}
	if d.Pro.Monthly.WorkflowRunsIncluded != nil || d.Hobby.Monthly.WorkflowRunsIncluded != nil || d.Pro.Monthly.WorkflowRunsOverage != nil {
		t.Errorf("plans without a workflow price show one: %+v %+v", d.Pro, d.Hobby)
	}
	if jsonString(t, d.Scale.Monthly.WorkflowRunsOverage) != expectedOverage {
		t.Errorf("workflow overage %s", jsonString(t, d.Scale.Monthly.WorkflowRunsOverage))
	}

	without := mustCatalog(t, standardPrices()...)
	if got := itemsOf(t, without, PlanScale, IntervalMonthly); !reflect.DeepEqual(got, []string{"price_scale_monthlyx1", "price_scale_responses"}) {
		t.Errorf("scale items without a workflow price %v", got)
	}

	_, err := buildCatalog(decodePrices(t, append(standardPrices(), workflowRunsPrice("price_a"), workflowRunsPrice("price_b"))...), discardLog())
	if err == nil || err.Error() != "expected at most one Stripe price for scale/workflow_runs/monthly, but found 2" {
		t.Fatalf("duplicate workflow prices: %v", err)
	}
}

// forma2's "degrades the workflow item only": a workflow price charging from the first run would make the usage card promise free runs Stripe bills, so only that item is dropped, loudly.
func TestCatalogDegradesAWorkflowPriceWithoutAFreeTier(t *testing.T) {
	paid := workflowRunsPrice("price_scale_workflow_runs")
	paid["tiers"] = []any{
		map[string]any{"unit_amount": 8, "unit_amount_decimal": "8", "up_to": 2000},
		map[string]any{"unit_amount": 2, "unit_amount_decimal": "2", "up_to": nil},
	}
	var logs bytes.Buffer
	c, err := buildCatalog(decodePrices(t, append(standardPrices(), paid)...), slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	d := c.Display()
	if d.Scale.Monthly.WorkflowRunsIncluded != nil || d.Scale.Monthly.WorkflowRunsOverage != nil {
		t.Errorf("degraded item still shown: %+v", d.Scale.Monthly)
	}
	if got := itemsOf(t, c, PlanScale, IntervalMonthly); !reflect.DeepEqual(got, []string{"price_scale_monthlyx1", "price_scale_responses"}) {
		t.Errorf("scale items %v", got)
	}
	if !strings.Contains(logs.String(), "Invalid workflow_runs price") {
		t.Error("the degraded price was not logged")
	}
}

// forma2's "fails fast when the catalog is incomplete", and an empty catalog.
func TestCatalogIncomplete(t *testing.T) {
	_, err := buildCatalog(decodePrices(t, price("price_hobby_monthly", PlanHobby, kindBase, IntervalMonthly)), discardLog())
	if err == nil || err.Error() != "expected exactly one Stripe price for pro/base/monthly, but found 0" {
		t.Fatalf("incomplete catalog: %v", err)
	}
	if _, err := buildCatalog(nil, discardLog()); err == nil || err.Error() != "no active Stripe billing catalog prices found" {
		t.Fatalf("empty catalog: %v", err)
	}
}

// forma2's price classification: metadata first, then the product and the recurring settings; an unknown kind keeps a separate metered product out of the catalog.
func TestPriceClassification(t *testing.T) {
	ps := decodePrices(t,
		map[string]any{"id": "a", "active": true, "metadata": map[string]any{}, "recurring": map[string]any{"usage_type": "licensed", "interval": "year"}, "product": product(PlanPro)},
		map[string]any{"id": "b", "active": true, "metadata": map[string]any{"forma_price_kind": "seats"}, "recurring": map[string]any{"usage_type": "metered", "interval": "month"}, "product": product(PlanPro)},
		map[string]any{"id": "c", "active": true, "metadata": map[string]any{"forma_plan": "custom"}, "recurring": map[string]any{"usage_type": "licensed", "interval": "month"}, "product": product(PlanPro)},
		map[string]any{"id": "d", "active": true, "metadata": map[string]any{}, "recurring": map[string]any{"usage_type": "metered", "interval": "month"}, "product": map[string]any{"id": "prod_x", "object": "product", "active": false, "metadata": map[string]any{"forma_plan": "pro"}}},
	)
	if pricePlan(ps[0]) != PlanPro || priceKind(ps[0]) != kindBase || priceInterval(ps[0]) != IntervalYearly || !catalogCandidate(ps[0]) {
		t.Errorf("a licensed yearly price of the Pro product is Pro's yearly base")
	}
	if priceKind(ps[1]) != "" || catalogCandidate(ps[1]) {
		t.Errorf("a price of an unknown kind is part of the catalog")
	}
	if pricePlan(ps[2]) != "" {
		t.Errorf("the price's own non-standard plan must win over its product's")
	}
	if catalogCandidate(ps[3]) {
		t.Errorf("a price of an inactive product is part of the catalog")
	}
}

// forma2's stripe-plan tests: the product's forma_plan, unknown otherwise.
func TestProductPlan(t *testing.T) {
	cases := map[string]*stripe.Product{
		PlanHobby:   {Object: "product", Metadata: map[string]string{"forma_plan": "hobby"}},
		PlanCustom:  {Object: "product", Metadata: map[string]string{"forma_plan": "custom"}},
		PlanUnknown: {Object: "product", Metadata: map[string]string{"forma_plan": "enterprise"}},
		"":          {ID: "prod_unexpanded"},
		"deleted":   {Object: "product", Deleted: true, Metadata: map[string]string{"forma_plan": "pro"}},
	}
	for want, p := range cases {
		if want == "" || want == "deleted" {
			want = PlanUnknown
		}
		if got := productPlan(p); got != want {
			t.Errorf("productPlan(%+v) = %s, want %s", p, got, want)
		}
	}
	if productPlan(nil) != PlanUnknown {
		t.Error("no product is unknown")
	}
}

// forma2's mapStripeTiersToResponsePricingTiers tests.
func TestPricingTiers(t *testing.T) {
	tiers := func(v ...any) []*stripe.PriceTier {
		var out []*stripe.PriceTier
		if err := json.Unmarshal([]byte(jsonString(t, v)), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	tier := func(upTo, unit any) map[string]any {
		return map[string]any{"up_to": upTo, "unit_amount": unit, "unit_amount_decimal": nil, "flat_amount": nil}
	}
	if got := jsonString(t, pricingTiers(tiers(tier(2000, 0), tier(5000, 8), tier(nil, 2)))); got != `[{"firstUnit":0,"lastUnit":2000,"perUnitCents":0},{"firstUnit":2001,"lastUnit":5000,"perUnitCents":8},{"firstUnit":5001,"lastUnit":null,"perUnitCents":2}]` {
		t.Errorf("graduated tiers %s", got)
	}
	fractional := map[string]any{"up_to": nil, "unit_amount": nil, "unit_amount_decimal": "0.5"}
	if got := jsonString(t, pricingTiers(tiers(tier(1000, 0), fractional))); got != `[{"firstUnit":0,"lastUnit":1000,"perUnitCents":0},{"firstUnit":1001,"lastUnit":null,"perUnitCents":0.5}]` {
		t.Errorf("fractional cents %s", got)
	}
	if pricingTiers(nil) != nil || pricingTiers(tiers()) != nil {
		t.Error("missing tiers map to a table")
	}
	if pricingTiers(tiers(tier(nil, 8), tier(5000, 2))) != nil {
		t.Error("an unbounded non-final tier maps to a table")
	}
	flatOnly := map[string]any{"up_to": nil, "unit_amount": nil, "unit_amount_decimal": nil, "flat_amount": 500, "flat_amount_decimal": "500"}
	if pricingTiers(tiers(tier(2000, 0), flatOnly)) != nil {
		t.Error("a tier without a per-unit amount maps to a table")
	}
}

// The catalog is read from Stripe once, page by page with products and tiers expanded, and reused for ten minutes; a failed read is not cached.
func TestCatalogIsCached(t *testing.T) {
	f := newFakeStripe(t)
	fail := true
	f.on("GET /v1/prices", func(r *http.Request) any {
		if r.Form.Get("starting_after") == "" {
			return map[string]any{"object": "list", "data": standardPrices()[:3], "has_more": true, "url": "/v1/prices"}
		}
		if fail {
			return list()
		}
		return list(standardPrices()[3:]...)
	})
	s := stripeFor(t, config.Config{}, f, discardLog())
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if _, err := s.Catalog(context.Background()); err == nil {
		t.Fatal("an incomplete catalog was accepted")
	}
	fail = false
	if _, err := s.Catalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := f.requests(http.MethodGet, "/v1/prices")
	if len(calls) != 4 {
		t.Fatalf("%d price list calls, want two pages twice", len(calls))
	}
	first := calls[2].Form
	if first.Get("active") != "true" || first.Get("limit") != "100" || first.Get("expand[0]") != "data.product" || first.Get("expand[1]") != "data.tiers" {
		t.Errorf("list params %v", first)
	}
	if calls[3].Form.Get("starting_after") != "price_pro_yearly" {
		t.Errorf("second page %v", calls[3].Form)
	}
	now = now.Add(catalogTTL - time.Second)
	if _, err := s.Catalog(context.Background()); err != nil || len(f.requests(http.MethodGet, "/v1/prices")) != 4 {
		t.Fatalf("a fresh catalog was read again: %v", err)
	}
	now = now.Add(2 * time.Second)
	if _, err := s.Catalog(context.Background()); err != nil || len(f.requests(http.MethodGet, "/v1/prices")) != 6 {
		t.Fatalf("an expired catalog was not read again: %v", err)
	}
}
