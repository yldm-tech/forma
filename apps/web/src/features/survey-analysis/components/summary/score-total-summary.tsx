import type { TSurveyScoreSummary } from "@forma/types/surveys/types";
import { InboxIcon, TrophyIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { EmptyState } from "../../../../components/ui/empty-state";
import { ProgressBar } from "../../../../components/ui/progress-bar";
import { convertFloatToNDecimal, formatAverage } from "../../lib/summary";

/** The label of a bin of the score spread: the score itself, or the range a grouped bin covers. */
export const scoreBinLabel = (bin: TSurveyScoreSummary["bins"][number]): string =>
  bin.from === bin.to ? String(bin.from) : `${String(bin.from)} – ${String(bin.to)}`;

/** The total score card: the score the responses under the current filter reached, from the survey's total score variable, with its mean, median, extremes and spread. */
export const ScoreTotalSummary = ({ score }: { score: TSurveyScoreSummary }) => {
  const { t } = useTranslation();
  const stats = [
    { label: t("workspace.surveys.summary.slider_median"), value: score.median },
    { label: t("workspace.surveys.summary.slider_lowest"), value: score.min },
    { label: t("workspace.surveys.summary.slider_highest"), value: score.max },
  ];

  return (
    <section
      aria-labelledby="score-total-summary-title"
      className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <div className="space-y-2 px-4 pt-6 pb-5 md:px-6">
        <h3 id="score-total-summary-title" className="pb-1 text-lg font-semibold text-slate-900 md:text-xl">
          {t("workspace.surveys.summary.score_title")}
        </h3>
        <p className="text-sm text-slate-600">
          {t("workspace.surveys.summary.score_description", { name: score.name })}
        </p>
        <div className="flex flex-wrap gap-2 text-xs font-semibold text-slate-600 md:text-sm">
          <div className="flex items-center rounded-lg bg-slate-100 p-2">
            <InboxIcon aria-hidden="true" className="mr-2 size-4" />
            {t("common.count_responses", { count: score.responseCount })}
          </div>
          <div className="flex items-center gap-x-2 rounded-lg bg-slate-100 p-2">
            <TrophyIcon aria-hidden="true" className="size-4" />
            <div>
              {t("workspace.surveys.summary.slider_average")}:{" "}
              {score.responseCount === 0 ? "-" : formatAverage(score.average)}
            </div>
          </div>
        </div>
      </div>
      <div className="px-4 pt-2 pb-6 md:px-6">
        {score.responseCount === 0 ? (
          <EmptyState text={t("workspace.surveys.summary.score_no_scored_responses")} variant="simple" />
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
            <ul className="space-y-4 text-sm md:text-base">
              {score.bins.map((bin) => (
                <li key={scoreBinLabel(bin)}>
                  <div className="flex justify-between px-2 pb-2">
                    <div className="mr-8 flex items-center gap-x-1">
                      <p className="font-semibold text-slate-700">{scoreBinLabel(bin)}</p>
                      <p className="rounded-lg bg-slate-100 px-2 text-slate-700">
                        {convertFloatToNDecimal(bin.percentage, 2)}%
                      </p>
                    </div>
                    <p className="flex w-32 items-end justify-end text-slate-600">
                      {t("common.count_responses", { count: bin.count })}
                    </p>
                  </div>
                  <ProgressBar barColor="bg-slate-700" progress={bin.percentage / 100} />
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </section>
  );
};
