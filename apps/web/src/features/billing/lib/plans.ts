import type { TFunction } from "i18next";
import { ApiError } from "../../../lib/api";
import type {
  BillingInterval,
  CatalogDisplay,
  CatalogItem,
  CloudPlan,
  PaidPlan,
  PendingChange,
  StandardPlan,
} from "../../../lib/queries/billing";

// The plan-card decisions of forma2's PricingTable (modules/billing/components/pricing-table.tsx), kept free of React so each rule can be tested on its own.

/** forma2's STANDARD_PLAN_LEVEL: the order the standard plans upgrade in. */
export const STANDARD_PLAN_LEVEL: Record<StandardPlan, number> = { hobby: 0, pro: 1, scale: 2 };

/** What the billing page knows about the organization's subscription when it decides a plan card's action. */
export interface PlanState {
  currentCloudPlan: CloudPlan;
  currentBillingInterval: BillingInterval | null;
  isTrialing: boolean;
  hasPaymentMethod: boolean;
  pendingChange: PendingChange | null;
}

/** The level of the current plan, null for a custom or unrecognised one. */
export const currentPlanLevel = ({ currentCloudPlan }: PlanState): number | null =>
  currentCloudPlan === "hobby" || currentCloudPlan === "pro" || currentCloudPlan === "scale"
    ? STANDARD_PLAN_LEVEL[currentCloudPlan]
    : null;

export const isCurrentPlanSelection = (state: PlanState, plan: StandardPlan, interval: BillingInterval) =>
  state.currentCloudPlan === plan && (plan === "hobby" || state.currentBillingInterval === interval);

export const isPendingSelection = (state: PlanState, plan: StandardPlan, interval: BillingInterval) =>
  state.pendingChange?.targetPlan === plan &&
  (plan === "hobby" || state.pendingChange.targetInterval === interval);

/** A paid organization choosing its own plan again cancels it at period end. A trial choosing its plan converts it instead, and nothing is cancelled twice. */
export const canCancelAtPeriodEnd = (state: PlanState, plan: StandardPlan, interval: BillingInterval) =>
  plan !== "hobby" &&
  !state.isTrialing &&
  state.pendingChange?.targetPlan !== "hobby" &&
  isCurrentPlanSelection(state, plan, interval);

/** A downgrade (or a same-level interval switch) that Stripe schedules for the end of the period, shown as a secondary button. */
export const isSwitchAtPeriodEnd = (state: PlanState, plan: StandardPlan, interval: BillingInterval) => {
  const level = currentPlanLevel(state);
  if (level === null) return false;
  if (isCurrentPlanSelection(state, plan, interval)) return false;
  if (isPendingSelection(state, plan, interval)) return false;
  if (!state.hasPaymentMethod && plan !== "hobby") return false;
  if (state.isTrialing && !state.hasPaymentMethod && plan === "hobby") return false;
  return STANDARD_PLAN_LEVEL[plan] <= level;
};

/** forma2's getCtaKey: which call to action a plan card shows. Each key is a message of workspace.settings.billing. */
export type PlanCta =
  | "unlock_all_plan_features"
  | "downgrade_to_hobby"
  | "upgrade_now"
  | "cancel_at_period_end"
  | "current_plan_cta"
  | "pending_plan_cta"
  | "switch_plan_now"
  | "switch_at_period_end";

export const planCta = (state: PlanState, plan: StandardPlan, interval: BillingInterval): PlanCta => {
  const isCurrent = isCurrentPlanSelection(state, plan, interval);
  const trialingWithoutPayment = state.isTrialing && !state.hasPaymentMethod;
  if (isCurrent && trialingWithoutPayment) return "unlock_all_plan_features";
  if (trialingWithoutPayment && plan === "hobby") return "downgrade_to_hobby";
  // A trial with a card converts on any paid plan, its own included, so that reads as an upgrade.
  if (state.isTrialing && state.hasPaymentMethod && plan !== "hobby") return "upgrade_now";
  if (canCancelAtPeriodEnd(state, plan, interval)) return "cancel_at_period_end";
  // The current plan stays current while a downgrade is scheduled; only the target shows as scheduled.
  if (isCurrent) return "current_plan_cta";
  if (isPendingSelection(state, plan, interval)) return "pending_plan_cta";
  if (!state.hasPaymentMethod && plan !== "hobby") return "upgrade_now";
  const level = currentPlanLevel(state);
  if (level === null) return "switch_plan_now";
  return STANDARD_PLAN_LEVEL[plan] > level ? "upgrade_now" : "switch_at_period_end";
};

/** forma2's getPlanCtaState: the card's badges and whether its button is a secondary or disabled one. */
export const planCtaState = (
  state: PlanState,
  plan: StandardPlan,
  interval: BillingInterval,
  {
    hasBillingRights,
    isStripeSetupIncomplete,
  }: { hasBillingRights: boolean; isStripeSetupIncomplete: boolean }
) => {
  const isCurrent = isCurrentPlanSelection(state, plan, interval);
  const isPending = isPendingSelection(state, plan, interval);
  const isCancel = canCancelAtPeriodEnd(state, plan, interval);
  const isSecondary = isCancel || isSwitchAtPeriodEnd(state, plan, interval);
  return {
    isCurrent,
    isPending,
    isSecondary,
    // The current plan's button is inert only when it really is the active plan: during a trial it opens the pay-now choice.
    isDisabled:
      !hasBillingRights ||
      (isCurrent && !state.isTrialing && !isCancel) ||
      isPending ||
      isStripeSetupIncomplete,
  };
};

