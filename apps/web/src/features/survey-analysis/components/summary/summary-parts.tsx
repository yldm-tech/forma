import type { TI18nString } from "@forma/types/i18n";
import type { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey, TSurveyElementSummary, TSurveySummaryBreakdown } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { Link, useParams } from "@tanstack/react-router";
import { BarChart, BarChartHorizontal, EyeIcon, InboxIcon, SkipForwardIcon } from "lucide-react";
import { type CSSProperties, type ReactNode, createContext, useContext } from "react";
import { useTranslation } from "react-i18next";
import { IdBadge } from "../../../../components/ui/id-badge";
import { ProgressBar } from "../../../../components/ui/progress-bar";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../../../../components/ui/tooltip";
import { cn } from "../../../../lib/cn";
import { recallToHeadline } from "../../../../lib/survey/recall";
import { getElementTypes } from "../../../survey-editor/lib/elements";
import { formatTextWithSlashes } from "../../../survey-editor/lib/utils";
import {
  convertFloatToNDecimal,
  getElementDenominator,
  getSampleTruncation,
  satisfactionColor,
} from "../../lib/summary";
import { RatingResponse } from "../response-values/rating-response";
import { useElementImpressions } from "./element-impressions-context";

/** Whether the summaries render in a read-only results link rather than the analysis page: there a bar filters nothing, a sample opens no response, and nobody who answered is named. */
export const SharedSummaryContext = createContext(false);

/** True inside a read-only results link. */
export const useIsSharedSummary = () => useContext(SharedSummaryContext);

/** What a click on a summary bar does: forma2's setFilter, adding or replacing the element's filter row. */
export type SetSummaryFilter = (
  elementId: string,
  label: TI18nString,
  elementType: TSurveyElementTypeEnum,
  filterValue: string,
  filterComboBoxValue?: string | string[]
) => void;

/** forma2's ElementSummaryHeader: the element's headline, type, answer count, denominator and id. */
export const ElementSummaryHeader = ({
  elementSummary,
  additionalInfo,
  showResponses = true,
  survey,
}: {
  elementSummary: TSurveyElementSummary;
  showResponses?: boolean;
  additionalInfo?: ReactNode;
  survey: TSurvey;
}) => {
  const { t } = useTranslation();
  const elementType = getElementTypes(t).find((type) => type.id === elementSummary.element.type);
  // The CTA card renders its own impressions row and passes showResponses={false}, so the denominator is not stated twice.
  const impressionCount = useElementImpressions(elementSummary.element.id);
  const denominator = showResponses
    ? getElementDenominator(impressionCount, elementSummary.responseCount)
    : null;
  const headline = recallToHeadline(elementSummary.element.headline, survey, true, "default").default ?? "";

  return (
    <div className="space-y-2 px-4 pt-6 pb-5 md:px-6">
      <div className="align-center flex justify-between gap-4">
        <h3 className="pb-1 text-lg font-semibold text-slate-900 md:text-xl">
          {formatTextWithSlashes(getTextContent(headline), "@", ["text-lg"])}
        </h3>
      </div>
      <div className="flex flex-wrap gap-2 text-xs font-semibold text-slate-600 md:text-sm">
        <div className="flex items-center rounded-lg bg-slate-100 p-2">
          {elementType && <elementType.icon className="mr-2 size-4" />}
          {t("workspace.surveys.summary.question_type", {
            type: elementType ? elementType.label : t("workspace.surveys.summary.unknown_question_type"),
          })}
        </div>
        {showResponses && (
          <div className="flex items-center rounded-lg bg-slate-100 p-2">
            <InboxIcon className="mr-2 size-4" />
            {t("common.count_responses", { count: elementSummary.responseCount })}
          </div>
        )}
        {denominator && (
          <div className="flex items-center rounded-lg bg-slate-100 p-2">
            <EyeIcon className="mr-2 size-4" />
            {t("workspace.surveys.summary.count_impressions", { count: denominator.impressionCount })}
          </div>
        )}
        {denominator && denominator.skipCount > 0 && (
          <div className="flex items-center rounded-lg bg-slate-100 p-2">
            <SkipForwardIcon className="mr-2 size-4" />
            {t("workspace.surveys.summary.count_skips_with_percentage", {
              count: denominator.skipCount,
              percentage: denominator.skipPercentage,
            })}
          </div>
        )}
        {additionalInfo}
        {!elementSummary.element.required && (
          <div className="flex items-center rounded-lg bg-slate-100 p-2">
            {t("workspace.surveys.edit.optional")}
          </div>
        )}
        <IdBadge id={elementSummary.element.id} />
      </div>
    </div>
  );
};

/** forma2's SampleTruncationNotice: "Shown: 50 / 312 responses" with a way to the Responses tab, which holds every answer, when a sample list was capped. */
export const SampleTruncationNotice = ({
  sampleCount,
  responseCount,
}: {
  sampleCount: number;
  responseCount: number;
}) => {
  const { t } = useTranslation();
  const params = useParams({ strict: false });
  const truncation = getSampleTruncation(sampleCount, responseCount);
  if (!truncation) return null;
  return (
    <div className="flex flex-wrap items-center justify-center gap-x-2 gap-y-1 py-3 text-sm text-slate-500">
      <span>
        {t("common.shown")}: {truncation.shown} / {t("common.count_responses", { count: truncation.total })}
      </span>
      {params.workspaceId && params.surveyId && (
        <Link
          to="/workspaces/$workspaceId/surveys/$surveyId/responses"
          params={{ workspaceId: params.workspaceId, surveyId: params.surveyId }}
          className="font-medium text-slate-700 underline underline-offset-2">
          {t("common.responses")}
        </Link>
      )}
    </div>
  );
};

