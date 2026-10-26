import type {
  TSurvey,
  TSurveyElementSummaryCal,
  TSurveyElementSummaryCes,
  TSurveyElementSummaryConsent,
  TSurveyElementSummaryCsat,
  TSurveyElementSummaryCta,
  TSurveyElementSummaryNps,
  TSurveyElementSummaryRating,
  TSurveyElementSummarySlider,
} from "@forma/types/surveys/types";
import { CircleSlash2, InboxIcon, SlidersHorizontalIcon, SmileIcon, StarIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { EmptyState } from "../../../../components/ui/empty-state";
import { HalfCircle, ProgressBar } from "../../../../components/ui/progress-bar";
import { TooltipRenderer } from "../../../../components/ui/tooltip";
import { cn } from "../../../../lib/cn";
import { SUMMARY_FILTER_OPERATOR, SUMMARY_FILTER_VALUE } from "../../lib/filters";
import { convertFloatToNDecimal, formatAverage, formatNpsScore, npsScoreColor } from "../../lib/summary";
import { RatingResponse } from "../response-values/rating-response";
import {
  ClickableBarSegment,
  ElementSummaryHeader,
  FilterTarget,
  RatingScaleLegend,
  SatisfactionIndicator,
  type SetSummaryFilter,
  ViewToggle,
} from "./summary-parts";

/** forma2's NPS bar opacity: detractors faint, passives stronger, promoters strongest. */
const calculateNPSOpacity = (rating: number): number => {
  if (rating <= 6) return 0.3 + (rating / 6) * 0.3;
  if (rating <= 8) return 0.6 + ((rating - 6) / 2) * 0.2;
  return 0.8 + ((rating - 8) / 2) * 0.2;
};

const NPS_GROUPS = ["promoters", "passives", "detractors", "dismissed"] as const;

type NpsGroup = (typeof NPS_GROUPS)[number];

/** Each group's bar colour: promoters emerald, passives slate, detractors rose, the dismissed neutral, so the composition reads at a glance. */
const NPS_GROUP_BAR_COLORS: Record<NpsGroup, string> = {
  promoters: "bg-emerald-500",
  passives: "bg-slate-400",
  detractors: "bg-rose-500",
  dismissed: "bg-slate-600",
};

/** forma2's NPSSummary: the score (in the header badge and the dial above the groups), the groups and the 0 to 10 bars, a click filtering by the group or the rating. */
export const NPSSummary = ({
  elementSummary,
  survey,
  setFilter,
}: {
  elementSummary: TSurveyElementSummaryNps;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
}) => {
  const { t, i18n } = useTranslation();
  const [activeTab, setActiveTab] = useState<"aggregated" | "individual">("aggregated");
  const promotersPercentage = convertFloatToNDecimal(elementSummary.promoters.percentage, 2);
  const groupLabels: Record<NpsGroup, string> = {
    promoters: t("workspace.surveys.summary.promoters"),
    passives: t("workspace.surveys.summary.passives"),
    detractors: t("workspace.surveys.summary.detractors"),
    dismissed: t("workspace.surveys.summary.nps_dismissed"),
  };

  // Wire operators, not labels: the filter matches these (lib/filters).
  const groupFilters: Record<NpsGroup, { comparison: string; values: string | string[] | undefined }> = {
    promoters: { comparison: SUMMARY_FILTER_OPERATOR.includesEither, values: ["9", "10"] },
    passives: { comparison: SUMMARY_FILTER_OPERATOR.includesEither, values: ["7", "8"] },
    detractors: { comparison: SUMMARY_FILTER_OPERATOR.isLessThan, values: "7" },
    dismissed: { comparison: SUMMARY_FILTER_OPERATOR.skipped, values: undefined },
  };

  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader
        elementSummary={elementSummary}
        survey={survey}
        additionalInfo={
          <TooltipRenderer
            tooltipContent={
              <div className="max-w-xs space-y-1">
                <p>{t("workspace.analysis.nps_formula_tooltip", { count: elementSummary.answered })}</p>
                <p>
                  {t("workspace.surveys.summary.nps_promoters_tooltip", {
                    percentage: promotersPercentage,
                  })}
                </p>
              </div>
            }
            delayDuration={150}>
            <div className="flex items-center gap-x-2 rounded-lg bg-slate-100 p-2">
              <div
                aria-hidden="true"
                className={cn("h-3 w-3 rounded-full", npsScoreColor(elementSummary.score))}
              />
              <div>
                {t("workspace.surveys.summary.nps_score_badge", {
                  score: formatNpsScore(elementSummary.score, i18n.resolvedLanguage ?? i18n.language),
                })}
              </div>
            </div>
          </TooltipRenderer>
        }
      />
      <div className="flex justify-center pt-2">
        <HalfCircle value={elementSummary.score} />
      </div>
      <ViewToggle value={activeTab} onChange={setActiveTab} />
      {activeTab === "aggregated" ? (
        <div className="mt-4 px-4 pt-4 pb-6 md:px-6">
          <div className="space-y-5 text-sm md:text-base">
            {NPS_GROUPS.map((group) => (
              <FilterTarget
                className="w-full cursor-pointer hover:opacity-80"
                key={group}
                onClick={() =>
                  setFilter(
                    elementSummary.element.id,
                    elementSummary.element.headline,
                    elementSummary.element.type,
                    groupFilters[group].comparison,
                    groupFilters[group].values
                  )
                }>
                <div
                  className={`mb-2 flex justify-between ${group === "dismissed" ? "border-t bg-white pt-4 text-sm md:text-base" : ""}`}>
                  <div className="mr-8 flex gap-x-1">
                    <p className="font-semibold text-slate-700">{groupLabels[group]}</p>
                    <p className="rounded-lg bg-slate-100 px-2 text-slate-700">
                      {convertFloatToNDecimal(elementSummary[group].percentage, 2)}%
                    </p>
                  </div>
                  <p className="flex w-32 items-end justify-end text-slate-600">
                    {t("common.count_responses", { count: elementSummary[group].count })}
                  </p>
                </div>
                <ProgressBar
                  barColor={NPS_GROUP_BAR_COLORS[group]}
                  progress={elementSummary[group].percentage / 100}
                />
              </FilterTarget>
            ))}
          </div>
        </div>
      ) : (
        <div className="mt-4 grid grid-cols-11 gap-2 px-4 pt-4 pb-6 text-sm md:px-6 md:text-base">
          {elementSummary.choices.map((choice) => (
            <ClickableBarSegment
              key={choice.rating}
              className="group flex cursor-pointer flex-col items-center"
              onClick={() =>
                setFilter(
                  elementSummary.element.id,
                  elementSummary.element.headline,
                  elementSummary.element.type,
                  SUMMARY_FILTER_OPERATOR.isEqualTo,
                  choice.rating.toString()
                )
              }>
              <div className="flex h-32 w-full flex-col items-center justify-end">
                <div
                  className="w-full rounded-t-lg border border-slate-200 bg-slate-700 transition-all group-hover:brightness-110"
                  style={{
                    height: `${Math.max(choice.percentage, 2)}%`,
                    opacity: calculateNPSOpacity(choice.rating),
                  }}
                />
              </div>
              <div className="flex w-full flex-col items-center rounded-b-lg border border-t-0 border-slate-200 bg-slate-50 px-1 py-2">
                <div className="mb-1.5 text-xs font-medium text-slate-500">{choice.rating}</div>
                <div className="mb-1 flex items-center gap-x-1">
                  <div className="text-base font-semibold text-slate-700">{choice.count}</div>
                  <div className="rounded-sm bg-slate-100 px-1.5 py-0.5 text-xs text-slate-600">
                    {convertFloatToNDecimal(choice.percentage, 1)}%
                  </div>
                </div>
              </div>
            </ClickableBarSegment>
          ))}
        </div>
      )}
    </div>
  );
};

