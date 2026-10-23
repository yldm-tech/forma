import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { CheckIcon } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "../../../components/ui/alert";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { ConfirmationModal } from "../../../components/ui/confirmation-modal";
import { SettingsCard } from "../../../components/ui/page";
import { TransLink } from "../../../components/ui/trans-link";
import { cn } from "../../../lib/cn";
import { organizationSettingsPath } from "../../../lib/navigation";
import {
  type BillingInterval,
  type BillingPage,
  type ChargePreview,
  type PaidPlan,
  type StandardPlan,
  billingApi,
  billingKeys,
} from "../../../lib/queries/billing";
import { toast } from "../../../lib/toast";
import {
  type PlanState,
  billingErrorMessage,
  canCancelAtPeriodEnd,
  ctaLabel,
  currentCatalogItem,
  formatBillingDate,
  formatMoney,
  periodLabel,
  planCta,
  planCtaState,
  planLabel,
  planRequest,
  trialedPaidPlan,
} from "../lib/plans";
import { confirmUpgradePayment } from "../lib/stripe";
import { MeteredFeature } from "./response-pricing-tooltip";
import { PlanComparisonTable } from "./plan-comparison";
import { TrialAlert } from "./trial-alert";
import { UsageCard } from "./usage-card";

/** The organization the confirmation page after Checkout goes back to, when Stripe's return URL does not carry it. */
export const BILLING_CONFIRMATION_ORGANIZATION_ID_KEY = "billingConfirmationOrganizationId";
const BILLING_PENDING_UPGRADE_PLAN_KEY = "billingPendingUpgradePlan";
const BILLING_PENDING_UPGRADE_INTERVAL_KEY = "billingPendingUpgradeInterval";

// The plan reaches the database through Stripe's webhook a moment after the browser returns: the page polls a forced sync until it shows, for a bounded time.
const UPGRADE_SYNC_POLL_INTERVAL_MS = 1500;
const UPGRADE_SYNC_POLL_TIMEOUT_MS = 45_000;

const CONTACT_SALES_URL =
  "https://app.forma.ylam.ai/s/trvp8tzy5uvsps9rc9qi9l9w?delivery=cloud&source=billingView&type=pricingRequest";
const SUPPORT_EMAIL = "hola@forma.ylam.ai";

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/** The query string Stripe's success URLs carry back to the billing page. */
export interface BillingSearch {
  checkout_success?: string;
  upgrade_pending?: string;
  session_id?: string;
}

type PlanFeature =
  | { type: "text"; label: string }
  | { type: "metered"; feature: "pro_responses" | "scale_responses" | "scale_workflow_runs" };

interface PlanCard {
  plan: StandardPlan;
  interval: BillingInterval;
  amount: string;
  description: string;
  features: PlanFeature[];
}

interface UpgradeConfirmation {
  plan: PaidPlan;
  interval: BillingInterval;
  mode: "upgrade" | "trial-continue";
}

/**
 * forma2's PricingTable: the subscription with its usage, the plans to choose from, and the flows behind each choice. A card on file makes an upgrade immediate (behind a confirmation of the charge, with 3D Secure confirmed here through Stripe.js) and a downgrade scheduled; without one, a paid plan goes through Stripe Checkout, which returns here to finish.
 */