/** A card-on-file change that bills at once and so goes behind the confirmation: an upgrade, or converting a trial. */
export const willChargeImmediately = (state: PlanState, plan: StandardPlan, interval: BillingInterval) => {
  if (plan === "hobby" || !state.hasPaymentMethod) return false;
  if (state.isTrialing) return true;
  const level = currentPlanLevel(state);
  return (
    level !== null &&
    STANDARD_PLAN_LEVEL[plan] > level &&
    !isCurrentPlanSelection(state, plan, interval) &&
    !canCancelAtPeriodEnd(state, plan, interval)
  );
};

/** A trial without a card moving to another paid plan adds a card and is charged at once. Continuing the trialed plan only saves a card. */
export const willChargeAfterAddingCard = (state: PlanState, plan: StandardPlan, interval: BillingInterval) =>
  state.isTrialing &&
  !state.hasPaymentMethod &&
  plan !== "hobby" &&
  !isCurrentPlanSelection(state, plan, interval);

/** The trialed plan's own card: pay now or keep the trial. */
export const isTrialContinue = (state: PlanState, plan: StandardPlan, interval: BillingInterval) =>
  state.isTrialing && plan !== "hobby" && isCurrentPlanSelection(state, plan, interval);

/** What a plan card's click does first, forma2's requestPlanAction. */
export type PlanRequest =
  | { kind: "confirm-hobby" }
  | { kind: "confirm"; mode: "upgrade" | "trial-continue" }
  | { kind: "run" };

export const planRequest = (state: PlanState, plan: StandardPlan, interval: BillingInterval): PlanRequest => {
  if (plan === "hobby") return state.isTrialing ? { kind: "confirm-hobby" } : { kind: "run" };
  if (isTrialContinue(state, plan, interval) && !state.hasPaymentMethod)
    return { kind: "confirm", mode: "trial-continue" };
  if (willChargeImmediately(state, plan, interval) || willChargeAfterAddingCard(state, plan, interval))
    return { kind: "confirm", mode: "upgrade" };
  return { kind: "run" };
};

/** The catalog item of the current plan and interval, null for a custom or unrecognised plan. */
export const currentCatalogItem = (
  catalog: CatalogDisplay,
  plan: CloudPlan,
  interval: BillingInterval
): CatalogItem | null => {
  if (plan === "hobby") return catalog.hobby.monthly;
  if (plan === "pro" || plan === "scale") return catalog[plan][interval];
  return null;
};

export const planLabel = (t: TFunction, plan: CloudPlan): string => {
  if (plan === "hobby") return t("workspace.settings.billing.plan_hobby");
  if (plan === "pro") return t("workspace.settings.billing.plan_pro");
  if (plan === "scale") return t("workspace.settings.billing.plan_scale");
  if (plan === "custom") return t("workspace.settings.billing.plan_custom");
  return t("workspace.settings.billing.plan_unknown");
};

export const periodLabel = (t: TFunction, plan: StandardPlan, interval: BillingInterval): string =>
  plan === "hobby" || interval === "monthly"
    ? t("workspace.settings.billing.per_month")
    : t("workspace.settings.billing.per_year");

export const ctaLabel = (t: TFunction, cta: PlanCta, plan: StandardPlan): string => {
  switch (cta) {
    case "unlock_all_plan_features":
      return t("workspace.settings.billing.unlock_all_plan_features", { plan: planLabel(t, plan) });
    case "downgrade_to_hobby":
      return t("workspace.settings.billing.downgrade_to_hobby");
    case "upgrade_now":
      return t("workspace.settings.billing.upgrade_now");
    case "cancel_at_period_end":
      return t("workspace.settings.billing.cancel_at_period_end");
    case "current_plan_cta":
      return t("workspace.settings.billing.current_plan_cta");
    case "pending_plan_cta":
      return t("workspace.settings.billing.pending_plan_cta");
    case "switch_plan_now":
      return t("workspace.settings.billing.switch_plan_now");
    case "switch_at_period_end":
      return t("workspace.settings.billing.switch_at_period_end");
  }
};

/** forma2's formatMoney: an amount in the currency's minor unit, without decimals when it is whole; a dash when there is none. */
export const formatMoney = (currency: string, unitAmount: number | null, locale: string): string => {
  if (unitAmount == null) return "—";
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency: currency.toUpperCase(),
    minimumFractionDigits: unitAmount % 100 === 0 ? 0 : 2,
  }).format(unitAmount / 100);
};

/** A date of the billing page, in UTC as forma2 showed it. */
export const formatBillingDate = (value: string | Date, locale: string): string =>
  new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(typeof value === "string" ? new Date(value) : value);

/** forma2's getActionErrorMessage: the billing error codes the page explains, and a generic retry prompt for anything else. */
export const billingErrorMessage = (t: TFunction, error: unknown): string => {
  const code = error instanceof ApiError ? error.code : null;
  if (code === "mixed_interval_checkout_unsupported")
    return t("workspace.settings.billing.yearly_checkout_unavailable");
  if (code === "payment_method_required") return t("workspace.settings.billing.payment_method_required");
  // A trial conversion charges synchronously, so a card that needs 3D Secure fails with nothing left to confirm.
  if (code === "card_authentication_required")
    return t("workspace.settings.billing.payment_authentication_failed");
  return t("common.something_went_wrong_please_try_again");
};

/** The trial's paid plan, the target of the trial banner's pay-now button. */
export const trialedPaidPlan = (state: PlanState): PaidPlan | null =>
  state.isTrialing && (state.currentCloudPlan === "pro" || state.currentCloudPlan === "scale")
    ? state.currentCloudPlan
    : null;
