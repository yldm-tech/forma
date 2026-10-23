import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// The Forma Cloud billing routes of /api/app (apps/server/internal/billing/handler.go), one per forma2 billing server action.

/** forma2's TCloudBillingPlan: the three standard plans, a custom contract, and a subscription whose product carries no recognised plan. */
export type CloudPlan = "hobby" | "pro" | "scale" | "custom" | "unknown";
export type StandardPlan = "hobby" | "pro" | "scale";
export type PaidPlan = "pro" | "scale";
export type BillingInterval = "monthly" | "yearly";

export type SubscriptionStatus =
  | "trialing"
  | "active"
  | "past_due"
  | "unpaid"
  | "paused"
  | "canceled"
  | "incomplete"
  | "incomplete_expired";

/** forma2's TOrganizationBillingPlanLimits; null is unlimited. */
export interface BillingLimits {
  workspaces: number | null;
  monthly: { responses: number | null; workflowRuns?: number | null };
}

/** A plan change Stripe applies later: a scheduled downgrade, or a cancellation at period end that drops the organization to Hobby. */
export interface PendingChange {
  type: "plan_change";
  targetPlan: StandardPlan;
  targetInterval: BillingInterval | null;
  effectiveAt: string;
}

export interface PaymentAttemptError {
  type: "requires_action" | "failed_invoice";
  paymentIntentId: string;
  message: string;
  createdAt: string;
}

/** The organization's Stripe state as the last sync saw it, forma2's TOrganizationStripeBilling. */
export interface BillingSnapshot {
  plan?: CloudPlan;
  interval: BillingInterval | null;
  subscriptionStatus: SubscriptionStatus | null;
  subscriptionId: string | null;
  hasPaymentMethod?: boolean;
  features?: string[];
  trialEnd: string | null;
  pendingChange: PendingChange | null;
  paymentAttemptError: PaymentAttemptError | null;
}

export interface BillingRecord {
  stripeCustomerId: string | null;
  limits: BillingLimits;
  usageCycleAnchor: string | null;
  stripe?: BillingSnapshot;
}

/** One band of a graduated metered price. */
export interface PricingTier {
  firstUnit: number;
  lastUnit: number | null;
  perUnitCents: number;
}

export interface Overage {
  currency: string;
  tiers: PricingTier[];
}

/** One plan and interval as the billing page shows it, forma2's TStripeBillingCatalogDisplayItem. */
export interface CatalogItem {
  plan: StandardPlan;
  interval: BillingInterval;
  currency: string;
  /** The base price in the currency's minor unit; null when Stripe gives it no fixed amount. */
  unitAmount: number | null;
  responseOverage: Overage | null;
  workflowRunsIncluded: number | null;
  workflowRunsOverage: Overage | null;
}

export interface CatalogDisplay {
  hobby: { monthly: CatalogItem };
  pro: Record<BillingInterval, CatalogItem>;
  scale: Record<BillingInterval, CatalogItem>;
}

/** GET /api/app/organizations/{id}/billing: forma2's PricingPage props. Forma Cloud only; a not-found elsewhere. */
export interface BillingPage {
  billing: BillingRecord;
  currentCloudPlan: CloudPlan;
  currentBillingInterval: BillingInterval | null;
  currentSubscriptionStatus: SubscriptionStatus | null;
  pendingChange: PendingChange | null;
  trialDaysRemaining: number | null;
  usageCycleStart: string;
  usageCycleEnd: string;
  billingCatalog: CatalogDisplay;
  responseCount: number;
  workspaceCount: number;
  workflowRunCount: number;
  isPlanComparison: boolean;
  hasBillingRights: boolean;
  isStripeSetupIncomplete: boolean;
  stripePublishableKey: string | null;
}

/** GET /api/app/organizations/{id}/billing/summary: what the layouts' limits banner and trial notice, the settings sidebar and the onboarding plan step read. Usage is counted on Forma Cloud only. */
export interface BillingSummary {
  isFormaCloud: boolean;
  plan: CloudPlan;
  subscriptionStatus: SubscriptionStatus | null;
  trialEnd: string | null;
  trialDaysRemaining: number | null;
  /** How long a Pro trial started now runs, forma2's getProTrialDays: 14 days, 7 in its a-b_billing_shorten-trial-days experiment's test variant. */
  proTrialDays: number;
  limits: BillingLimits;
  usage: { responses: number; workflowRuns: number; workspaces: number } | null;
  usageCycle: { start: string; end: string } | null;
}

