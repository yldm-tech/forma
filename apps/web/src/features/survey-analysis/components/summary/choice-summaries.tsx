import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type {
  TSurvey,
  TSurveyElementSummaryMatrix,
  TSurveyElementSummaryMultipleChoice,
  TSurveyElementSummaryPictureSelection,
  TSurveyElementSummaryRanking,
} from "@forma/types/surveys/types";
import { Link } from "@tanstack/react-router";
import { InboxIcon } from "lucide-react";
import { Fragment, type KeyboardEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "../../../../components/ui/avatar";
import { Button } from "../../../../components/ui/button";
import { IdBadge } from "../../../../components/ui/id-badge";
import { ProgressBar } from "../../../../components/ui/progress-bar";
import { TooltipRenderer } from "../../../../components/ui/tooltip";
import { getChoiceIdByValue, getContactIdentifier } from "../../lib/responses";
import { convertFloatToNDecimal } from "../../lib/summary";
import { ElementSummaryHeader, SampleTruncationNotice, type SetSummaryFilter } from "./summary-parts";

type ChoiceSummaryResult = TSurveyElementSummaryMultipleChoice["choices"][number];

/** forma2's MultipleChoiceSummary: each choice's share, Other's typed values, a click filtering by the choice. */
export const MultipleChoiceSummary = ({
  elementSummary,
  survey,
  setFilter,
}: {
  elementSummary: TSurveyElementSummaryMultipleChoice;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
}) => {
  const { t } = useTranslation();
  const [visibleOtherResponses, setVisibleOtherResponses] = useState(10);
  const otherValue = elementSummary.element.choices.find((choice) => choice.id === "other")?.label.default;
  // Choices with typed Other values go last; the rest by count.
  const results = [...elementSummary.choices].sort((a, b) => {
    const aHasOthers = (a.others?.length ?? 0) > 0;
    const bHasOthers = (b.others?.length ?? 0) > 0;
    if (aHasOthers && !bHasOthers) return 1;
    if (!aHasOthers && bHasOthers) return -1;
    return b.count - a.count;
  });

  const applyChoiceFilter = (result: ChoiceSummaryResult) => {
    setFilter(
      elementSummary.element.id,
      elementSummary.element.headline,
      elementSummary.element.type,
      elementSummary.type === TSurveyElementTypeEnum.MultipleChoiceSingle || otherValue === result.value
        ? t("workspace.surveys.summary.includes_either")
        : t("workspace.surveys.summary.includes_all"),
      [result.value]
    );
  };

  const handleChoiceKeyDown = (event: KeyboardEvent<HTMLDivElement>, result: ChoiceSummaryResult) => {
    if (event.target !== event.currentTarget || (event.key !== "Enter" && event.key !== " ")) return;
    event.preventDefault();
    applyChoiceFilter(result);
  };

  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader
        elementSummary={elementSummary}
        survey={survey}
        additionalInfo={
          elementSummary.type === TSurveyElementTypeEnum.MultipleChoiceMulti ? (
            <div className="flex items-center rounded-lg bg-slate-100 p-2">
              <InboxIcon className="mr-2 size-4" />
              {t("common.count_selections", { count: elementSummary.selectionCount })}
            </div>
          ) : undefined
        }
      />
      <div className="px-4 pt-4 pb-6 text-sm md:px-6 md:text-base">
        <div className="space-y-5">
          {results.map((result) => {
            const choiceId = getChoiceIdByValue(result.value, elementSummary.element);
            const others = (result.others ?? []).filter((other) => other.value !== "");
            return (
              <Fragment key={result.value}>
                <div
                  role="button"
                  tabIndex={0}
                  className="group w-full cursor-pointer"
                  onClick={() => applyChoiceFilter(result)}
                  onKeyDown={(event) => handleChoiceKeyDown(event, result)}>
                  <div className="text flex flex-col justify-between px-2 pb-2 sm:flex-row">
                    <div className="mr-8 flex w-full justify-between gap-x-2 sm:justify-normal">
                      <p className="font-semibold text-slate-700 underline-offset-4 group-hover:underline">
                        {result.value}
                      </p>
                      {choiceId && <IdBadge id={choiceId} />}
                    </div>
                    <div className="flex w-full gap-x-2">
                      <p className="flex w-full pt-1 text-slate-600 sm:items-end sm:justify-end sm:pt-0">
                        {t("common.count_selections", { count: result.count })}
                      </p>
                      <p className="rounded-lg bg-slate-100 px-2 text-slate-700">
                        {convertFloatToNDecimal(result.percentage, 2)}%
                      </p>
                    </div>
                  </div>
                  <div className="group-hover:opacity-80">
                    <ProgressBar barColor="bg-slate-700" progress={result.percentage / 100} />
                  </div>
                </div>
                {result.others && result.others.length > 0 && (
                  <div className="mt-4 rounded-lg border border-slate-200">
                    <div className="grid h-12 grid-cols-2 content-center rounded-t-lg bg-slate-100 text-left text-sm font-semibold text-slate-900">
                      <div className="col-span-1 pl-6">
                        {t("workspace.surveys.summary.other_values_found")}
                      </div>
                      <div className="col-span-1 pl-6">{survey.type === "app" && t("common.user")}</div>
                    </div>
                    {others.slice(0, visibleOtherResponses).map((other, idx) => (
                      // Typed values repeat; the position tells the rows apart.
                      // oxlint-disable-next-line react/no-array-index-key -- see above
                      <div key={`${idx}-${String(other.value)}`} dir="auto">
                        {survey.type === "link" && (
                          <div className="ph-no-capture col-span-1 m-2 flex h-10 items-center rounded-lg pl-4 text-sm font-medium text-slate-900">
                            <span>{other.value}</span>
                          </div>
                        )}
                        {survey.type === "app" && other.contact && (
                          <Link
                            to="/workspaces/$workspaceId/contacts/$contactId"
                            params={{ workspaceId: survey.workspaceId, contactId: other.contact.id }}
                            className="m-2 grid h-16 grid-cols-2 items-center rounded-lg text-sm hover:bg-slate-100">
                            <div className="ph-no-capture col-span-1 pl-4 font-medium text-slate-900">
                              <span>{other.value}</span>
                            </div>
                            <div className="ph-no-capture col-span-1 flex items-center gap-x-4 pl-6 font-medium text-slate-900">
                              <PersonAvatar personId={other.contact.id} />
                              <span>{getContactIdentifier(other.contact, other.contactAttributes)}</span>
                            </div>
                          </Link>
                        )}
                      </div>
                    ))}
                    {visibleOtherResponses < result.others.length ? (
                      <div className="flex justify-center py-4">
                        <Button
                          onClick={(e) => {
                            e.stopPropagation();
                            setVisibleOtherResponses((current) =>
                              Math.min(current + 10, result.others?.length ?? 0)
                            );
                          }}
                          variant="secondary"
                          size="sm">
                          {t("common.load_more")}
                        </Button>
                      </div>
                    ) : (
                      <SampleTruncationNotice
                        sampleCount={result.others.length}
                        responseCount={result.count}
                      />
                    )}
                  </div>
                )}
              </Fragment>
            );
          })}
        </div>
      </div>
    </div>
  );
};