type RatingLikeElementSummary =
  | TSurveyElementSummaryCes
  | TSurveyElementSummaryCsat
  | TSurveyElementSummaryRating;

/** forma2's RatingLikeSummary: the distribution of a rating, CSAT or CES element as one stacked bar or as rows, a click filtering by the rating. Ratings outside the scale arrive without a value and are left out of the bar. */
const RatingLikeSummary = ({
  elementSummary,
  survey,
  setFilter,
  additionalInfo,
}: {
  elementSummary: RatingLikeElementSummary;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
  additionalInfo: ReactNode;
}) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<"aggregated" | "individual">("aggregated");
  const { element } = elementSummary;
  const filterByRating = (rating: number | null) =>
    setFilter(element.id, element.headline, element.type, SUMMARY_FILTER_OPERATOR.isEqualTo, String(rating));
  const visibleChoices = elementSummary.choices.filter((choice) => choice.percentage !== 0);

  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader elementSummary={elementSummary} survey={survey} additionalInfo={additionalInfo} />
      <ViewToggle value={activeTab} onChange={setActiveTab} />
      {activeTab === "aggregated" ? (
        <div className="mt-4 px-4 pt-4 pb-6 md:px-6">
          {elementSummary.responseCount === 0 ? (
            <>
              <EmptyState text={t("workspace.surveys.summary.no_responses_found")} variant="simple" />
              <RatingScaleLegend scale={element.scale} range={element.range} />
            </>
          ) : (
            <>
              <div className="flex h-12 w-full overflow-hidden rounded-t-lg border border-slate-200">
                {visibleChoices.map((result, index) => {
                  const isFirst = index === 0;
                  const isLast = index === visibleChoices.length - 1;
                  return (
                    <ClickableBarSegment
                      key={`${result.rating}-${index}`}
                      className="relative h-full cursor-pointer transition-opacity hover:brightness-110"
                      style={{
                        width: `${result.percentage}%`,
                        borderRight: isLast ? "none" : "1px solid rgb(226, 232, 240)",
                      }}
                      onClick={() => filterByRating(result.rating)}>
                      <div
                        className={`h-full bg-slate-700 ${isFirst ? "rounded-tl-lg" : ""} ${isLast ? "rounded-tr-lg" : ""}`}
                        style={{ opacity: 0.3 + ((result.rating ?? 0) / element.range) * 0.7 }}
                      />
                    </ClickableBarSegment>
                  );
                })}
              </div>
              <div className="flex w-full overflow-hidden rounded-b-lg border border-t-0 border-slate-200 bg-slate-50">
                {visibleChoices.map((result, index) => (
                  <div
                    key={`${result.rating}-${index}`}
                    className="flex flex-col items-center justify-center py-2"
                    style={{
                      width: `${result.percentage}%`,
                      borderRight:
                        index < visibleChoices.length - 1 ? "1px solid rgb(226, 232, 240)" : "none",
                    }}>
                    <div className="mb-1 flex items-center justify-center">
                      <RatingResponse
                        scale={element.scale}
                        answer={result.rating}
                        range={element.range}
                        addColors={false}
                        variant="aggregated"
                      />
                    </div>
                    <div className="text-xs font-medium text-slate-600">
                      {convertFloatToNDecimal(result.percentage, 1)}%
                    </div>
                  </div>
                ))}
              </div>
              <RatingScaleLegend scale={element.scale} range={element.range} />
            </>
          )}
        </div>
      ) : (
        <div className="mt-4 px-4 pt-4 pb-6 md:px-6">
          <div className="space-y-5 text-sm md:text-base">
            {elementSummary.choices.map((result, index) => (
              <div key={`${result.rating}-${index}`}>
                <FilterTarget
                  className="w-full cursor-pointer hover:opacity-80"
                  onClick={() => filterByRating(result.rating)}>
                  <div className="text flex justify-between px-2 pb-2">
                    <div className="mr-8 flex items-center gap-x-1">
                      <div className="font-semibold text-slate-700">
                        <RatingResponse
                          scale={element.scale}
                          answer={result.rating}
                          range={element.range}
                          addColors={element.isColorCodingEnabled}
                          variant="individual"
                        />
                      </div>
                      <p className="rounded-lg bg-slate-100 px-2 text-slate-700">
                        {convertFloatToNDecimal(result.percentage, 2)}%
                      </p>
                    </div>
                    <p className="flex w-32 items-end justify-end text-slate-600">
                      {t("common.count_responses", { count: result.count })}
                    </p>
                  </div>
                  <ProgressBar barColor="bg-slate-700" progress={result.percentage / 100} />
                </FilterTarget>
              </div>
            ))}
          </div>
        </div>
      )}
      {elementSummary.dismissed.count > 0 && (
        <div className="rounded-b-lg border-t bg-white px-6 py-4">
          <div className="text flex justify-between px-2">
            <p className="font-semibold text-slate-700">{t("common.dismissed")}</p>
            <p className="flex w-32 items-end justify-end text-slate-600">
              {t("common.count_responses", { count: elementSummary.dismissed.count })}
            </p>
          </div>
        </div>
      )}
    </div>
  );
};

