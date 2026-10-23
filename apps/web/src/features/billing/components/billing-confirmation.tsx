import { Link } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { Confetti } from "../../../components/ui/confetti";
import { PageContentWrapper } from "../../../components/ui/page";
import { organizationSettingsPath } from "../../../lib/navigation";
import { billingApi } from "../../../lib/queries/billing";
import { BILLING_CONFIRMATION_ORGANIZATION_ID_KEY } from "./pricing-table";

const isSyncablePlan = (plan: string | undefined): plan is "pro" | "scale" =>
  plan === "pro" || plan === "scale";

/**
 * forma2's billing ConfirmationPage, where Stripe Checkout returns after a paid subscription starts. The organization comes from the return URL, or from this tab's session when the URL has none. The way back to billing waits until a forced sync shows the purchased plan, so the billing page opens on it.
 */
export const BillingConfirmation = ({ organizationId, plan }: { organizationId?: string; plan?: string }) => {
  const { t } = useTranslation();
  const [resolvedOrganizationId] = useState(
    () => organizationId ?? sessionStorage.getItem(BILLING_CONFIRMATION_ORGANIZATION_ID_KEY)
  );
  const [isSyncing, setIsSyncing] = useState(() => !!resolvedOrganizationId && isSyncablePlan(plan));

  useEffect(() => {
    if (!resolvedOrganizationId || !isSyncablePlan(plan)) return;
    let cancelled = false;
    void billingApi
      .waitForPlan(resolvedOrganizationId, plan)
      .catch(() => null)
      .finally(() => {
        if (!cancelled) setIsSyncing(false);
      });
    return () => {
      cancelled = true;
    };
  }, [resolvedOrganizationId, plan]);

  const billingHref = resolvedOrganizationId
    ? organizationSettingsPath(resolvedOrganizationId, "billing")
    : "/";

  return (
    <PageContentWrapper>
      <Confetti />
      <div className="mx-auto max-w-sm py-8 sm:px-6 lg:px-8">
        <div className="my-6 sm:flex-auto">
          <h1 className="text-center text-xl font-semibold text-slate-900">
            {t("billing_confirmation.upgrade_successful")}
          </h1>
          <p className="mt-2 text-center text-sm text-slate-700">
            {t("billing_confirmation.thanks_for_upgrading")}
          </p>
        </div>
        {isSyncing ? (
          <Button loading disabled className="w-full justify-center">
            {t("billing_confirmation.back_to_billing_overview")}
          </Button>
        ) : (
          <Button asChild className="w-full justify-center">
            <Link to={billingHref}>{t("billing_confirmation.back_to_billing_overview")}</Link>
          </Button>
        )}
      </div>
    </PageContentWrapper>
  );
};
