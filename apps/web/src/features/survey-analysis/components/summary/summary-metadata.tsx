import type { TSurveySummary } from "@forma/types/surveys/types";
import { ChevronDownIcon, ChevronUpIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../../../../components/ui/tooltip";
import { cn } from "../../../../lib/cn";
import { formatTimeToComplete, visiblePercentage } from "../../lib/summary";

export type SummaryTab = "dropOffs" | "quotas" | "impressions";

const BaseCard = ({
  label,
  percentage = null,
  tooltipText,
  isLoading = false,
  onClick,
  children,
  testId,
}: {
  label: ReactNode;
  percentage?: number | null;
  tooltipText?: ReactNode;
  isLoading?: boolean;
  onClick?: () => void;
  children: ReactNode;
  testId?: string;
}) => {
  const content = (
    <div
      className={cn(
        "flex h-full flex-col justify-between space-y-2 rounded-xl border border-slate-200 bg-white p-4 text-left shadow-xs",
        onClick ? "cursor-pointer" : "cursor-default"
      )}>
      <p className="flex items-center gap-1 text-sm text-slate-600">
        {label}
        {typeof percentage === "number" && Number.isFinite(percentage) && !isLoading && (
          <span className="ml-1 rounded-xl bg-slate-100 px-2 py-1 text-xs">{Math.round(percentage)}%</span>
        )}
      </p>
      {children}
    </div>
  );
  return (
    <TooltipProvider delayDuration={50}>
      <Tooltip>
        <TooltipTrigger asChild>
          {onClick ? (
            <button
              type="button"
              onClick={onClick}
              data-testid={testId}
              id={testId}
              className="h-full w-full">
              {content}
            </button>
          ) : (
            <div className="h-full w-full">{content}</div>
          )}
        </TooltipTrigger>
        {tooltipText && (
          <TooltipContent>
            <p>{tooltipText}</p>
          </TooltipContent>
        )}
      </Tooltip>
    </TooltipProvider>
  );
};

const Placeholder = () => <div className="h-6 w-12 animate-pulse rounded-full bg-slate-200" />;

/** A value of the summary row: "-" stands for zero. */
const valueOrDash = (value: number): ReactNode => (value === 0 ? <span>-</span> : value);

/** forma2's SummaryMetadata: impressions, starts, completions, drop-offs, time to complete and quotas; three of them open a detail tab. */
export const SummaryMetadata = ({
  surveySummary,
  quotasCount,
  isLoading,
  tab,
  setTab,
}: {
  surveySummary: TSurveySummary["meta"];
  quotasCount: number;
  isLoading: boolean;
  tab: SummaryTab | undefined;
  setTab: (tab: SummaryTab | undefined) => void;
}) => {
  const { t } = useTranslation();
  const toggle = (value: SummaryTab) => setTab(tab === value ? undefined : value);

  const interactive = (
    value: SummaryTab,
    label: string,
    shown: ReactNode,
    percentage: number | null,
    tooltip: string
  ) => (
    <BaseCard
      label={label}
      percentage={percentage}
      tooltipText={tooltip}
      isLoading={isLoading}
      onClick={() => toggle(value)}
      testId={`${value}-toggle`}>
      <div className="flex w-full items-end justify-between">
        <span className="text-2xl font-bold text-slate-800">{isLoading ? <Placeholder /> : shown}</span>
        {!isLoading && (
          <div className="flex size-6 items-center justify-center rounded-md border border-slate-200 bg-slate-50 hover:bg-slate-100">
            {tab === value ? <ChevronUpIcon className="size-4" /> : <ChevronDownIcon className="size-4" />}
          </div>
        )}
      </div>
    </BaseCard>
  );

  const stat = (label: string, shown: ReactNode, percentage: number | null, tooltip: string) => (
    <BaseCard label={label} percentage={percentage} tooltipText={tooltip} isLoading={isLoading}>
      {isLoading ? <Placeholder /> : <p className="text-2xl font-bold text-slate-800">{shown}</p>}
    </BaseCard>
  );

  return (
    <div
      className={cn(
        "grid gap-4 sm:grid-cols-2 md:grid-cols-3 md:gap-x-2 lg:grid-cols-3 2xl:grid-cols-5",
        quotasCount > 0 && "2xl:grid-cols-6"
      )}>
      {interactive(
        "impressions",
        t("workspace.surveys.summary.impressions"),
        valueOrDash(surveySummary.displayCount),
        null,
        t("workspace.surveys.summary.impressions_tooltip")
      )}
      {stat(
        t("workspace.surveys.summary.starts"),
        valueOrDash(surveySummary.totalResponses),
        visiblePercentage(surveySummary.startsPercentage),
        t("workspace.surveys.summary.starts_tooltip")
      )}
      {stat(
        t("workspace.surveys.summary.completed"),
        valueOrDash(surveySummary.completedResponses),
        visiblePercentage(surveySummary.completedPercentage),
        t("workspace.surveys.summary.completed_tooltip")
      )}
      {interactive(
        "dropOffs",
        t("workspace.surveys.summary.drop_offs"),
        valueOrDash(surveySummary.dropOffCount),
        surveySummary.dropOffPercentage,
        t("workspace.surveys.summary.drop_offs_tooltip")
      )}
      {stat(
        t("workspace.surveys.summary.time_to_complete"),
        surveySummary.ttcAverage === 0 ? <span>-</span> : formatTimeToComplete(surveySummary.ttcAverage),
        null,
        t("workspace.surveys.summary.ttc_survey_tooltip")
      )}
      {quotasCount > 0 &&
        interactive(
          "quotas",
          t("workspace.surveys.summary.quotas_completed"),
          surveySummary.quotasCompleted,
          surveySummary.quotasCompletedPercentage,
          t("workspace.surveys.summary.quotas_completed_tooltip")
        )}
    </div>
  );
};