const ScaleIcon = ({ scale }: { scale: "number" | "star" | "smiley" }) => {
  if (scale === "number") return <CircleSlash2 className="size-4" />;
  if (scale === "star") return <StarIcon fill="rgb(250 204 21)" className="size-4 text-yellow-400" />;
  return <SmileIcon className="size-4" />;
};

/** forma2's RatingSummary: the overall average beside the distribution. */
export const RatingSummary = ({
  elementSummary,
  survey,
  setFilter,
}: {
  elementSummary: TSurveyElementSummaryRating;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
}) => {
  const { t } = useTranslation();
  return (
    <RatingLikeSummary
      elementSummary={elementSummary}
      survey={survey}
      setFilter={setFilter}
      additionalInfo={
        <div className="flex items-center gap-x-2 rounded-lg bg-slate-100 p-2">
          <ScaleIcon scale={elementSummary.element.scale} />
          <div>
            {t("workspace.surveys.summary.overall")}: {formatAverage(elementSummary.average)}
          </div>
        </div>
      }
    />
  );
};

/** forma2's CSATSummary: the average and the satisfied share (4 and 5). */
export const CSATSummary = ({
  elementSummary,
  survey,
  setFilter,
}: {
  elementSummary: TSurveyElementSummaryCsat;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
}) => {
  const { t } = useTranslation();
  const { satisfiedPercentage } = elementSummary.csat;
  return (
    <RatingLikeSummary
      elementSummary={elementSummary}
      survey={survey}
      setFilter={setFilter}
      additionalInfo={
        <div className="flex items-center gap-x-2">
          <div className="flex items-center gap-x-2 rounded-lg bg-slate-100 p-2">
            <ScaleIcon scale={elementSummary.element.scale} />
            <div>
              {t("workspace.surveys.summary.overall")}: {formatAverage(elementSummary.average)}
            </div>
          </div>
          <TooltipRenderer
            tooltipContent={t("workspace.surveys.summary.csat_satisfied_tooltip", {
              percentage: satisfiedPercentage,
            })}
            delayDuration={150}>
            <div className="flex items-center gap-x-2 rounded-lg bg-slate-100 p-2">
              <SatisfactionIndicator percentage={satisfiedPercentage} />
              <div>{t("workspace.surveys.summary.csat_satisfied", { percentage: satisfiedPercentage })}</div>
            </div>
          </TooltipRenderer>
        </div>
      }
    />
  );
};