export interface ChargePreview {
  amountDue: number;
  currency: string;
}

/** The answer to a plan change; a clientSecret with requiresAction means the browser must confirm 3D Secure. */
export interface PlanChange {
  mode: "immediate" | "scheduled";
  pendingChange: PendingChange | null;
  clientSecret: string | null;
  requiresAction: boolean;
}

export interface SetupUpgrade {
  mode: "immediate" | "scheduled";
  clientSecret: string | null;
  requiresAction: boolean;
  targetPlan: PaidPlan | null;
}

export const billingKeys = {
  all: (organizationId: string) => ["organizations", organizationId, "billing"] as const,
  page: (organizationId: string) => ["organizations", organizationId, "billing", "page"] as const,
  summary: (organizationId: string) => ["organizations", organizationId, "billing", "summary"] as const,
};

const base = (organizationId: string) => `/api/app/organizations/${organizationId}/billing`;

export const billingPageQuery = (organizationId: string) =>
  queryOptions({
    queryKey: billingKeys.page(organizationId),
    queryFn: ({ signal }) => api.get<BillingPage>(base(organizationId), { signal }),
    staleTime: 0,
  });

export const billingSummaryQuery = (organizationId: string) =>
  queryOptions({
    queryKey: billingKeys.summary(organizationId),
    queryFn: ({ signal }) => api.get<BillingSummary>(`${base(organizationId)}/summary`, { signal }),
  });

/** The billing actions, each forma2's server action of the same purpose. */
export const billingApi = {
  previewUpgrade: (organizationId: string, targetPlan: PaidPlan, targetInterval: BillingInterval) =>
    api
      .get<{
        preview: ChargePreview | null;
      }>(
        `${base(organizationId)}/upgrade-preview?${new URLSearchParams({ targetPlan, targetInterval }).toString()}`
      )
      .then((res) => res.preview),
  portal: (organizationId: string) =>
    api.post<{ url: string }>(`${base(organizationId)}/portal`).then((res) => res.url),
  checkout: (organizationId: string, targetPlan: PaidPlan, targetInterval: BillingInterval) =>
    api
      .post<{ url: string }>(`${base(organizationId)}/checkout`, { targetPlan, targetInterval })
      .then((res) => res.url),
  setupCheckout: (
    organizationId: string,
    intent?: { targetPlan: PaidPlan; targetInterval: BillingInterval }
  ) =>
    api.post<{ url: string }>(`${base(organizationId)}/setup-checkout`, intent ?? {}).then((res) => res.url),
  finalizeSetupCheckout: (organizationId: string, checkoutSessionId: string) =>
    api.post<SetupUpgrade>(`${base(organizationId)}/setup-checkout/finalize`, { checkoutSessionId }),
  changePlan: (organizationId: string, targetPlan: StandardPlan, targetInterval: BillingInterval) =>
    api.post<PlanChange>(`${base(organizationId)}/plan`, { targetPlan, targetInterval }),
  undoPendingChange: (organizationId: string) =>
    api.delete<{ success: boolean }>(`${base(organizationId)}/pending-change`),
  reportPaymentIssue: (organizationId: string, paymentIntentId: string) =>
    api.post<{ success: boolean }>(`${base(organizationId)}/payment-issue`, { paymentIntentId }),
  retrySetup: (organizationId: string) =>
    api.post<{ success: boolean }>(`${base(organizationId)}/retry-setup`),
  startHobby: (organizationId: string) => api.post<{ success: boolean }>(`${base(organizationId)}/hobby`),
  startProTrial: (organizationId: string) => api.post<{ success: boolean }>(`${base(organizationId)}/trial`),
  waitForPlan: (organizationId: string, targetPlan: StandardPlan) =>
    api
      .post<{ plan: CloudPlan | null }>(`${base(organizationId)}/wait/plan`, { targetPlan })
      .then((res) => res.plan),
  waitForPaymentMethod: (organizationId: string) =>
    api
      .post<{ hasPaymentMethod: boolean }>(`${base(organizationId)}/wait/payment-method`)
      .then((res) => res.hasPaymentMethod),
};