export const PricingTable = ({
  organizationId,
  page,
  search,
}: {
  organizationId: string;
  page: BillingPage;
  search: BillingSearch;
}) => {
  const { t, i18n } = useTranslation();
  const locale = i18n.resolvedLanguage ?? i18n.language;
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const checkoutReturnHandled = useRef(false);
  // Bumped by every confirmation opened or closed, so a preview that resolves late never lands in the wrong modal.
  const previewRequest = useRef(0);
  const [isRetryingSetup, setIsRetryingSetup] = useState(false);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [upgradeConfirmation, setUpgradeConfirmation] = useState<UpgradeConfirmation | null>(null);
  const [upgradePreview, setUpgradePreview] = useState<ChargePreview | null>(null);
  const [isLoadingPreview, setIsLoadingPreview] = useState(false);
  const [isHobbyConfirmOpen, setIsHobbyConfirmOpen] = useState(false);
  const [selectedInterval, setSelectedInterval] = useState<BillingInterval>(
    page.currentBillingInterval ?? "monthly"
  );

  const { billing, billingCatalog: catalog, currentCloudPlan, currentBillingInterval, pendingChange } = page;
  const billingPath = organizationSettingsPath(organizationId, "billing");
  const state: PlanState = {
    currentCloudPlan,
    currentBillingInterval,
    isTrialing: page.currentSubscriptionStatus === "trialing",
    hasPaymentMethod: billing.stripe?.hasPaymentMethod === true,
    pendingChange,
  };
  const existingSubscriptionId = billing.stripe?.subscriptionId ?? null;
  const canManageSubscription = page.hasBillingRights && !!billing.stripeCustomerId;
  const trialPlan = trialedPaidPlan(state);
  // The workflow-runs card shows the free tier of the plan's price, exactly what Stripe leaves uncharged, and hides when the plan has no workflow price.
  const workflowRunsLimit =
    currentCatalogItem(catalog, currentCloudPlan, currentBillingInterval ?? "monthly")
      ?.workflowRunsIncluded ?? null;
  const trialEnd = billing.stripe?.trialEnd ? new Date(billing.stripe.trialEnd) : null;
  const trialEndLabel =
    trialEnd && Number.isFinite(trialEnd.getTime()) ? formatBillingDate(trialEnd, locale) : null;
  const paymentError = billing.stripe?.paymentAttemptError ?? null;

  const refresh = () => queryClient.invalidateQueries({ queryKey: billingKeys.all(organizationId) });
  const leaveCheckoutReturn = () => void navigate({ href: billingPath, replace: true });
  const goTo = (url: string) => globalThis.location.assign(url);

  const persistOrganizationId = () =>
    sessionStorage.setItem(BILLING_CONFIRMATION_ORGANIZATION_ID_KEY, organizationId);
  const persistUpgradeIntent = (plan: PaidPlan, interval: BillingInterval) => {
    sessionStorage.setItem(BILLING_PENDING_UPGRADE_PLAN_KEY, plan);
    sessionStorage.setItem(BILLING_PENDING_UPGRADE_INTERVAL_KEY, interval);
  };
  const clearUpgradeIntent = () => {
    sessionStorage.removeItem(BILLING_PENDING_UPGRADE_PLAN_KEY);
    sessionStorage.removeItem(BILLING_PENDING_UPGRADE_INTERVAL_KEY);
  };

  /** Completes any 3D Secure the change needs; whether the change went through. A payment abandoned or declined is recorded at once, since Stripe's own event arrives late. */
  const settlePayment = async (result: {
    requiresAction: boolean;
    clientSecret: string | null;
  }): Promise<{ applied: boolean; message: string | null }> => {
    if (!result.requiresAction || !result.clientSecret) return { applied: true, message: null };
    const outcome = await confirmUpgradePayment(page.stripePublishableKey, result.clientSecret);
    if (outcome.status === "succeeded" || outcome.status === "processing")
      return { applied: true, message: null };
    if (outcome.paymentIntentId) await billingApi.reportPaymentIssue(organizationId, outcome.paymentIntentId);
    return { applied: false, message: t("workspace.settings.billing.payment_authentication_failed") };
  };

  // Stripe Checkout returns here. A card-only setup checkout waits until the webhook has attached the card; a setup checkout for an upgrade applies the upgrade on-session and waits until the plan shows.
  useEffect(() => {
    if (search.checkout_success !== "1" || checkoutReturnHandled.current) return;
    checkoutReturnHandled.current = true;

    if (search.upgrade_pending !== "1") {
      const toastId = toast.loading(t("common.saving"));
      void billingApi
        .waitForPaymentMethod(organizationId)
        .catch(() => false)
        .finally(() => {
          toast.dismiss(toastId);
          leaveCheckoutReturn();
          void refresh();
        });
      return;
    }

    const checkoutSessionId = search.session_id;
    if (!checkoutSessionId) return;
    const pendingPlan = sessionStorage.getItem(BILLING_PENDING_UPGRADE_PLAN_KEY) as PaidPlan | null;
    const toastId = toast.loading(t("workspace.settings.billing.upgrade_checkout_pending"));

    // Ends without the plan in place: the webhook will still apply it, and the page shows it once it has.
    const settleWithError = (message: string) => {
      clearUpgradeIntent();
      toast.error(message, { id: toastId });
      leaveCheckoutReturn();
      void refresh();
    };
    const finish = (plan: PaidPlan | null) => {
      clearUpgradeIntent();
      toast.success(
        t("workspace.settings.billing.upgrade_checkout_success", { plan: planLabel(t, plan ?? "pro") }),
        { id: toastId }
      );
      leaveCheckoutReturn();
      void refresh();
    };
    const pollUntilPlan = async (plan: PaidPlan): Promise<boolean> => {
      const deadline = Date.now() + UPGRADE_SYNC_POLL_TIMEOUT_MS;
      for (;;) {
        const synced = await billingApi.waitForPlan(organizationId, plan).catch(() => null);
        if (synced === plan) return true;
        if (Date.now() >= deadline) return false;
        await sleep(UPGRADE_SYNC_POLL_INTERVAL_MS);
      }
    };

    void (async () => {
      let result;
      try {
        result = await billingApi.finalizeSetupCheckout(organizationId, checkoutSessionId);
      } catch (error) {
        settleWithError(billingErrorMessage(t, error));
        return;
      }
      const settled = await settlePayment(result).catch(() => ({ applied: false, message: null }));
      if (!settled.applied) {
        settleWithError(settled.message ?? t("common.something_went_wrong_please_try_again"));
        return;
      }
      const plan = result.targetPlan ?? pendingPlan;
      if (!plan || (await pollUntilPlan(plan))) finish(plan);
      else settleWithError(t("workspace.settings.billing.upgrade_checkout_pending"));
    })();
    // oxlint-disable-next-line react/exhaustive-deps -- runs once per return from Checkout; the ref keeps a re-render from starting it again
  }, [search.checkout_success, search.upgrade_pending, search.session_id]);

  const planCards: PlanCard[] = [
    {
      plan: "hobby",
      interval: "monthly",
      amount: formatMoney(catalog.hobby.monthly.currency, catalog.hobby.monthly.unitAmount, locale),
      description: t("workspace.settings.billing.plan_hobby_description"),
      features: [
        t("workspace.settings.billing.plan_hobby_feature_responses"),
        t("workspace.settings.billing.plan_hobby_feature_workspaces"),
        t("workspace.settings.billing.plan_hobby_feature_surveys"),
        t("workspace.settings.billing.plan_hobby_feature_question_types"),
        t("workspace.settings.billing.plan_hobby_feature_logic"),
        t("workspace.settings.billing.plan_hobby_feature_partial"),
        t("workspace.settings.billing.plan_hobby_feature_file_uploads"),
        t("workspace.settings.billing.plan_hobby_feature_api"),
      ].map((label) => ({ type: "text", label })),
    },
    {
      plan: "pro",
      interval: selectedInterval,
      amount: formatMoney(
        catalog.pro[selectedInterval].currency,
        catalog.pro[selectedInterval].unitAmount,
        locale
      ),
      description: t("workspace.settings.billing.plan_pro_description"),
      features: [
        { type: "text", label: t("workspace.settings.billing.plan_feature_everything_in_hobby") },
        { type: "text", label: t("workspace.settings.billing.plan_pro_feature_smart_tools") },
        { type: "metered", feature: "pro_responses" },
        { type: "text", label: t("workspace.settings.billing.plan_pro_feature_workspaces") },
        { type: "text", label: t("workspace.settings.billing.plan_pro_feature_unlimited_seats") },
        { type: "text", label: t("workspace.settings.billing.plan_pro_feature_hide_branding") },
        { type: "text", label: t("workspace.settings.billing.plan_pro_feature_contacts") },
        { type: "text", label: t("workspace.settings.billing.plan_pro_feature_integrations") },
        { type: "text", label: t("workspace.settings.billing.plan_pro_feature_sdks") },
        { type: "text", label: t("workspace.settings.billing.plan_pro_feature_ai_translations") },
      ],
    },
    {
      plan: "scale",
      interval: selectedInterval,
      amount: formatMoney(
        catalog.scale[selectedInterval].currency,
        catalog.scale[selectedInterval].unitAmount,
        locale
      ),
      description: t("workspace.settings.billing.plan_scale_description"),
      features: [
        { type: "text", label: t("workspace.settings.billing.plan_feature_everything_in_pro") },
        { type: "metered", feature: "scale_responses" },
        { type: "text", label: t("workspace.settings.billing.plan_scale_feature_workspaces") },
        { type: "text", label: t("workspace.settings.billing.plan_scale_feature_rbac") },
        { type: "text", label: t("workspace.settings.billing.plan_scale_feature_quota") },
        { type: "metered", feature: "scale_workflow_runs" },
        { type: "text", label: t("workspace.settings.billing.plan_scale_feature_feedback") },
        { type: "text", label: t("workspace.settings.billing.plan_scale_feature_semantic_analysis") },
        { type: "text", label: t("workspace.settings.billing.plan_scale_feature_security") },
      ],
    },
  ];

  const meteredOverage = (feature: "pro_responses" | "scale_responses" | "scale_workflow_runs") => {
    if (feature === "pro_responses") return catalog.pro[selectedInterval].responseOverage;
    if (feature === "scale_responses") return catalog.scale[selectedInterval].responseOverage;
    return catalog.scale[selectedInterval].workflowRunsOverage;
  };

  /** Stripe Checkout in setup mode for the existing subscription, carrying the upgrade to apply when it returns. */
  const openUpgradeCheckout = async (plan: PaidPlan, interval: BillingInterval) => {
    persistOrganizationId();
    persistUpgradeIntent(plan, interval);
    try {
      goTo(await billingApi.setupCheckout(organizationId, { targetPlan: plan, targetInterval: interval }));
    } catch (error) {
      clearUpgradeIntent();
      toast.error(billingErrorMessage(t, error));
    }
  };

  /** A paid plan without a card on file: a setup checkout for an existing subscription, a subscription checkout otherwise (monthly only, as Checkout cannot mix a yearly base with monthly metered prices). */
  const redirectToPlanCheckout = async (plan: PaidPlan, interval: BillingInterval) => {
    if (existingSubscriptionId) {
      await openUpgradeCheckout(plan, interval);
      return;
    }
    if (interval === "yearly") {
      toast.error(t("workspace.settings.billing.yearly_checkout_unavailable"));
      return;
    }
    persistOrganizationId();
    try {
      goTo(await billingApi.checkout(organizationId, plan, interval));
    } catch (error) {
      toast.error(billingErrorMessage(t, error));
    }
  };

  const planChangeMessage = (mode: "immediate" | "scheduled") =>
    mode === "scheduled"
      ? t("workspace.settings.billing.plan_change_scheduled")
      : t("workspace.settings.billing.plan_change_applied");

  const handlePlanAction = async (plan: StandardPlan, interval: BillingInterval) => {
    setPendingAction(`${plan}-${interval}`);
    try {
      if (!state.hasPaymentMethod && plan !== "hobby") {
        await redirectToPlanCheckout(plan, interval);
        return;
      }
      if (canCancelAtPeriodEnd(state, plan, interval)) {
        const change = await billingApi.changePlan(organizationId, "hobby", "monthly");
        toast.success(planChangeMessage(change.mode));
        await refresh();
        return;
      }
      const change = await billingApi.changePlan(organizationId, plan, interval);
      const settled = await settlePayment(change);
      if (!settled.applied) {
        if (settled.message) toast.error(settled.message);
        await refresh();
        return;
      }
      if (change.mode === "immediate") {
        // Waits for the forced sync to show the new plan, so the page does not keep showing the old one.
        await billingApi.waitForPlan(organizationId, plan);
        toast.success(t("workspace.settings.billing.upgrade_checkout_success", { plan: planLabel(t, plan) }));
        await refresh();
        return;
      }
      toast.success(planChangeMessage(change.mode));
      await refresh();
    } catch (error) {
      toast.error(billingErrorMessage(t, error));
    } finally {
      setPendingAction(null);
    }
  };

  const openConfirmation = (plan: PaidPlan, interval: BillingInterval, mode: UpgradeConfirmation["mode"]) => {
    setUpgradeConfirmation({ plan, interval, mode });
    setUpgradePreview(null);
    setIsLoadingPreview(true);
    const request = ++previewRequest.current;
    void billingApi
      .previewUpgrade(organizationId, plan, interval)
      .catch(() => null)
      .then((preview) => {
        if (previewRequest.current !== request) return;
        setUpgradePreview(preview);
        setIsLoadingPreview(false);
      });
  };

  const closeConfirmation = () => {
    previewRequest.current += 1;
    setUpgradeConfirmation(null);
    setUpgradePreview(null);
    setIsLoadingPreview(false);
  };

  /** A plan card's click: a confirmation first wherever it ends a trial or charges now. */
  const requestPlanAction = (plan: StandardPlan, interval: BillingInterval) => {
    const request = planRequest(state, plan, interval);
    if (request.kind === "confirm-hobby") setIsHobbyConfirmOpen(true);
    else if (request.kind === "confirm" && plan !== "hobby") openConfirmation(plan, interval, request.mode);
    else void handlePlanAction(plan, interval);
  };

  /** The trial-continue modal's payment: with a card the trial converts at once, without one a setup checkout adds it and converts on return. */
  const handleTrialPayNow = (plan: PaidPlan, interval: BillingInterval) => {
    if (state.hasPaymentMethod) {
      void handlePlanAction(plan, interval);
      return;
    }
    setPendingAction(`${plan}-${interval}`);
    void redirectToPlanCheckout(plan, interval).finally(() => setPendingAction(null));
  };

  const openPortal = async () => {
    try {
      goTo(await billingApi.portal(organizationId));
    } catch (error) {
      toast.error(billingErrorMessage(t, error));
    }
  };

  const retrySetup = async () => {
    setIsRetryingSetup(true);
    try {
      await billingApi.retrySetup(organizationId);
      await refresh();
    } catch (error) {
      toast.error(billingErrorMessage(t, error));
    } finally {
      setIsRetryingSetup(false);
    }
  };

  const undoPendingChange = async () => {
    setPendingAction("undo");
    try {
      await billingApi.undoPendingChange(organizationId);
      toast.success(t("workspace.settings.billing.pending_change_removed"));
      await refresh();
    } catch (error) {
      toast.error(billingErrorMessage(t, error));
    } finally {
      setPendingAction(null);
    }
  };

  const catalogAmount = (plan: PaidPlan, interval: BillingInterval) =>
    formatMoney(catalog[plan][interval].currency, catalog[plan][interval].unitAmount, locale);

  // Deliberately period-neutral ("charged X now"): the same modal covers a mid-cycle proration and a trial conversion's full period.
  const upgradeConfirmationBody = (confirmation: UpgradeConfirmation): ReactNode => {
    if (isLoadingPreview) return t("workspace.settings.billing.confirm_upgrade_calculating");
    const plan = planLabel(t, confirmation.plan);
    const period = periodLabel(t, confirmation.plan, confirmation.interval);
    if (upgradePreview)
      return t("workspace.settings.billing.confirm_upgrade_body_with_charge", {
        plan,
        period,
        chargeNow: formatMoney(upgradePreview.currency, upgradePreview.amountDue, locale),
      });
    return t("workspace.settings.billing.confirm_upgrade_body", {
      plan,
      amount: catalogAmount(confirmation.plan, confirmation.interval),
      period,
    });
  };

  const trialContinueBody = (confirmation: UpgradeConfirmation): ReactNode => {
    if (isLoadingPreview) return t("workspace.settings.billing.confirm_upgrade_calculating");
    const values = {
      plan: planLabel(t, confirmation.plan),
      period: periodLabel(t, confirmation.plan, confirmation.interval),
      fullPrice: catalogAmount(confirmation.plan, confirmation.interval),
    };
    return upgradePreview ? (
      <Trans
        i18nKey="workspace.settings.billing.confirm_trial_continue_body"
        values={{
          ...values,
          chargeNow: formatMoney(upgradePreview.currency, upgradePreview.amountDue, locale),
        }}
        components={{ b: <b />, br: <br /> }}
      />
    ) : (
      <Trans
        i18nKey="workspace.settings.billing.confirm_trial_continue_body_fallback"
        values={values}
        components={{ b: <b />, br: <br /> }}
      />
    );
  };

  const trialContinuePayNowLabel =
    upgradePreview && !isLoadingPreview
      ? t("workspace.settings.billing.confirm_trial_continue_pay_now", {
          chargeNow: formatMoney(upgradePreview.currency, upgradePreview.amountDue, locale),
        })
      : t("workspace.settings.billing.confirm_trial_continue_pay_now_generic");

  const cardState = (card: PlanCard) =>
    planCtaState(state, card.plan, card.interval, {
      hasBillingRights: page.hasBillingRights,
      isStripeSetupIncomplete: page.isStripeSetupIncomplete,
    });

  const cardCta = (card: PlanCard) => ctaLabel(t, planCta(state, card.plan, card.interval), card.plan);

  const planCardGrid = (
    <div className="grid gap-4 lg:grid-cols-3">
      {planCards.map((card) => {
        const { isCurrent, isPending, isSecondary, isDisabled } = cardState(card);
        const name = planLabel(t, card.plan);
        return (
          <section
            key={`${card.plan}-${card.interval}`}
            aria-label={name}
            className={cn(
              "grid h-full grid-rows-[minmax(1.75rem,auto)_minmax(8rem,auto)_minmax(4.5rem,auto)_auto_1fr] rounded-2xl border bg-white p-6 shadow-xs",
              card.plan === "pro" ? "border-slate-900/20" : "border-slate-200"
            )}>
            <div className="mb-4 flex min-h-7 items-start gap-2">
              {card.plan === "pro" && (
                <span className="rounded-md bg-slate-100 px-2 py-1 text-xs font-medium text-slate-600">
                  {t("workspace.settings.billing.most_popular")}
                </span>
              )}
              {isCurrent && (
                <span className="rounded-md bg-emerald-100 px-2 py-1 text-xs font-medium text-emerald-700">
                  {t("workspace.settings.billing.current_plan_badge")}
                </span>
              )}
              {isPending && (
                <span className="rounded-md bg-amber-100 px-2 py-1 text-xs font-medium text-amber-700">
                  {t("workspace.settings.billing.pending_plan_badge")}
                </span>
              )}
            </div>
            <div className="min-h-32">
              <h3 className="text-3xl font-semibold text-slate-900">{name}</h3>
              <p className="mt-3 text-sm leading-6 text-slate-500">{card.description}</p>
            </div>
            <div className="mt-4 flex min-h-12 items-end gap-2">
              <span className="text-3xl font-normal tracking-tight text-slate-900">{card.amount}</span>
              <span className="pb-1 text-sm text-slate-500">{periodLabel(t, card.plan, card.interval)}</span>
            </div>
            <Button
              variant={isSecondary || card.plan !== "pro" ? "secondary" : "default"}
              className="mt-4 w-full"
              disabled={isDisabled}
              loading={pendingAction === `${card.plan}-${card.interval}`}
              onClick={() => requestPlanAction(card.plan, card.interval)}>
              {cardCta(card)}
            </Button>
            <div className="mt-8 border-t border-slate-100 pt-6">
              <p className="mb-4 text-sm font-semibold text-slate-900">
                {t("workspace.settings.billing.this_includes")}
              </p>
              <ul className="space-y-3">
                {card.features.map((feature) => (
                  <li
                    key={feature.type === "text" ? feature.label : feature.feature}
                    className="flex items-start gap-3 text-sm text-slate-700">
                    <CheckIcon className="mt-0.5 size-4 shrink-0 text-slate-500" />
                    <span>
                      {feature.type === "text" ? (
                        feature.label
                      ) : (
                        <MeteredFeature
                          feature={feature.feature}
                          overage={meteredOverage(feature.feature)}
                          locale={locale}
                        />
                      )}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          </section>
        );
      })}
    </div>
  );

  return (
    <div className="flex max-w-6xl flex-col gap-4">
      {page.trialDaysRemaining !== null && (
        // One banner for every trial: its action opens the same confirmation as the trialed plan's card.
        <TrialAlert trialDaysRemaining={page.trialDaysRemaining} className="max-w-5xl">
          <AlertDescription>{t("workspace.settings.billing.trial_alert_description")}</AlertDescription>
          {page.hasBillingRights && trialPlan && (
            <AlertButton onClick={() => requestPlanAction(trialPlan, currentBillingInterval ?? "monthly")}>
              {t("workspace.settings.billing.unlock_all_plan_features", { plan: planLabel(t, trialPlan) })}
            </AlertButton>
          )}
        </TrialAlert>
      )}

      {paymentError && (
        <Alert variant="error" className="max-w-5xl">
          <AlertTitle>
            {paymentError.type === "requires_action"
              ? t("workspace.settings.billing.payment_error_requires_action")
              : t("workspace.settings.billing.payment_error_failed_invoice")}
          </AlertTitle>
          <AlertDescription>
            <Trans
              i18nKey="workspace.settings.billing.payment_error_contact_support"
              components={{
                supportLink: <TransLink href={`mailto:${SUPPORT_EMAIL}`} className="font-medium underline" />,
              }}
            />
          </AlertDescription>
        </Alert>
      )}

      {pendingChange && (
        <Alert variant="info" className="max-w-5xl" role="status">
          <AlertTitle>{t("workspace.settings.billing.pending_plan_change_title")}</AlertTitle>
          <AlertDescription>
            {t("workspace.settings.billing.pending_plan_change_description", {
              plan: planLabel(t, pendingChange.targetPlan),
              date: formatBillingDate(pendingChange.effectiveAt, locale),
            })}
          </AlertDescription>
          {page.hasBillingRights && (
            <AlertButton onClick={() => void undoPendingChange()} loading={pendingAction === "undo"}>
              {t("workspace.settings.billing.keep_current_plan")}
            </AlertButton>
          )}
        </Alert>
      )}

      {page.isStripeSetupIncomplete && page.hasBillingRights && (
        <Alert variant="warning" className="max-w-5xl" role="status">
          <AlertTitle>{t("workspace.settings.billing.stripe_setup_incomplete")}</AlertTitle>
          <AlertDescription>
            {t("workspace.settings.billing.stripe_setup_incomplete_description")}
          </AlertDescription>
          <AlertButton onClick={() => void retrySetup()} loading={isRetryingSetup}>
            {t("workspace.settings.billing.retry_setup")}
          </AlertButton>
        </Alert>
      )}

      {currentCloudPlan === "custom" && (
        <Alert className="max-w-5xl" role="status">
          <AlertTitle>{t("workspace.settings.billing.custom_plan_title")}</AlertTitle>
          <AlertDescription>{t("workspace.settings.billing.custom_plan_description")}</AlertDescription>
        </Alert>
      )}

      <SettingsCard
        width="full"
        className="my-0 max-w-5xl"
        title={t("workspace.settings.billing.subscription")}
        description={t("workspace.settings.billing.subscription_description")}
        cta={
          canManageSubscription ? (
            <Button variant="secondary" onClick={() => void openPortal()}>
              {t("workspace.settings.billing.manage_billing_details")}
            </Button>
          ) : undefined
        }>
        <div className="flex flex-col gap-6 pb-2">
          <div className="flex flex-col gap-1">
            <p className="text-sm font-semibold text-slate-700">
              {t("workspace.settings.billing.your_plan")}
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <Badge type="success" size="normal" text={planLabel(t, currentCloudPlan)} />
              {currentCloudPlan !== "hobby" && currentBillingInterval && (
                <Badge
                  type="gray"
                  size="normal"
                  text={
                    currentBillingInterval === "monthly"
                      ? t("workspace.settings.billing.monthly")
                      : t("workspace.settings.billing.yearly")
                  }
                />
              )}
              {state.isTrialing && (
                <Badge type="warning" size="normal" text={t("workspace.settings.billing.status_trialing")} />
              )}
            </div>
            {state.isTrialing && trialEndLabel && (
              <p className="mt-1 text-sm text-slate-500">
                {t("workspace.settings.billing.trial_cancels_automatically", { date: trialEndLabel })}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <UsageCard
              metric={t("common.responses")}
              currentCount={page.responseCount}
              limit={billing.limits.monthly.responses}
              unlimitedLabel={t("workspace.settings.billing.unlimited_responses")}
            />
            <p className="text-sm text-slate-500">
              {t("workspace.settings.billing.usage_cycle")}: {formatBillingDate(page.usageCycleStart, locale)}{" "}
              - {formatBillingDate(page.usageCycleEnd, locale)}
            </p>
          </div>

          {workflowRunsLimit !== null && (
            <UsageCard
              metric={t("common.workflow_runs")}
              currentCount={page.workflowRunCount}
              limit={workflowRunsLimit}
              unlimitedLabel={t("workspace.settings.billing.unlimited_workflow_runs")}
            />
          )}

          <UsageCard
            metric={t("common.workspaces")}
            currentCount={page.workspaceCount}
            limit={billing.limits.workspaces}
            unlimitedLabel={t("workspace.settings.billing.unlimited_workspaces")}
          />
        </div>
      </SettingsCard>

      {!page.isStripeSetupIncomplete && (
        <SettingsCard
          width="full"
          className="my-0 max-w-5xl"
          title={t("workspace.settings.billing.plan_selection_title")}
          description={t("workspace.settings.billing.plan_selection_description")}>
          <div className="flex flex-col gap-6 pb-2">
            <div
              className={cn(
                "flex w-fit rounded-xl border border-slate-200 bg-slate-100 p-1",
                page.isPlanComparison && "self-end"
              )}
              role="tablist"
              aria-label={t("workspace.settings.billing.billing_interval_toggle")}>
              {(["monthly", "yearly"] as const).map((interval) => (
                <button
                  key={interval}
                  type="button"
                  role="tab"
                  aria-selected={selectedInterval === interval}
                  tabIndex={selectedInterval === interval ? 0 : -1}
                  onClick={() => setSelectedInterval(interval)}
                  className={cn(
                    "rounded-lg px-5 py-2 text-sm font-medium transition-colors",
                    selectedInterval === interval
                      ? "bg-slate-900 text-white"
                      : "text-slate-600 hover:text-slate-900"
                  )}>
                  {interval === "monthly"
                    ? t("workspace.settings.billing.monthly")
                    : t("workspace.settings.billing.yearly")}
                </button>
              ))}
            </div>

            {page.isPlanComparison ? (
              <>
                <div className="lg:hidden">{planCardGrid}</div>
                <div className="hidden lg:block">
                  <PlanComparisonTable
                    columns={planCards.map((card) => {
                      const { isCurrent, isPending, isSecondary, isDisabled } = cardState(card);
                      return {
                        key: `${card.plan}-${card.interval}`,
                        name: planLabel(t, card.plan),
                        description: card.description,
                        amount: card.amount,
                        periodLabel: periodLabel(t, card.plan, card.interval),
                        isPopular: card.plan === "pro",
                        isCurrent,
                        isPending,
                        ctaLabel: cardCta(card),
                        ctaVariant: isSecondary || card.plan !== "pro" ? "secondary" : "default",
                        ctaDisabled: isDisabled,
                        ctaLoading: pendingAction === `${card.plan}-${card.interval}`,
                        onCtaClick: () => requestPlanAction(card.plan, card.interval),
                      };
                    })}
                  />
                </div>
              </>
            ) : (
              planCardGrid
            )}

            <div className="mt-4 flex flex-col items-start justify-between gap-4 rounded-2xl border border-slate-200 bg-white p-6 shadow-xs sm:flex-row sm:items-center">
              <div>
                <h3 className="text-lg font-semibold text-slate-900">
                  {t("workspace.settings.billing.contact_sales_title")}
                </h3>
                <p className="mt-1 text-sm leading-6 text-slate-500">
                  {t("workspace.settings.billing.contact_sales_description")}
                </p>
              </div>
              <Button variant="default" className="shrink-0" asChild>
                <a href={CONTACT_SALES_URL} target="_blank" rel="noopener noreferrer">
                  {t("workspace.settings.billing.contact_sales_cta")}
                </a>
              </Button>
            </div>
          </div>
        </SettingsCard>
      )}

      {upgradeConfirmation?.mode === "trial-continue" && (
        <ConfirmationModal
          open
          setOpen={(open) => {
            if (!open) closeConfirmation();
          }}
          title={t("workspace.settings.billing.confirm_trial_continue_title", {
            plan: planLabel(t, upgradeConfirmation.plan),
          })}
          description={t("workspace.settings.billing.confirm_trial_continue_description")}
          body={trialContinueBody(upgradeConfirmation)}
          buttonText={trialContinuePayNowLabel}
          buttonVariant="default"
          buttonLoading={isLoadingPreview}
          isButtonDisabled={isLoadingPreview}
          cancelButtonText={t("common.cancel")}
          onConfirm={() => {
            const { plan, interval } = upgradeConfirmation;
            closeConfirmation();
            handleTrialPayNow(plan, interval);
          }}
        />
      )}

      {upgradeConfirmation?.mode === "upgrade" && (
        <ConfirmationModal
          open
          setOpen={(open) => {
            if (!open) closeConfirmation();
          }}
          title={t("workspace.settings.billing.confirm_upgrade_title")}
          description={t("workspace.settings.billing.confirm_upgrade_description")}
          body={upgradeConfirmationBody(upgradeConfirmation)}
          buttonText={t("workspace.settings.billing.confirm_upgrade_button")}
          buttonVariant="default"
          buttonLoading={isLoadingPreview}
          isButtonDisabled={isLoadingPreview}
          cancelButtonText={t("common.cancel")}
          onConfirm={() => {
            const { plan, interval } = upgradeConfirmation;
            closeConfirmation();
            void handlePlanAction(plan, interval);
          }}
        />
      )}

      {isHobbyConfirmOpen && (
        <ConfirmationModal
          open
          setOpen={setIsHobbyConfirmOpen}
          title={t("workspace.settings.billing.confirm_hobby_downgrade_title")}
          description={t("workspace.settings.billing.confirm_hobby_downgrade_description")}
          body={t("workspace.settings.billing.confirm_hobby_downgrade_body", {
            plan: planLabel(t, currentCloudPlan),
          })}
          buttonText={t("workspace.settings.billing.downgrade_to_hobby")}
          buttonVariant="destructive"
          cancelButtonText={t("common.cancel")}
          onConfirm={() => {
            setIsHobbyConfirmOpen(false);
            void handlePlanAction("hobby", "monthly");
          }}
        />
      )}
    </div>
  );
};
