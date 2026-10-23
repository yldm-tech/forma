import type { i18n as I18n } from "i18next";
import { beforeAll, describe, expect, test } from "vite-plus/test";
import { ApiError } from "../../../lib/api";
import { initI18n } from "../../../lib/i18n";
import {
  type PlanState,
  billingErrorMessage,
  currentCatalogItem,
  formatMoney,
  isSwitchAtPeriodEnd,
  planCta,
  planCtaState,
  planRequest,
  trialedPaidPlan,
} from "./plans";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

const state = (overrides: Partial<PlanState> = {}): PlanState => ({
  currentCloudPlan: "hobby",
  currentBillingInterval: null,
  isTrialing: false,
  hasPaymentMethod: false,
  pendingChange: null,
  ...overrides,
});

const hobbyWithoutCard = state();
const hobbyWithCard = state({ hasPaymentMethod: true });
const proMonthly = state({
  currentCloudPlan: "pro",
  currentBillingInterval: "monthly",
  hasPaymentMethod: true,
});
const trialWithoutCard = state({
  currentCloudPlan: "pro",
  currentBillingInterval: "monthly",
  isTrialing: true,
});
const trialWithCard = state({ ...trialWithoutCard, hasPaymentMethod: true });
const custom = state({
  currentCloudPlan: "custom",
  currentBillingInterval: "monthly",
  hasPaymentMethod: true,
});

describe("planCta", () => {
  test.each([
    ["Hobby without a card, Hobby", hobbyWithoutCard, "hobby", "monthly", "current_plan_cta"],
    ["Hobby without a card, Pro", hobbyWithoutCard, "pro", "monthly", "upgrade_now"],
    ["Hobby with a card, Scale yearly", hobbyWithCard, "scale", "yearly", "upgrade_now"],
    ["Pro monthly, its own card", proMonthly, "pro", "monthly", "cancel_at_period_end"],
    ["Pro monthly, Pro yearly", proMonthly, "pro", "yearly", "switch_at_period_end"],
    ["Pro monthly, Scale", proMonthly, "scale", "monthly", "upgrade_now"],
    ["Pro monthly, Hobby", proMonthly, "hobby", "monthly", "switch_at_period_end"],
    ["trial without a card, its plan", trialWithoutCard, "pro", "monthly", "unlock_all_plan_features"],
    ["trial without a card, Hobby", trialWithoutCard, "hobby", "monthly", "downgrade_to_hobby"],
    ["trial without a card, Scale", trialWithoutCard, "scale", "monthly", "upgrade_now"],
    ["trial with a card, its plan", trialWithCard, "pro", "monthly", "upgrade_now"],
    ["custom plan, Pro", custom, "pro", "monthly", "switch_plan_now"],
  ] as const)("%s", (_name, planState, plan, interval, cta) => {
    expect(planCta(planState, plan, interval)).toBe(cta);
  });

  test("a scheduled downgrade keeps the current plan current and marks its target scheduled", () => {
    const downgrading = state({
      ...proMonthly,
      pendingChange: {
        type: "plan_change",
        targetPlan: "hobby",
        targetInterval: null,
        effectiveAt: "2026-11-01T00:00:00.000Z",
      },
    });
    // Cancelling again is not offered while the drop to Hobby is already pending.
    expect(planCta(downgrading, "pro", "monthly")).toBe("current_plan_cta");
    expect(planCta(downgrading, "hobby", "monthly")).toBe("pending_plan_cta");
  });
});

describe("planCtaState", () => {
  const rights = { hasBillingRights: true, isStripeSetupIncomplete: false };

  test("the active plan's own button is inert, a trial's is not", () => {
    expect(planCtaState(hobbyWithoutCard, "hobby", "monthly", rights).isDisabled).toBe(true);
    expect(planCtaState(trialWithoutCard, "pro", "monthly", rights).isDisabled).toBe(false);
    // A paid plan's own button cancels it at period end.
    expect(planCtaState(proMonthly, "pro", "monthly", rights)).toMatchObject({
      isDisabled: false,
      isSecondary: true,
    });
  });

  test("every button is inert without billing rights or a Stripe customer", () => {
    expect(
      planCtaState(hobbyWithoutCard, "pro", "monthly", { ...rights, hasBillingRights: false }).isDisabled
    ).toBe(true);
    expect(
      planCtaState(hobbyWithoutCard, "pro", "monthly", { ...rights, isStripeSetupIncomplete: true })
        .isDisabled
    ).toBe(true);
  });
});