/** forma2's PictureChoiceSummary: each picture's share, a click filtering by it. */
export const PictureChoiceSummary = ({
  elementSummary,
  survey,
  setFilter,
}: {
  elementSummary: TSurveyElementSummaryPictureSelection;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
}) => {
  const { t } = useTranslation();
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader
        elementSummary={elementSummary}
        survey={survey}
        additionalInfo={
          elementSummary.element.allowMulti ? (
            <div className="flex items-center rounded-lg bg-slate-100 p-2">
              <InboxIcon className="mr-2 size-4" />
              {t("common.count_selections", { count: elementSummary.selectionCount })}
            </div>
          ) : undefined
        }
      />
      <div className="space-y-5 px-4 pt-4 pb-6 text-sm md:px-6 md:text-base">
        {elementSummary.choices.map((result, index) => {
          const choiceId = getChoiceIdByValue(result.imageUrl, elementSummary.element);
          return (
            <button
              type="button"
              className="w-full cursor-pointer hover:opacity-80"
              key={result.id}
              onClick={() =>
                setFilter(
                  elementSummary.element.id,
                  elementSummary.element.headline,
                  elementSummary.element.type,
                  t("workspace.surveys.summary.includes_all"),
                  [t("workspace.surveys.edit.picture_idx", { idx: index + 1 })]
                )
              }>
              <div className="text flex flex-col justify-between px-2 pb-2 sm:flex-row">
                <div className="mr-8 flex w-full justify-between gap-x-2 sm:justify-normal">
                  <img src={result.imageUrl} alt="" className="h-32 w-[220px] rounded-md object-cover" />
                  <div className="self-end">{choiceId && <IdBadge id={choiceId} />}</div>
                </div>
                <div className="flex w-full gap-x-2">
                  <p className="flex w-full pt-1 text-slate-600 sm:items-end sm:justify-end sm:pt-0">
                    {t("common.count_selections", { count: result.count })}
                  </p>
                  <p className="self-end rounded-lg bg-slate-100 px-2 text-slate-700">
                    {convertFloatToNDecimal(result.percentage, 2)}%
                  </p>
                </div>
              </div>
              <ProgressBar barColor="bg-slate-700" progress={result.percentage / 100 || 0} />
            </button>
          );
        })}
      </div>
    </div>
  );
};

