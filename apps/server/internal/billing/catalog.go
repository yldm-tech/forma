package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/stripe/stripe-go/v84"
)

// Price kinds, forma2's TStripePriceKind: the plan's licensed base price, the metered response overage, and the metered workflow-run overage.
const (
	kindBase         = "base"
	kindResponses    = "responses"
	kindWorkflowRuns = "workflow_runs"
)

// catalogTTL is how long a fetched catalog is reused, forma2's STRIPE_BILLING_CATALOG_CACHE_TTL_MS.
const catalogTTL = 10 * time.Minute

// errStripeNotConfigured is an operation that needs Stripe on an instance without STRIPE_SECRET_KEY.
var errStripeNotConfigured = errors.New("stripe is not configured")

// standardPlan reports whether plan is one the catalog sells.
func standardPlan(plan string) bool {
	return plan == PlanHobby || plan == PlanPro || plan == PlanScale
}

// productPlan is forma2's getCloudPlanFromProduct: the product's forma_plan metadata, unknown when the product is missing, not expanded, deleted, or tagged with anything else.
func productPlan(p *stripe.Product) string {
	if p == nil || p.Object == "" || p.Deleted {
		return PlanUnknown
	}
	switch plan := p.Metadata["forma_plan"]; plan {
	case PlanHobby, PlanPro, PlanScale, PlanCustom:
		return plan
	default:
		return PlanUnknown
	}
}

// pricePlan is forma2's getPricePlan: the price's forma_plan metadata, else its product's, and only a standard plan.
func pricePlan(p *stripe.Price) string {
	if p == nil {
		return ""
	}
	plan, ok := p.Metadata["forma_plan"]
	if !ok && p.Product != nil && p.Product.Object != "" && !p.Product.Deleted {
		plan = p.Product.Metadata["forma_plan"]
	}
	if !standardPlan(plan) {
		return ""
	}
	return plan
}

func normalizeInterval(v string) string {
	switch v {
	case "month", IntervalMonthly:
		return IntervalMonthly
	case "year", IntervalYearly:
		return IntervalYearly
	default:
		return ""
	}
}

// priceInterval is forma2's getPriceInterval: the forma_interval metadata, else the recurring interval.
func priceInterval(p *stripe.Price) string {
	if p == nil {
		return ""
	}
	if v := normalizeInterval(p.Metadata["forma_interval"]); v != "" {
		return v
	}
	if p.Recurring == nil {
		return ""
	}
	return normalizeInterval(string(p.Recurring.Interval))
}

// priceKind is forma2's getPriceKind: the forma_price_kind metadata, else licensed is the base price and metered the responses price. A price tagged with an unrecognised kind is a separate metered product and belongs to no plan.
func priceKind(p *stripe.Price) string {
	if p == nil {
		return ""
	}
	switch kind := p.Metadata["forma_price_kind"]; kind {
	case kindBase, kindResponses, kindWorkflowRuns:
		return kind
	case "":
	default:
		return ""
	}
	if p.Recurring == nil {
		return ""
	}
	switch p.Recurring.UsageType {
	case stripe.PriceRecurringUsageTypeLicensed:
		return kindBase
	case stripe.PriceRecurringUsageTypeMetered:
		return kindResponses
	default:
		return ""
	}
}

// catalogCandidate is forma2's isCatalogCandidate: an active recurring price of an active product, with a plan, a kind and an interval.
func catalogCandidate(p *stripe.Price) bool {
	if !p.Active || p.Recurring == nil || p.Product == nil || p.Product.Object == "" || p.Product.Deleted || !p.Product.Active {
		return false
	}
	return pricePlan(p) != "" && priceKind(p) != "" && priceInterval(p) != ""
}

// CatalogItem is one plan and interval as Stripe sells it, forma2's TStripeBillingCatalogItem. The response and workflow-run prices are always the monthly metered ones, even with a yearly base, because usage is billed monthly.
type CatalogItem struct {
	Plan              string
	Interval          string
	BasePrice         *stripe.Price
	ResponsePrice     *stripe.Price
	WorkflowRunsPrice *stripe.Price
	// WorkflowRunsIncluded is the free volume of the workflow-run price's first tier: the number the usage card shows is the number Stripe leaves uncharged.
	WorkflowRunsIncluded *int64
}

// lineItem is one item of a subscription or checkout: the base price with quantity 1, a metered price without one.
type lineItem struct {
	Price    string
	Quantity *int64
}

// lineItems is forma2's getCatalogItemsForPlan.
func (c *CatalogItem) lineItems() []lineItem {
	items := []lineItem{{Price: c.BasePrice.ID, Quantity: ptr[int64](1)}}
	if c.ResponsePrice != nil {
		items = append(items, lineItem{Price: c.ResponsePrice.ID})
	}
	if c.WorkflowRunsPrice != nil {
		items = append(items, lineItem{Price: c.WorkflowRunsPrice.ID})
	}
	return items
}

// Catalog is forma2's TStripeBillingCatalog: Hobby is monthly only, Pro and Scale are sold monthly and yearly.
type Catalog struct {
	items map[string]*CatalogItem
}

