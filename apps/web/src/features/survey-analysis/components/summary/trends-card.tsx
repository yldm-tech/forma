import type { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { DownloadIcon, TrendingUpIcon } from "lucide-react";
import { type KeyboardEvent, useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../../components/ui/dropdown-menu";
import { Select } from "../../../../components/ui/select";
import { ApiError } from "../../../../lib/api";
import { cn } from "../../../../lib/cn";
import { errorMessage } from "../../../../lib/errors";
import { toast } from "../../../../lib/toast";
import { getElementsFromBlocks } from "../../../survey-editor/lib/client-utils";
import { getElementTypes } from "../../../survey-editor/lib/elements";
import {
  type ExportFormat,
  type SurveyTrends,
  TREND_TOO_MANY_BUCKETS,
  type TrendInterval,
  analysisApi,
  surveyTrendsQuery,
} from "../../api";
import { saveExport } from "../../lib/download";
import { sharedTrendsQuery } from "../../result-shares";
import {
  type TrendDatum,
  type TrendScale,
  type TrendView,
  bucketLabel,
  bucketRangeLabel,
  countScale,
  formatTrendValue,
  labelledBuckets,
  metricScale,
  tooltipPosition,
  trendData,
  viewElement,
} from "../../lib/trends";
import { useResponseFilter } from "../filter/response-filter-context";

const INTERVALS: TrendInterval[] = ["day", "week", "month"];

/** How many bucket labels the x axis prints at most. */
const MAX_X_LABELS = 8;

/** The y position of value on the scale, as a percentage from the top of the plot. */
const yPercent = (value: number, scale: TrendScale) =>
  100 - ((Math.min(Math.max(value, scale.min), scale.max) - scale.min) / (scale.max - scale.min)) * 100;

type ValueKind = "count" | "percentage" | "nps" | "satisfied" | "average";

/** The chart: bars of responses split into completed and not completed, or a line of a rate or score with a gap where a bucket has none; a partial period is drawn muted. Axis text is HTML so it keeps its size at any width; only the line is drawn in a stretched SVG. Hovering a bucket shows its tooltip, as does focusing the plot, a slider over the buckets whose arrow keys move between them; the same numbers are in the table below for screen readers. */
const TrendChart = ({
  data,
  scale,
  kind,
  interval,
  locale,
  label,
}: {
  data: TrendDatum[];
  scale: TrendScale;
  kind: ValueKind;
  interval: TrendInterval;
  locale: string;
  label: string;
}) => {
  const { t } = useTranslation();
  const [active, setActive] = useState<number | null>(null);
  const n = data.length;
  const labelled = labelledBuckets(n, MAX_X_LABELS);
  const bars = kind === "count";
  // The line runs through the buckets' centres, broken where a bucket has no value, and muted on a step to or from a partial period; a lone value shows as its dot.
  const segments: { key: number; points: string; muted: boolean }[] = [];
  data.forEach((datum, i) => {
    const before = data[i - 1];
    if (before === undefined || datum.value === null || before.value === null) return;
    segments.push({
      key: i,
      points: `${i - 0.5},${yPercent(before.value, scale)} ${i + 0.5},${yPercent(datum.value, scale)}`,
      muted: before.bucket.partial || datum.bucket.partial,
    });
  });
  const activeDatum = active === null ? undefined : data[active];
  const last = n - 1;
  // The bucket the slider is on: the active one, the latest until one is.
  const current = Math.min(active ?? last, last);
  const currentDatum = data[current];
  // A bucket as the slider reads it out: its days, partial or not, and its value.
  const valueText = (datum: TrendDatum) => {
    const period = bucketRangeLabel(datum.bucket, locale);
    return t("workspace.analysis.trends.bucket_value", {
      period: datum.bucket.partial ? t("workspace.analysis.trends.partial_row", { period }) : period,
      value: bars
        ? t("workspace.analysis.trends.responses_count", { count: datum.value ?? 0 })
        : datum.value === null
          ? t("workspace.analysis.trends.no_data")
          : formatTrendValue(datum.value, kind, locale),
    });
  };
  // The arrow keys move the tooltip between the buckets while the plot has focus.
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const next =
      event.key === "ArrowLeft"
        ? Math.max(0, current - 1)
        : event.key === "ArrowRight"
          ? Math.min(last, current + 1)
          : event.key === "Home"
            ? 0
            : event.key === "End"
              ? last
              : undefined;
    if (next === undefined) return;
    event.preventDefault();
    setActive(next);
  };

  return (
    <div className="flex gap-2">
      <div className="relative h-48 w-12 shrink-0 text-right text-xs text-slate-500" aria-hidden="true">
        {scale.ticks.map((tick) => (
          <span
            key={tick}
            className="absolute right-0 -translate-y-1/2"
            style={{ top: `${yPercent(tick, scale)}%` }}>
            {formatTrendValue(tick, kind === "average" ? "tick" : kind, locale)}
          </span>
        ))}
      </div>
      <div className="min-w-0 flex-1">
        <div
          role="slider"
          aria-label={label}
          aria-valuemin={0}
          aria-valuemax={last}
          aria-valuenow={current}
          aria-valuetext={currentDatum ? valueText(currentDatum) : undefined}
          tabIndex={0}
          className="focus-visible:ring-ring relative h-48 rounded-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden"
          onMouseLeave={() => setActive(null)}
          onFocus={() => setActive((current) => current ?? n - 1)}
          onBlur={() => setActive(null)}
          onKeyDown={onKeyDown}>
          {scale.ticks.map((tick) => (
            <div
              key={tick}
              aria-hidden="true"
              className={cn(
                "absolute inset-x-0 border-t",
                tick === 0 && scale.min < 0 ? "border-slate-400" : "border-slate-100"
              )}
              style={{ top: `${yPercent(tick, scale)}%` }}
            />
          ))}
          <div className="absolute inset-0 flex">
            {data.map((datum, i) => (
              <div
                key={datum.bucket.start}
                data-testid="trend-bucket"
                className={cn("relative h-full flex-1", active === i && "bg-slate-50")}
                onMouseEnter={() => setActive(i)}>
                {bars && datum.value !== null && datum.value > 0 && (
                  <div
                    className={cn(
                      "absolute inset-x-[15%] bottom-0 flex flex-col justify-end gap-0.5",
                      datum.bucket.partial && "opacity-40"
                    )}
                    style={{ height: `${100 - yPercent(datum.value, scale)}%` }}>
                    {datum.value - (datum.completed ?? 0) > 0 && (
                      <div
                        className="rounded-t-sm bg-slate-300"
                        style={{ flexGrow: datum.value - (datum.completed ?? 0) }}
                      />
                    )}
                    {(datum.completed ?? 0) > 0 && (
                      <div
                        className={cn("bg-slate-700", datum.value === datum.completed && "rounded-t-sm")}
                        style={{ flexGrow: datum.completed }}
                      />
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
          {!bars && (
            <svg
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 size-full overflow-visible"
              viewBox={`0 0 ${Math.max(n, 1)} 100`}
              preserveAspectRatio="none">
              {segments.map((segment) => (
                <polyline
                  key={segment.key}
                  points={segment.points}
                  fill="none"
                  className={segment.muted ? "stroke-slate-400" : "stroke-slate-700"}
                  strokeWidth={2}
                  strokeLinecap="round"
                  vectorEffect="non-scaling-stroke"
                />
              ))}
            </svg>
          )}
          {!bars && (
            <div aria-hidden="true" className="pointer-events-none absolute inset-0 flex">
              {data.map((datum) => (
                <div key={datum.bucket.start} className="relative h-full flex-1">
                  {datum.value !== null && (
                    <span
                      className={cn(
                        "absolute left-1/2 size-2 -translate-x-1/2 -translate-y-1/2 rounded-full ring-2 ring-white",
                        datum.bucket.partial ? "border-2 border-slate-500 bg-white" : "bg-slate-700"
                      )}
                      style={{ top: `${yPercent(datum.value, scale)}%` }}
                    />
                  )}
                </div>
              ))}
            </div>
          )}
          {activeDatum && active !== null && (
            <div
              role="tooltip"
              className="pointer-events-none absolute z-10 w-max max-w-56 rounded-md border border-slate-200 bg-white px-3 py-2 text-xs text-slate-700 shadow-md"
              style={tooltipPosition(
                active,
                n,
                activeDatum.value === null ? null : yPercent(activeDatum.value, scale)
              )}>
              <p className="font-semibold text-slate-900">{bucketRangeLabel(activeDatum.bucket, locale)}</p>
              {bars ? (
                <>
                  <p>{t("workspace.analysis.trends.responses_count", { count: activeDatum.value ?? 0 })}</p>
                  <p>
                    {t("workspace.analysis.trends.completed_count", { count: activeDatum.completed ?? 0 })}
                  </p>
                </>
              ) : (
                <>
                  <p className="text-sm font-semibold text-slate-900">
                    {activeDatum.value === null
                      ? t("workspace.analysis.trends.no_data")
                      : formatTrendValue(activeDatum.value, kind, locale)}
                  </p>
                  <p>
                    {activeDatum.answered === undefined
                      ? t("workspace.analysis.trends.responses_count", {
                          count: activeDatum.bucket.responses,
                        })
                      : t("workspace.analysis.trends.answers_count", { count: activeDatum.answered })}
                  </p>
                </>
              )}
              {activeDatum.bucket.partial && (
                <p className="mt-1 text-slate-500">{t("workspace.analysis.trends.partial_tooltip")}</p>
              )}
            </div>
          )}
        </div>
        <div className="flex pt-1 text-xs text-slate-500" aria-hidden="true">
          {data.map((datum, i) => (
            <div key={datum.bucket.start} className="relative h-4 flex-1">
              {labelled.has(i) && (
                <span className="absolute left-1/2 -translate-x-1/2 whitespace-nowrap">
                  {bucketLabel(datum.bucket, interval, locale)}
                </span>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

/** The trend table for screen readers: a row per bucket with the value the chart draws. */
const TrendTable = ({
  data,
  kind,
  caption,
  valueHeader,
  locale,
}: {
  data: TrendDatum[];
  kind: ValueKind;
  caption: string;
  valueHeader: string;
  locale: string;
}) => {
  const { t } = useTranslation();
  const bars = kind === "count";
  return (
    <table className="sr-only">
      <caption>{caption}</caption>
      <thead>
        <tr>
          <th scope="col">{t("workspace.analysis.trends.period")}</th>
          <th scope="col">{valueHeader}</th>
          <th scope="col">
            {bars ? t("workspace.analysis.trends.completed") : t("workspace.analysis.trends.answers")}
          </th>
        </tr>
      </thead>
      <tbody>
        {data.map((datum) => (
          <tr key={datum.bucket.start}>
            <th scope="row">
              {datum.bucket.partial
                ? t("workspace.analysis.trends.partial_row", {
                    period: bucketRangeLabel(datum.bucket, locale),
                  })
                : bucketRangeLabel(datum.bucket, locale)}
            </th>
            <td>
              {datum.value === null
                ? t("workspace.analysis.trends.no_data")
                : formatTrendValue(datum.value, kind, locale)}
            </td>
            <td>{bars ? (datum.completed ?? 0) : (datum.answered ?? datum.bucket.responses)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
};

/** The trend over time of the summary: responses, completion rate, or a score element's NPS, CSAT or average per day, week or month in the organization's time zone, under the current filter. In a read-only results link (sharedToken) it reads the trend through the link, under the filter the link froze, and offers no download. */
export const TrendsCard = ({
  survey,
  locale,
  sharedToken,
}: {
  survey: TSurvey;
  locale: string;
  sharedToken?: string;
}) => {
  const { t } = useTranslation();
  const selectId = useId();
  const { filterCriteria } = useResponseFilter();
  // No interval until the user picks one: the server picks one from the range and says which.
  const [chosenInterval, setChosenInterval] = useState<TrendInterval | undefined>(undefined);
  const [view, setView] = useState<TrendView>("responses");
  const [isDownloading, setIsDownloading] = useState(false);
  const isShared = sharedToken !== undefined;
  // One of the two runs: the analysis page's under its filter, or the link's.
  const analysisQuery = useQuery({
    ...surveyTrendsQuery(survey.id, filterCriteria, chosenInterval),
    placeholderData: keepPreviousData,
    enabled: !isShared,
  });
  const sharedQuery = useQuery({
    ...sharedTrendsQuery(sharedToken ?? "", chosenInterval),
    placeholderData: keepPreviousData,
    enabled: isShared,
  });
  const query = isShared ? sharedQuery : analysisQuery;
  const trends: SurveyTrends | undefined = query.data;
  const shownInterval = trends?.interval ?? chosenInterval ?? "day";
  // The radios show the user's pick at once, while the chart keeps the interval of the data it draws until the new data arrives.
  const checkedInterval = chosenInterval ?? shownInterval;

  const elementTypes = useMemo(() => getElementTypes(t), [t]);
  const surveyElements = useMemo(() => getElementsFromBlocks(survey.blocks), [survey.blocks]);
  const elementLabel = (elementId: string, type: string, fallback: string) => {
    const element = surveyElements.find((el) => el.id === elementId);
    const headline = element ? getTextContent(element.headline.default ?? "") : fallback;
    const typeLabel = elementTypes.find((elementType) => elementType.id === type)?.label ?? type;
    return t("workspace.analysis.trends.view_element", { type: typeLabel, headline: headline || fallback });
  };
  const viewOptions: { value: TrendView; label: string }[] = [
    { value: "responses", label: t("workspace.analysis.trends.view_responses") },
    { value: "completion", label: t("workspace.analysis.trends.view_completion") },
    ...(trends?.elements ?? []).map((element) => ({
      value: `element:${element.elementId}` as TrendView,
      label: elementLabel(element.elementId, element.type, element.headline),
    })),
  ];
  // A view of an element the survey no longer has falls back to the responses.
  const activeView: TrendView = viewOptions.some((option) => option.value === view) ? view : "responses";
  const activeViewLabel = viewOptions.find((option) => option.value === activeView)?.label ?? "";
  const element = trends ? viewElement(trends, activeView) : undefined;
  const data = trends ? trendData(trends, activeView) : [];
  const kind: ValueKind =
    activeView === "responses"
      ? "count"
      : activeView === "completion"
        ? "percentage"
        : (element?.metric ?? "average");
  const surveyElement = surveyElements.find((candidate) => candidate.id === element?.elementId);
  const elementRange =
    surveyElement && "range" in surveyElement && typeof surveyElement.range === "number"
      ? surveyElement.range
      : undefined;
  const scale =
    kind === "count"
      ? countScale(Math.max(0, ...data.map((datum) => datum.value ?? 0)))
      : metricScale(
          kind,
          elementRange,
          data.map((datum) => datum.value)
        );
  const hint =
    kind === "count"
      ? null
      : kind === "percentage"
        ? t("workspace.analysis.trends.hint_completion")
        : kind === "nps"
          ? t("workspace.analysis.trends.hint_nps")
          : kind === "satisfied"
            ? t("workspace.analysis.trends.hint_satisfied")
            : t("workspace.analysis.trends.hint_average");
  const tooManyBuckets = query.error instanceof ApiError && query.error.code === TREND_TOO_MANY_BUCKETS;
  // Only an interval the user picked can be refused for its length, so the advice follows it: a longer interval while there is one, a shorter date range by month.
  const tooManyBucketsMessage = {
    day: t("workspace.analysis.trends.too_many_buckets_day"),
    week: t("workspace.analysis.trends.too_many_buckets_week"),
    month: t("workspace.analysis.trends.too_many_buckets_month"),
  }[chosenInterval ?? "month"];
  const intervalLabels: Record<TrendInterval, string> = {
    day: t("workspace.analysis.trends.interval_day"),
    week: t("workspace.analysis.trends.interval_week"),
    month: t("workspace.analysis.trends.interval_month"),
  };

  const download = async (format: ExportFormat) => {
    setIsDownloading(true);
    try {
      saveExport(
        await analysisApi.exportTrends(survey.id, format, filterCriteria, chosenInterval),
        format,
        "trends"
      );
    } catch (error) {
      toast.error(
        error instanceof ApiError && error.code === TREND_TOO_MANY_BUCKETS
          ? tooManyBucketsMessage
          : t("workspace.analysis.trends.download_error")
      );
    } finally {
      setIsDownloading(false);
    }
  };

  const renderBody = () => {
    if (query.isPending) {
      return <div className="h-56 animate-pulse rounded-lg bg-slate-100" />;
    }
    if (query.isError && !trends) {
      return (
        <p role="alert" className="py-12 text-center text-sm text-slate-600">
          {tooManyBuckets ? tooManyBucketsMessage : errorMessage(t, query.error)}
        </p>
      );
    }
    if (!trends || trends.buckets.length === 0 || trends.buckets.every((bucket) => bucket.responses === 0)) {
      return (
        <p className="py-12 text-center text-sm text-slate-500">{t("workspace.analysis.trends.empty")}</p>
      );
    }
    return (
      <>
        {query.isError && (
          <p role="alert" className="mb-2 text-sm text-red-600">
            {tooManyBuckets ? tooManyBucketsMessage : errorMessage(t, query.error)}
          </p>
        )}
        <TrendChart
          data={data}
          scale={scale}
          kind={kind}
          interval={shownInterval}
          locale={locale}
          label={t("workspace.analysis.trends.chart_label", {
            view: activeViewLabel,
            interval: intervalLabels[shownInterval],
          })}
        />
        <TrendTable
          data={data}
          kind={kind}
          caption={t("workspace.analysis.trends.chart_label", {
            view: activeViewLabel,
            interval: intervalLabels[shownInterval],
          })}
          valueHeader={activeViewLabel}
          locale={locale}
        />
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-slate-500">
          {kind === "count" && (
            <>
              <span className="flex items-center gap-1.5">
                <span aria-hidden="true" className="size-2.5 rounded-sm bg-slate-700" />
                {t("workspace.analysis.trends.completed")}
              </span>
              <span className="flex items-center gap-1.5">
                <span aria-hidden="true" className="size-2.5 rounded-sm bg-slate-300" />
                {t("workspace.analysis.trends.not_completed")}
              </span>
            </>
          )}
          {data.some((datum) => datum.bucket.partial) && (
            <span className="flex items-center gap-1.5">
              <span
                aria-hidden="true"
                className={
                  kind === "count"
                    ? "size-2.5 rounded-sm bg-slate-700 opacity-40"
                    : "size-2 rounded-full border-2 border-slate-500 bg-white"
                }
              />
              {t("workspace.analysis.trends.partial")}
            </span>
          )}
          {hint && <span>{hint}</span>}
        </div>
      </>
    );
  };

  return (
    <section
      aria-labelledby={`${selectId}-title`}
      data-testid="summary-trends"
      className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs md:p-6">
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3
            id={`${selectId}-title`}
            className="flex items-center gap-2 text-lg font-semibold text-slate-900">
            <TrendingUpIcon className="size-5 text-slate-600" aria-hidden="true" />
            {t("workspace.analysis.trends.title")}
          </h3>
          <p className="text-sm text-slate-500">
            {t("workspace.analysis.trends.description", {
              timeZone: trends?.timeZone ?? "UTC",
            })}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label htmlFor={selectId} className="sr-only">
            {t("workspace.analysis.trends.view_label")}
          </label>
          <Select
            id={selectId}
            containerClassName="w-auto max-w-72"
            className="h-9"
            value={activeView}
            onChange={(event) => setView(event.target.value as TrendView)}>
            {viewOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
          <div
            role="radiogroup"
            aria-label={t("workspace.analysis.trends.interval_label")}
            className="inline-flex items-center rounded-lg bg-slate-100 p-1 text-slate-500">
            {INTERVALS.map((option) => (
              <label
                key={option}
                className={cn(
                  "cursor-pointer rounded-md px-3 py-1 text-sm font-medium has-focus-visible:ring-2 has-focus-visible:ring-ring",
                  checkedInterval === option ? "bg-white text-slate-900 shadow-xs" : "hover:text-slate-700"
                )}>
                <input
                  type="radio"
                  name={`${selectId}-interval`}
                  value={option}
                  checked={checkedInterval === option}
                  onChange={() => setChosenInterval(option)}
                  className="sr-only"
                />
                {intervalLabels[option]}
              </label>
            ))}
          </div>
          {!isShared && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  loading={isDownloading}
                  disabled={!trends || trends.buckets.length === 0}
                  aria-label={t("workspace.analysis.trends.download")}
                  title={t("workspace.analysis.trends.download")}>
                  <DownloadIcon />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => void download("csv")}>
                  {t("workspace.analysis.trends.download_csv")}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => void download("xlsx")}>
                  {t("workspace.analysis.trends.download_xlsx")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
      </div>
      {renderBody()}
    </section>
  );
};
