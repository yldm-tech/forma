import { Trans, useTranslation } from "react-i18next";
import { TooltipRenderer } from "../../../components/ui/tooltip";
import type { Overage } from "../../../lib/queries/billing";

const formatPerUnitPrice = (perUnitCents: number, currency: string, locale: string) =>
  new Intl.NumberFormat(locale, {
    style: "currency",
    currency: currency.toUpperCase(),
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(perUnitCents / 100);

/** The bands of a graduated metered price: from, to and the price of each unit in the band. */
export const PricingTierTable = ({ overage, locale }: { overage: Overage; locale: string }) => {
  const { t } = useTranslation();
  return (
    <table className="w-full text-left text-xs">
      <thead>
        <tr className="border-b border-slate-200 text-slate-500">
          <th className="pr-4 pb-2 font-medium">
            {t("workspace.settings.billing.dynamic_pricing_first_unit")}
          </th>
          <th className="pr-4 pb-2 font-medium">
            {t("workspace.settings.billing.dynamic_pricing_last_unit")}
          </th>
          <th className="pb-2 font-medium">{t("workspace.settings.billing.dynamic_pricing_per_unit")}</th>
        </tr>
      </thead>
      <tbody>
        {overage.tiers.map((tier) => (
          <tr key={`${tier.firstUnit}-${tier.lastUnit ?? "unlimited"}`} className="text-slate-700">
            <td className="py-1 pr-4 tabular-nums">{tier.firstUnit.toLocaleString(locale)}</td>
            <td className="py-1 pr-4 tabular-nums">
              {tier.lastUnit === null
                ? t("workspace.settings.billing.dynamic_pricing_unlimited")
                : tier.lastUnit.toLocaleString(locale)}
            </td>
            <td className="py-1 tabular-nums">
              {formatPerUnitPrice(tier.perUnitCents, overage.currency, locale)}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
};

// <Trans> puts the translated text of the <dynamicPricing> tag inside this element: a tooltip trigger when the price has bands to show, plain text otherwise.
const dynamicPricing = (overage: Overage | null, locale: string) =>
  overage ? (
    <TooltipRenderer
      tooltipContent={<PricingTierTable overage={overage} locale={locale} />}
      className="max-w-none p-3"
      triggerClass="cursor-help border-b border-dotted border-slate-400">
      <span />
    </TooltipRenderer>
  ) : (
    <span />
  );

/** forma2's PlanResponseFeature and PlanWorkflowRunsFeature: a metered allowance whose "dynamic pricing" shows the overage bands Stripe charges. */
export const MeteredFeature = ({
  feature,
  overage,
  locale,
}: {
  feature: "pro_responses" | "scale_responses" | "scale_workflow_runs";
  overage: Overage | null;
  locale: string;
}) => {
  const i18nKey = {
    pro_responses: "workspace.settings.billing.plan_pro_feature_responses",
    scale_responses: "workspace.settings.billing.plan_scale_feature_responses",
    scale_workflow_runs: "workspace.settings.billing.plan_scale_feature_workflow_runs",
  }[feature];
  return <Trans i18nKey={i18nKey} components={{ dynamicPricing: dynamicPricing(overage, locale) }} />;
};