/** forma2's RankingSummary: the choices by their average rank. */
export const RankingSummary = ({
  elementSummary,
  survey,
}: {
  elementSummary: TSurveyElementSummaryRanking;
  survey: TSurvey;
}) => {
  const { t } = useTranslation();
  const results = [...elementSummary.choices].sort((a, b) => a.avgRanking - b.avgRanking);
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader elementSummary={elementSummary} survey={survey} />
      <div className="space-y-5 px-4 pt-4 pb-6 text-sm md:px-6 md:text-base">
        {results.map((result, index) => {
          const choiceId = getChoiceIdByValue(result.value, elementSummary.element);
          return (
            <div key={result.value}>
              <div className="text flex flex-col justify-between px-2 pb-2 sm:flex-row">
                <div className="mr-8 flex w-full items-center gap-x-2">
                  <span className="mr-2 text-slate-400">#{index + 1}</span>
                  <div className="rounded-sm bg-slate-100 px-2 py-1">{result.value}</div>
                  {choiceId && <IdBadge id={choiceId} />}
                  <span className="ml-auto font-semibold text-slate-600">
                    {t("workspace.surveys.summary.avg_rank", {
                      value: convertFloatToNDecimal(result.avgRanking, 2),
                    })}
                  </span>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

/** forma2's MatrixElementSummary: each row's share of each column, a cell click filtering by it. */
export const MatrixElementSummary = ({
  elementSummary,
  survey,
  setFilter,
}: {
  elementSummary: TSurveyElementSummaryMatrix;
  survey: TSurvey;
  setFilter: SetSummaryFilter;
}) => {
  const { t } = useTranslation();
  const getOpacityLevel = (percentage: number): string => ((percentage * 0.75 + 15) / 100).toFixed(2);
  const columns = elementSummary.data[0]?.columnPercentages.map((c) => c.column) ?? [];

  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <ElementSummaryHeader elementSummary={elementSummary} survey={survey} />
      <div className="overflow-x-auto p-6">
        <table className="mx-auto border-collapse cursor-default text-left">
          <thead>
            <tr>
              <th className="p-4 pt-0 pb-3 font-medium text-slate-400" />
              {columns.map((column) => (
                <th key={column} className="text-center font-medium">
                  <TooltipRenderer tooltipContent={column}>
                    <p className="max-w-40 overflow-hidden text-ellipsis whitespace-nowrap">{column}</p>
                  </TooltipRenderer>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {elementSummary.data.map(({ rowLabel, columnPercentages, totalResponsesForRow }) => (
              <tr key={rowLabel}>
                <td className="max-w-60 overflow-hidden p-4 text-ellipsis whitespace-nowrap">
                  <TooltipRenderer tooltipContent={rowLabel}>
                    <p className="max-w-40 overflow-hidden text-ellipsis whitespace-nowrap">{rowLabel}</p>
                  </TooltipRenderer>
                </td>
                {columnPercentages.map(({ column, percentage }) => (
                  <td key={column} className="text-center text-slate-500">
                    <TooltipRenderer
                      tooltipContent={t("common.count_responses", {
                        count: Math.round((percentage / 100) * totalResponsesForRow),
                      })}>
                      <button
                        type="button"
                        style={{ backgroundColor: `rgba(148,163,184,${getOpacityLevel(percentage)})` }}
                        className="hover:outline-ink m-1 flex h-full w-40 cursor-pointer items-center justify-center rounded-sm p-4 text-sm text-slate-950 hover:outline-solid"
                        onClick={() =>
                          setFilter(
                            elementSummary.element.id,
                            elementSummary.element.headline,
                            elementSummary.element.type,
                            rowLabel,
                            column
                          )
                        }>
                        {percentage}
                      </button>
                    </TooltipRenderer>
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
};