/** A row or cell that filters the responses by what it shows; in a read-only results link it is shown, not offered. */
export const FilterTarget = ({
  children,
  onClick,
  className,
  style,
}: {
  children: ReactNode;
  onClick: () => void;
  className?: string;
  style?: CSSProperties;
}) => {
  const isShared = useIsSharedSummary();
  if (isShared) {
    return (
      <div className={cn(className, "cursor-default hover:opacity-100")} style={style}>
        {children}
      </div>
    );
  }
  return (
    <button type="button" className={className} style={style} onClick={onClick}>
      {children}
    </button>
  );
};

/** A bar or cell that filters the responses by what it shows, saying so in a tooltip; in a read-only results link it is shown, not offered. */
export const ClickableBarSegment = ({
  children,
  onClick,
  className = "",
  style,
}: {
  children: ReactNode;
  onClick: () => void;
  className?: string;
  style?: CSSProperties;
}) => {
  const { t } = useTranslation();
  const isShared = useIsSharedSummary();
  if (isShared) {
    return (
      <div className={cn(className, "cursor-default")} style={style}>
        {children}
      </div>
    );
  }
  return (
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button type="button" className={className} style={style} onClick={onClick}>
            {children}
          </button>
        </TooltipTrigger>
        <TooltipContent>{t("common.click_to_filter")}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
};

/** forma2's SatisfactionIndicator: a dot coloured by how satisfied the respondents are. */
export const SatisfactionIndicator = ({ percentage }: { percentage: number }) => (
  <div className={cn("h-3 w-3 rounded-full", satisfactionColor(percentage))} />
);

/** The aggregated / individual switch of the score cards. */
export const ViewToggle = ({
  value,
  onChange,
}: {
  value: "aggregated" | "individual";
  onChange: (value: "aggregated" | "individual") => void;
}) => {
  const { t } = useTranslation();
  const options = [
    { id: "aggregated" as const, label: t("workspace.surveys.summary.aggregated"), Icon: BarChartHorizontal },
    { id: "individual" as const, label: t("workspace.surveys.summary.individual"), Icon: BarChart },
  ];
  return (
    <div className="flex justify-end px-4 md:px-6">
      <div role="tablist" className="inline-flex items-center rounded-lg bg-slate-100 p-1 text-slate-500">
        {options.map(({ id, label, Icon }) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={value === id}
            onClick={() => onChange(id)}
            className={cn(
              "inline-flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium",
              value === id ? "bg-white text-slate-900 shadow-xs" : "hover:text-slate-700"
            )}>
            <Icon className="size-4" />
            {label}
          </button>
        ))}
      </div>
    </div>
  );
};

/** forma2's RatingScaleLegend: the two ends of a rating scale under its bar. A number scale's end already shows its number, so only stars and faces get one beside them. */
export const RatingScaleLegend = ({
  scale,
  range,
}: {
  scale: "number" | "star" | "smiley";
  range: number;
}) => {
  const showNumber = scale !== "number";
  return (
    <div className="mt-3 flex w-full items-start justify-between px-1">
      <div className="flex items-center gap-x-1">
        <RatingResponse scale={scale} answer={1} range={range} addColors={false} variant="scale" />
        {showNumber && <span className="text-xs text-slate-500">1</span>}
      </div>
      <div className="flex items-center gap-x-1">
        {showNumber && <span className="text-xs text-slate-500">{range}</span>}
        <RatingResponse scale={scale} answer={range} range={range} addColors={false} variant="scale" />
      </div>
    </div>
  );
};

/** The aggregated view of a free-text field (a hidden field, the values typed into Other): its most frequent values as bars, the others added up in a last bar. Shares are of the answers counted. */
export const BreakdownBars = ({
  breakdown,
  countLabel,
}: {
  breakdown: TSurveySummaryBreakdown;
  countLabel: (count: number) => string;
}) => {
  const { t } = useTranslation();
  const rows = breakdown.values.map((value) => ({ ...value, isRest: false }));
  if (breakdown.restCount > 0) {
    rows.push({
      value: t("workspace.analysis.breakdown_rest"),
      count: breakdown.restCount,
      percentage: breakdown.restPercentage,
      isRest: true,
    });
  }
  return (
    <ul className="space-y-5">
      {rows.map((row) => (
        <li key={row.isRest ? "rest" : `value-${row.value}`}>
          <div className="flex flex-col justify-between px-2 pb-2 sm:flex-row">
            <p
              className={cn(
                "mr-8 font-semibold break-words",
                row.isRest ? "text-slate-500 italic" : "ph-no-capture text-slate-700"
              )}
              dir="auto">
              {row.value}
            </p>
            <div className="flex shrink-0 gap-x-2">
              <p className="flex pt-1 text-slate-600 sm:items-end sm:pt-0">{countLabel(row.count)}</p>
              <p className="rounded-lg bg-slate-100 px-2 text-slate-700">
                {convertFloatToNDecimal(row.percentage, 2)}%
              </p>
            </div>
          </div>
          <ProgressBar
            barColor={row.isRest ? "bg-slate-400" : "bg-slate-700"}
            progress={row.percentage / 100}
          />
        </li>
      ))}
    </ul>
  );
};