// Item is forma2's getCatalogItemForPlan: Hobby ignores the interval.
func (c *Catalog) Item(plan, interval string) (*CatalogItem, error) {
	if plan == PlanHobby {
		interval = IntervalMonthly
	}
	item, ok := c.items[plan+"/"+interval]
	if !ok {
		return nil, fmt.Errorf("no Stripe catalog item for %s/%s", plan, interval)
	}
	return item, nil
}

// buildCatalog is forma2's fetchStripeBillingCatalog over the candidate prices. A missing or duplicated base or responses price fails the whole catalog; a misconfigured workflow-run price only drops that item, so checkout and the billing page stay up.
func buildCatalog(prices []*stripe.Price, log *slog.Logger) (*Catalog, error) {
	if len(prices) == 0 {
		return nil, errors.New("no active Stripe billing catalog prices found")
	}
	matches := func(plan, kind, interval string) []*stripe.Price {
		var out []*stripe.Price
		for _, p := range prices {
			if pricePlan(p) == plan && priceKind(p) == kind && priceInterval(p) == interval {
				out = append(out, p)
			}
		}
		return out
	}
	single := func(plan, kind, interval string) (*stripe.Price, error) {
		m := matches(plan, kind, interval)
		if len(m) != 1 {
			return nil, fmt.Errorf("expected exactly one Stripe price for %s/%s/%s, but found %d", plan, kind, interval, len(m))
		}
		return m[0], nil
	}
	c := &Catalog{items: map[string]*CatalogItem{}}
	for _, entry := range []struct {
		plan, interval string
		responses      bool
	}{
		{PlanHobby, IntervalMonthly, false},
		{PlanPro, IntervalMonthly, true}, {PlanPro, IntervalYearly, true},
		{PlanScale, IntervalMonthly, true}, {PlanScale, IntervalYearly, true},
	} {
		item := &CatalogItem{Plan: entry.plan, Interval: entry.interval}
		var err error
		if item.BasePrice, err = single(entry.plan, kindBase, entry.interval); err != nil {
			return nil, err
		}
		if entry.responses {
			if item.ResponsePrice, err = single(entry.plan, kindResponses, IntervalMonthly); err != nil {
				return nil, err
			}
		}
		workflow := matches(entry.plan, kindWorkflowRuns, IntervalMonthly)
		if len(workflow) > 1 {
			return nil, fmt.Errorf("expected at most one Stripe price for %s/%s/%s, but found %d", entry.plan, kindWorkflowRuns, IntervalMonthly, len(workflow))
		}
		if len(workflow) == 1 {
			included, err := workflowIncludedVolume(workflow[0])
			if err != nil {
				log.Error("Invalid workflow_runs price; excluding it from the catalog so the rest of billing stays up", "err", err, "plan", entry.plan, "priceId", workflow[0].ID)
			} else {
				item.WorkflowRunsPrice, item.WorkflowRunsIncluded = workflow[0], &included
			}
		}
		c.items[entry.plan+"/"+entry.interval] = item
	}
	return c, nil
}

// workflowIncludedVolume is forma2's resolveWorkflowIncludedVolume: a workflow-run price must be graduated with a free first tier up to a finite boundary, which is the included volume; any other shape would let the usage card promise runs Stripe charges for.
func workflowIncludedVolume(p *stripe.Price) (int64, error) {
	if p.TiersMode != stripe.PriceTiersModeGraduated || len(p.Tiers) == 0 {
		return 0, fmt.Errorf("metered workflow price %s must be graduated with a free first tier, got tiers_mode=%q", p.ID, p.TiersMode)
	}
	first := p.Tiers[0]
	// An unbounded tier decodes with up_to 0, which Stripe never uses for a bounded one.
	if first.UnitAmount != 0 || first.FlatAmount != 0 || first.UpTo <= 0 {
		return 0, fmt.Errorf("metered workflow price %s must start with a free tier with a finite up_to, got unit_amount=%d flat_amount=%d up_to=%d", p.ID, first.UnitAmount, first.FlatAmount, first.UpTo)
	}
	return first.UpTo, nil
}

// PricingTier is one band of a graduated metered price, forma2's TResponsePricingTier.
type PricingTier struct {
	FirstUnit    int64   `json:"firstUnit"`
	LastUnit     *int64  `json:"lastUnit"`
	PerUnitCents float64 `json:"perUnitCents"`
}

// pricingTiers is forma2's mapStripeTiersToResponsePricingTiers: contiguous ranges from 0, nil when a non-final tier is unbounded or a tier has no per-unit amount. Stripe sends null for an absent amount and the SDK decodes it as 0, so a tier whose only amount is a flat one is the tier without a per-unit amount.
func pricingTiers(tiers []*stripe.PriceTier) []PricingTier {
	if len(tiers) == 0 {
		return nil
	}
	out := make([]PricingTier, 0, len(tiers))
	var first int64
	for i, t := range tiers {
		last := i == len(tiers)-1
		if t.UpTo == 0 && !last {
			return nil
		}
		perUnit := float64(t.UnitAmount)
		if t.UnitAmount == 0 {
			perUnit = t.UnitAmountDecimal
		}
		if perUnit == 0 && (t.FlatAmount != 0 || t.FlatAmountDecimal != 0) {
			return nil
		}
		tier := PricingTier{FirstUnit: first, PerUnitCents: perUnit}
		if t.UpTo != 0 {
			tier.LastUnit = ptr(t.UpTo)
			first = t.UpTo + 1
		}
		out = append(out, tier)
	}
	return out
}