describe("isSwitchAtPeriodEnd", () => {
  test("downgrades with a card are scheduled, upgrades and card-less paid plans are not", () => {
    expect(isSwitchAtPeriodEnd(proMonthly, "hobby", "monthly")).toBe(true);
    expect(isSwitchAtPeriodEnd(proMonthly, "scale", "monthly")).toBe(false);
    expect(isSwitchAtPeriodEnd(hobbyWithoutCard, "pro", "monthly")).toBe(false);
    // A trial without a card returns to Hobby at once, after a confirmation.
    expect(isSwitchAtPeriodEnd(trialWithoutCard, "hobby", "monthly")).toBe(false);
    expect(isSwitchAtPeriodEnd(custom, "hobby", "monthly")).toBe(false);
  });
});

describe("planRequest", () => {
  test.each([
    [
      "Hobby from a trial is confirmed first",
      trialWithoutCard,
      "hobby",
      "monthly",
      { kind: "confirm-hobby" },
    ],
    ["Hobby from a paid plan is scheduled at once", proMonthly, "hobby", "monthly", { kind: "run" }],
    [
      "the trialed plan without a card offers paying now",
      trialWithoutCard,
      "pro",
      "monthly",
      { kind: "confirm", mode: "trial-continue" },
    ],
    [
      "the trialed plan with a card confirms the charge",
      trialWithCard,
      "pro",
      "monthly",
      { kind: "confirm", mode: "upgrade" },
    ],
    [
      "another plan during a card-less trial confirms the charge",
      trialWithoutCard,
      "scale",
      "monthly",
      { kind: "confirm", mode: "upgrade" },
    ],
    [
      "an upgrade with a card confirms the charge",
      proMonthly,
      "scale",
      "monthly",
      { kind: "confirm", mode: "upgrade" },
    ],
    ["a paid plan without a card goes to Checkout", hobbyWithoutCard, "pro", "monthly", { kind: "run" }],
    ["cancelling at period end needs no confirmation", proMonthly, "pro", "monthly", { kind: "run" }],
  ] as const)("%s", (_name, planState, plan, interval, request) => {
    expect(planRequest(planState, plan, interval)).toEqual(request);
  });
});

test("trialedPaidPlan is the paid plan of a trial only", () => {
  expect(trialedPaidPlan(trialWithoutCard)).toBe("pro");
  expect(trialedPaidPlan(proMonthly)).toBeNull();
});

test("currentCatalogItem has no item for a custom or unrecognised plan", () => {
  const item = (plan: "hobby" | "pro" | "scale", interval: "monthly" | "yearly") => ({
    plan,
    interval,
    currency: "usd",
    unitAmount: 0,
    responseOverage: null,
    workflowRunsIncluded: null,
    workflowRunsOverage: null,
  });
  const catalog = {
    hobby: { monthly: item("hobby", "monthly") },
    pro: { monthly: item("pro", "monthly"), yearly: item("pro", "yearly") },
    scale: { monthly: item("scale", "monthly"), yearly: item("scale", "yearly") },
  };
  expect(currentCatalogItem(catalog, "hobby", "yearly")).toBe(catalog.hobby.monthly);
  expect(currentCatalogItem(catalog, "scale", "yearly")).toBe(catalog.scale.yearly);
  expect(currentCatalogItem(catalog, "custom", "monthly")).toBeNull();
});

test("formatMoney shows whole amounts without decimals and nothing as a dash", () => {
  expect(formatMoney("usd", 2900, "en-US")).toBe("$29");
  expect(formatMoney("usd", 2950, "en-US")).toBe("$29.50");
  expect(formatMoney("eur", 0, "en-US")).toBe("€0");
  expect(formatMoney("usd", null, "en-US")).toBe("—");
});

test("billingErrorMessage explains the billing codes and hides anything else", () => {
  const t = i18n.t.bind(i18n);
  const error = (code: string) => new ApiError(403, code, code);
  expect(billingErrorMessage(t, error("mixed_interval_checkout_unsupported"))).toBe(
    t("workspace.settings.billing.yearly_checkout_unavailable")
  );
  expect(billingErrorMessage(t, error("payment_method_required"))).toBe(
    t("workspace.settings.billing.payment_method_required")
  );
  expect(billingErrorMessage(t, error("card_authentication_required"))).toBe(
    t("workspace.settings.billing.payment_authentication_failed")
  );
  expect(billingErrorMessage(t, new ApiError(500, "internal_server_error", "boom"))).toBe(
    t("common.something_went_wrong_please_try_again")
  );
});