/** forma2's CESSummary: the effort score out of the scale. */
export const CESSummary = ({
  elementSummary,
  survey,
  setFilter,
}: {
  elementSummary: TSurveyElementSummaryCes;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
}) => {
  const { t } = useTranslation();
  return (
    <RatingLikeSummary
      elementSummary={elementSummary}
      survey={survey}
      setFilter={setFilter}
      additionalInfo={
        <div className="flex items-center gap-x-2 rounded-lg bg-slate-100 p-2">
          <ScaleIcon scale={elementSummary.element.scale} />
          <div>
            {t("workspace.surveys.summary.effort_score")}: {formatAverage(elementSummary.average)} /{" "}
            {elementSummary.element.range}
          </div>
        </div>
      }
    />
  );
};

/**
 * A slider's answers: the mean in the header, the median and the extremes, and how the answers spread over the scale. A bin of one position filters by that value when clicked; a wider bin cannot be expressed as one filter condition and is not a button.
 */
export const SliderSummary = ({
  elementSummary,
  survey,
  setFilter,
}: {
  elementSummary: TSurveyElementSummarySlider;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
}) => {
  const { t } = useTranslation();
  const { element } = elementSummary;
  const stats = [
    { label: t("workspace.surveys.summary.slider_median"), value: elementSummary.median },
    { label: t("workspace.surveys.summary.slider_lowest"), value: elementSummary.min },
    { label: t("workspace.surveys.summary.slider_highest"), value: elementSummary.max },
  ];

  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader
        elementSummary={elementSummary}
        survey={survey}
        additionalInfo={
          <div className="flex items-center gap-x-2 rounded-lg bg-slate-100 p-2">
            <SlidersHorizontalIcon aria-hidden="true" className="size-4" />
            <div>
              {t("workspace.surveys.summary.slider_average")}:{" "}
              {elementSummary.responseCount === 0 ? "-" : formatAverage(elementSummary.average)}
            </div>
          </div>
        }
      />
      <div className="px-4 pt-2 pb-6 md:px-6">
        {elementSummary.responseCount === 0 ? (
          <EmptyState text={t("workspace.surveys.summary.no_responses_found")} variant="simple" />
        ) : (
          <>
            <dl className="mb-6 grid grid-cols-3 gap-2 text-center">
              {stats.map((stat) => (
                <div key={stat.label} className="rounded-lg bg-slate-50 p-3">
                  <dt className="text-xs text-slate-500">{stat.label}</dt>
                  <dd className="text-lg font-semibold text-slate-800">{stat.value}</dd>
                </div>
              ))}
            </dl>
            <div className="space-y-4 text-sm md:text-base">
              {elementSummary.bins.map((bin) => {
                const label =
                  bin.from === bin.to ? String(bin.from) : `${String(bin.from)} – ${String(bin.to)}`;
                const row = (
                  <>
                    <div className="flex justify-between px-2 pb-2">
                      <div className="mr-8 flex items-center gap-x-1">
                        <p className="font-semibold text-slate-700">{label}</p>
                        <p className="rounded-lg bg-slate-100 px-2 text-slate-700">
                          {convertFloatToNDecimal(bin.percentage, 2)}%
                        </p>
                      </div>
                      <p className="flex w-32 items-end justify-end text-slate-600">
                        {t("common.count_responses", { count: bin.count })}
                      </p>
                    </div>
                    <ProgressBar barColor="bg-slate-700" progress={bin.percentage / 100} />
                  </>
                );
                return bin.from === bin.to ? (
                  <FilterTarget
                    key={label}
                    className="w-full cursor-pointer text-left hover:opacity-80"
                    onClick={() =>
                      setFilter(
                        element.id,
                        element.headline,
                        element.type,
                        SUMMARY_FILTER_OPERATOR.isEqualTo,
                        String(bin.from)
                      )
                    }>
                    {row}
                  </FilterTarget>
                ) : (
                  <div key={label}>{row}</div>
                );
              })}
            </div>
            {elementSummary.dismissed.count > 0 ? (
              <p className="mt-4 px-2 text-sm text-slate-500">
                {t("workspace.surveys.summary.slider_dismissed", { count: elementSummary.dismissed.count })}
              </p>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
};

/** One labelled share with its bar, the row the CTA, consent and booking cards are made of. */
const ShareRow = ({
  title,
  percentage,
  footer,
}: {
  title: string;
  percentage: number;
  footer: ReactNode;
}) => (
  <>
    <div className="text flex justify-between px-2 pb-2">
      <div className="mr-8 flex gap-x-1">
        <p className="font-semibold text-slate-700 underline-offset-4 group-hover:underline">{title}</p>
        <p className="rounded-lg bg-slate-100 px-2 text-slate-700">
          {convertFloatToNDecimal(percentage, 2)}%
        </p>
      </div>
      <p className="flex w-32 items-end justify-end text-slate-600">{footer}</p>
    </div>
    <div className="group-hover:opacity-80">
      <ProgressBar barColor="bg-slate-700" progress={percentage / 100} />
    </div>
  </>
);

/** forma2's CTASummary: impressions, clicks, skips and the click-through rate. */
export const CTASummary = ({
  elementSummary,
  survey,
}: {
  elementSummary: TSurveyElementSummaryCta;
  survey: TSurvey;
}) => {
  const { t } = useTranslation();
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader
        survey={survey}
        elementSummary={elementSummary}
        showResponses={false}
        additionalInfo={
          <>
            <div className="flex items-center rounded-lg bg-slate-100 p-2">
              <InboxIcon className="mr-2 size-4" />
              {t("workspace.surveys.summary.count_impressions", { count: elementSummary.impressionCount })}
            </div>
            <div className="flex items-center rounded-lg bg-slate-100 p-2">
              <InboxIcon className="mr-2 size-4" />
              {t("workspace.surveys.summary.count_clicks", { count: elementSummary.clickCount })}
            </div>
            {!elementSummary.element.required && (
              <div className="flex items-center rounded-lg bg-slate-100 p-2">
                <InboxIcon className="mr-2 size-4" />
                {t("workspace.surveys.summary.count_skips", { count: elementSummary.skipCount })}
              </div>
            )}
          </>
        }
      />
      <div className="space-y-5 px-4 pt-4 pb-6 text-sm md:px-6 md:text-base">
        <ShareRow
          title="CTR"
          percentage={elementSummary.ctr.percentage}
          footer={t("workspace.surveys.summary.count_clicks", { count: elementSummary.ctr.count })}
        />
      </div>
    </div>
  );
};

/** forma2's CalSummary: booked against dismissed. */
export const CalSummary = ({
  elementSummary,
  survey,
}: {
  elementSummary: TSurveyElementSummaryCal;
  survey: TSurvey;
}) => {
  const { t } = useTranslation();
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader elementSummary={elementSummary} survey={survey} />
      <div className="space-y-5 px-4 pt-4 pb-6 text-sm md:px-6 md:text-base">
        <div>
          <ShareRow
            title={t("common.booked")}
            percentage={elementSummary.booked.percentage}
            footer={t("common.count_responses", { count: elementSummary.booked.count })}
          />
        </div>
        <div>
          <ShareRow
            title={t("common.dismissed")}
            percentage={elementSummary.skipped.percentage}
            footer={t("common.count_responses", { count: elementSummary.skipped.count })}
          />
        </div>
      </div>
    </div>
  );
};

/** forma2's ConsentSummary: accepted against dismissed, a click filtering by either. */
export const ConsentSummary = ({
  elementSummary,
  survey,
  setFilter,
}: {
  elementSummary: TSurveyElementSummaryConsent;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
}) => {
  const { t } = useTranslation();
  const items = [
    { title: t("common.accepted"), filterValue: SUMMARY_FILTER_VALUE.accepted, ...elementSummary.accepted },
    {
      title: t("common.dismissed"),
      filterValue: SUMMARY_FILTER_VALUE.dismissed,
      ...elementSummary.dismissed,
    },
  ];
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader elementSummary={elementSummary} survey={survey} />
      <div className="space-y-5 px-4 pt-4 pb-6 text-sm md:px-6 md:text-base">
        {items.map((item) => (
          <FilterTarget
            className="group w-full cursor-pointer"
            key={item.filterValue}
            onClick={() =>
              setFilter(
                elementSummary.element.id,
                elementSummary.element.headline,
                elementSummary.element.type,
                SUMMARY_FILTER_OPERATOR.is,
                item.filterValue
              )
            }>
            <ShareRow
              title={item.title}
              percentage={item.percentage}
              footer={t("common.count_responses", { count: item.count })}
            />
          </FilterTarget>
        ))}
      </div>
    </div>
  );
};