// Overage is a metered price's tier table for the plan card, forma2's TResponseOverageDisplay.
type Overage struct {
	Currency string        `json:"currency"`
	Tiers    []PricingTier `json:"tiers"`
}

// overage is forma2's toOverageDisplay: only graduated prices, whose bands are priced separately, can be shown as a tier table.
func overage(p *stripe.Price) *Overage {
	if p == nil || p.TiersMode != stripe.PriceTiersModeGraduated {
		return nil
	}
	tiers := pricingTiers(p.Tiers)
	if tiers == nil {
		return nil
	}
	return &Overage{Currency: string(p.Currency), Tiers: tiers}
}

// DisplayItem is one plan and interval as the billing page shows it, forma2's TStripeBillingCatalogDisplayItem.
type DisplayItem struct {
	Plan                 string   `json:"plan"`
	Interval             string   `json:"interval"`
	Currency             string   `json:"currency"`
	UnitAmount           *int64   `json:"unitAmount"`
	ResponseOverage      *Overage `json:"responseOverage"`
	WorkflowRunsIncluded *int64   `json:"workflowRunsIncluded"`
	WorkflowRunsOverage  *Overage `json:"workflowRunsOverage"`
}

// CatalogDisplay is forma2's TStripeBillingCatalogDisplay.
type CatalogDisplay struct {
	Hobby struct {
		Monthly DisplayItem `json:"monthly"`
	} `json:"hobby"`
	Pro   IntervalDisplay `json:"pro"`
	Scale IntervalDisplay `json:"scale"`
}

// IntervalDisplay is a paid plan's monthly and yearly items.
type IntervalDisplay struct {
	Monthly DisplayItem `json:"monthly"`
	Yearly  DisplayItem `json:"yearly"`
}

func displayItem(c *CatalogItem) DisplayItem {
	d := DisplayItem{
		Plan: c.Plan, Interval: c.Interval, Currency: string(c.BasePrice.Currency),
		ResponseOverage: overage(c.ResponsePrice), WorkflowRunsIncluded: c.WorkflowRunsIncluded, WorkflowRunsOverage: overage(c.WorkflowRunsPrice),
	}
	// A tiered or customer-chosen price has no unit_amount; the SDK decodes the null as 0.
	if c.BasePrice.BillingScheme != stripe.PriceBillingSchemeTiered && c.BasePrice.CustomUnitAmount == nil {
		d.UnitAmount = ptr(c.BasePrice.UnitAmount)
	}
	return d
}

// Display is forma2's getStripeBillingCatalogDisplay.
func (c *Catalog) Display() CatalogDisplay {
	var d CatalogDisplay
	d.Hobby.Monthly = displayItem(c.items[PlanHobby+"/"+IntervalMonthly])
	d.Pro = IntervalDisplay{Monthly: displayItem(c.items[PlanPro+"/"+IntervalMonthly]), Yearly: displayItem(c.items[PlanPro+"/"+IntervalYearly])}
	d.Scale = IntervalDisplay{Monthly: displayItem(c.items[PlanScale+"/"+IntervalMonthly]), Yearly: displayItem(c.items[PlanScale+"/"+IntervalYearly])}
	return d
}

// catalogCache holds the account's catalog for catalogTTL. forma2 shared it across processes in Redis; each process here reads it from Stripe at most once per TTL, which bounds staleness the same way.
type catalogCache struct {
	mu      sync.Mutex
	catalog *Catalog
	fetched time.Time
}

// Catalog is forma2's getStripeBillingCatalog: every active price, with its product and tiers expanded, read page by page and cached.
func (s *Stripe) Catalog(ctx context.Context) (*Catalog, error) {
	if s.api == nil {
		return nil, errStripeNotConfigured
	}
	s.catalog.mu.Lock()
	defer s.catalog.mu.Unlock()
	if s.catalog.catalog != nil && s.now().Sub(s.catalog.fetched) < catalogTTL {
		return s.catalog.catalog, nil
	}
	params := &stripe.PriceListParams{Active: stripe.Bool(true)}
	params.Limit = stripe.Int64(100)
	params.AddExpand("data.product")
	params.AddExpand("data.tiers")
	var prices []*stripe.Price
	for p, err := range s.api.V1Prices.List(ctx, params) {
		if err != nil {
			return nil, fmt.Errorf("list Stripe prices: %w", err)
		}
		if catalogCandidate(p) {
			prices = append(prices, p)
		}
	}
	c, err := buildCatalog(prices, s.log)
	if err != nil {
		return nil, err
	}
	s.catalog.catalog, s.catalog.fetched = c, s.now()
	return c, nil
}
