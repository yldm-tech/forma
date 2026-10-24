import { CheckIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { cn } from "../../../lib/cn";

/** One plan's column: its header (badges, price, call to action) above the feature matrix. */
export interface PlanColumn {
  key: string;
  name: string;
  description: string;
  amount: string;
  periodLabel: string;
  isPopular: boolean;
  isCurrent: boolean;
  isPending: boolean;
  ctaLabel: string;
  ctaVariant: "default" | "secondary";
  ctaDisabled: boolean;
  ctaLoading: boolean;
  onCtaClick: () => void;
}

// A cell: true is included, false is not, "addon" is a paid add-on, and any other text is shown as is.
type ComparisonValue = boolean | string;

type ComparisonRow =
  | { type: "feature"; label: string; values: ComparisonValue[] }
  | { type: "section"; label: string };

const PLAN_CELL = "border-l border-slate-100 px-3 sm:px-5";
const LABEL_CELL = "px-3 sm:px-5";
// The band down the most popular plan.
const HIGHLIGHT = "bg-[#D1EFED]/60";

/** forma2's PlanComparisonTable, the a-b_billing_plan-comparison-table experiment: plan headers over a feature matrix, with "Compare all details" revealing what every plan shares. */
export const PlanComparisonTable = ({ columns }: { columns: PlanColumn[] }) => {
  const { t } = useTranslation();
  const [showDetails, setShowDetails] = useState(false);

  const renderValue = (value: ComparisonValue): ReactNode => {
    if (value === true)
      return (
        <>
          <CheckIcon className="size-5 text-emerald-600" strokeWidth={3} aria-hidden="true" />
          <span className="sr-only">{t("workspace.settings.billing.comparison_included")}</span>
        </>
      );
    if (value === false)
      return (
        <>
          <span aria-hidden="true" className="text-slate-300">
            —
          </span>
          <span className="sr-only">{t("workspace.settings.billing.comparison_not_included")}</span>
        </>
      );
    if (value === "addon")
      return <span className="text-slate-500">{t("workspace.settings.billing.comparison_addon")}</span>;
    return <span className="text-slate-700">{value}</span>;
  };

  const feature = (label: string, values: ComparisonValue[]): ComparisonRow => ({
    type: "feature",
    label,
    values,
  });
  const section = (label: string): ComparisonRow => ({ type: "section", label });

  const rows: ComparisonRow[] = [
    section(t("workspace.settings.billing.comparison_section_basic_usage")),
    feature(t("workspace.settings.billing.comparison_row_workspaces"), ["1", "3", "5"]),
    feature(t("workspace.settings.billing.comparison_row_responses"), ["250", "2,000", "5,000"]),
    feature(t("workspace.settings.billing.comparison_row_overage"), [false, true, true]),
    feature(t("workspace.settings.billing.comparison_row_unlimited_seats"), [true, true, true]),
    section(t("workspace.settings.billing.comparison_section_pro_unlocks")),
    feature(t("workspace.settings.billing.comparison_row_hide_branding"), [false, true, true]),
    feature(t("workspace.settings.billing.comparison_row_contact_management"), [false, true, true]),
    feature(t("workspace.settings.billing.comparison_row_attribute_segmentation"), [false, true, true]),
    feature(t("workspace.settings.billing.comparison_row_email_followups"), [false, true, true]),
    feature(t("workspace.settings.billing.comparison_row_ai_translations"), [false, true, true]),
    ...(showDetails
      ? [
          section(t("workspace.settings.billing.comparison_section_all_plans")),
          feature(t("workspace.settings.billing.comparison_row_link_surveys"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_inproduct_surveys"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_question_types"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_conditional_logic"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_hidden_fields"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_partial_responses"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_recall"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_backgrounds"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_file_uploads"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_single_use_links"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_api"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_frankfurt"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_respondent_id"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_all_integrations"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_custom_webhooks"), [true, true, true]),
          feature(t("workspace.settings.billing.comparison_row_mobile_sdks"), [true, true, true]),
          section(t("workspace.settings.billing.comparison_section_scale_unlocks")),
          feature(t("workspace.settings.billing.comparison_row_teams_roles"), [false, false, true]),
          feature(t("workspace.settings.billing.comparison_row_quota"), [false, false, true]),
          feature(t("workspace.settings.billing.comparison_row_workflows"), [false, false, true]),
          feature(t("workspace.settings.billing.comparison_row_custom_dashboards"), [false, false, "addon"]),
          feature(t("workspace.settings.billing.comparison_row_two_factor_auth"), [false, false, "addon"]),
        ]
      : []),
  ];

  return (
    <div className="overflow-x-auto">
      <div
        role="table"
        aria-label={t("workspace.settings.billing.plan_selection_title")}
        className="grid min-w-[48rem] grid-cols-[minmax(0,1fr)_repeat(3,minmax(0,1fr))]">
        <div role="row" className="contents">
          <div role="columnheader" className={LABEL_CELL} />
          {columns.map((col) => (
            <div
              key={col.key}
              role="columnheader"
              className={cn(
                "flex flex-col pt-6",
                PLAN_CELL,
                col.isPopular && cn(HIGHLIGHT, "rounded-t-2xl")
              )}>
              <div className="mb-4 flex min-h-7 items-start gap-2">
                {col.isPopular && (
                  <span className="rounded-md bg-slate-100 px-2 py-1 text-xs font-medium text-slate-600">
                    {t("workspace.settings.billing.most_popular")}
                  </span>
                )}
                {col.isCurrent && (
                  <span className="rounded-md bg-emerald-100 px-2 py-1 text-xs font-medium text-emerald-700">
                    {t("workspace.settings.billing.current_plan_badge")}
                  </span>
                )}
                {col.isPending && (
                  <span className="rounded-md bg-amber-100 px-2 py-1 text-xs font-medium text-amber-700">
                    {t("workspace.settings.billing.pending_plan_badge")}
                  </span>
                )}
              </div>
              <h3 className="text-2xl font-semibold text-slate-900 sm:text-3xl">{col.name}</h3>
              <div className="mt-4 flex min-h-12 flex-wrap items-end gap-x-2">
                <span className="text-2xl font-normal tracking-tight text-slate-900 sm:text-3xl">
                  {col.amount}
                </span>
                <span className="pb-1 text-sm text-slate-500">{col.periodLabel}</span>
              </div>
              <Button
                variant={col.ctaVariant}
                className="mt-4 w-full"
                disabled={col.ctaDisabled}
                loading={col.ctaLoading}
                onClick={col.onCtaClick}>
                {col.ctaLabel}
              </Button>
            </div>
          ))}
        </div>

        <div role="row" className="contents">
          <div role="rowheader" className={cn(LABEL_CELL, "pt-6 pb-6 text-sm font-medium text-slate-600")}>
            {t("workspace.settings.billing.comparison_best_for")}
          </div>
          {columns.map((col) => (
            <div
              key={col.key}
              role="cell"
              className={cn(
                PLAN_CELL,
                "pt-6 pb-6 text-sm leading-6 text-slate-500",
                col.isPopular && HIGHLIGHT
              )}>
              {col.description}
            </div>
          ))}
        </div>

        {rows.map((row, rowIndex) => {
          const hasTopBorder = rowIndex === 0 || row.type === "section";
          const isSection = row.type === "section";
          const isLastRow = rowIndex === rows.length - 1;
          return (
            <div key={`${row.type}-${row.label}`} role="row" className="contents">
              <div
                role="rowheader"
                className={cn(
                  LABEL_CELL,
                  hasTopBorder && "border-t border-slate-100",
                  isSection
                    ? "pt-8 pb-2 text-xs font-semibold tracking-wide text-slate-400 uppercase"
                    : "py-3 text-sm text-slate-600",
                  isLastRow && "pb-6"
                )}>
                {row.label}
              </div>
              {columns.map((col, columnIndex) => (
                <div
                  key={col.key}
                  role="cell"
                  className={cn(
                    PLAN_CELL,
                    "flex justify-center text-center",
                    hasTopBorder && "border-t border-slate-100",
                    isSection ? "pt-8 pb-2" : "py-3 text-sm",
                    isLastRow && "pb-6",
                    col.isPopular && HIGHLIGHT,
                    col.isPopular && isLastRow && "rounded-b-2xl"
                  )}>
                  {row.type === "feature" ? renderValue(row.values[columnIndex] ?? false) : null}
                </div>
              ))}
            </div>
          );
        })}
      </div>

      <div className="mt-8 flex justify-center">
        <Button variant="secondary" onClick={() => setShowDetails((prev) => !prev)}>
          {showDetails
            ? t("workspace.settings.billing.plan_comparison_hide_details")
            : t("workspace.settings.billing.plan_comparison_see_details")}
        </Button>
      </div>
    </div>
  );
};
