import type { TDisplayWithContact } from "@forma/types/displays";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey, TSurveySummary } from "@forma/types/surveys/types";
import { Link } from "@tanstack/react-router";
import { AlertCircleIcon, InfoIcon, TimerIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { ProgressBar } from "../../../../components/ui/progress-bar";
import { TooltipRenderer } from "../../../../components/ui/tooltip";
import { recallToHeadline } from "../../../../lib/survey/recall";
import { timeSince } from "../../../../lib/time";
import { getElementTypes } from "../../../survey-editor/lib/elements";
import { formatTextWithSlashes } from "../../../survey-editor/lib/utils";

/** forma2's SummaryDropOffs: per element, its time to complete, impressions and drop-offs. */
export const SummaryDropOffs = ({
  dropOff,
  survey,
}: {
  dropOff: TSurveySummary["dropOff"];
  survey: TSurvey;
}) => {
  const { t } = useTranslation();
  const elementTypes = getElementTypes(t);
  const ElementIcon = ({ type }: { type: TSurveyElementTypeEnum }) => {
    const Icon = elementTypes.find((elementType) => elementType.id === type)?.icon;
    return Icon ? <Icon className="mt-[3px] size-5 shrink-0 text-slate-600" /> : null;
  };

  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <div className="grid min-h-10 grid-cols-6 items-center rounded-t-xl border-b border-slate-200 bg-slate-100 text-sm font-semibold text-slate-600">
        <div className="col-span-3 px-4 md:px-6">{t("common.questions")}</div>
        <div className="flex justify-end px-4 md:px-6">
          <TooltipRenderer
            tooltipContent={
              <p className="text-center font-normal">{t("workspace.surveys.summary.ttc_tooltip")}</p>
            }>
            <TimerIcon className="size-5" aria-label={t("workspace.surveys.summary.ttc_tooltip")} />
          </TooltipRenderer>
        </div>
        <div className="px-4 text-right md:px-6">{t("workspace.surveys.summary.impressions")}</div>
        <div className="px-4 text-right md:mr-1 md:pr-6 md:pl-6">
          {t("workspace.surveys.summary.drop_offs")}
        </div>
      </div>
      {dropOff.map((entry) => (
        <div
          key={entry.elementId}
          className="grid grid-cols-6 items-start border-b border-slate-100 text-xs text-slate-800 md:text-sm">
          <div className="col-span-3 flex gap-3 px-4 py-2 md:px-6">
            <ElementIcon type={entry.elementType} />
            <p>
              {formatTextWithSlashes(
                recallToHeadline({ default: entry.headline }, survey, true, "default").default ?? "",
                "@",
                ["text-sm"]
              )}
            </p>
          </div>
          <div className="px-4 py-2 text-right font-mono font-medium whitespace-pre-wrap md:px-6">
            {entry.ttc > 0 ? `${(entry.ttc / 1000).toFixed(2)}s` : "N/A"}
          </div>
          <div className="px-4 py-2 text-right font-mono font-medium whitespace-pre-wrap md:px-6">
            {entry.impressions}
          </div>
          <div className="px-4 py-2 text-right md:px-6">
            <span className="mr-1 inline-block w-fit rounded-xl bg-slate-100 px-2 py-1 text-left text-xs">
              {Math.round(entry.dropOffPercentage)}%
            </span>
            <span className="mr-1 font-mono font-medium">{entry.dropOffCount}</span>
          </div>
        </div>
      ))}
    </div>
  );
};

const displayContactIdentifier = (display: TDisplayWithContact): string => {
  if (!display.contact) return "";
  return display.contact.attributes?.email || display.contact.attributes?.userId || display.contact.id;
};

/** forma2's SummaryImpressions: the identified contacts the survey was shown to, newest first. */
export const SummaryImpressions = ({
  displays,
  workspaceId,
  isLoading,
  hasMore,
  displaysError,
  locale,
  onLoadMore,
  onRetry,
}: {
  displays: TDisplayWithContact[];
  workspaceId: string;
  isLoading: boolean;
  hasMore: boolean;
  displaysError: string | null;
  locale: string;
  onLoadMore: () => void;
  onRetry: () => void;
}) => {
  const { t } = useTranslation();

  if (isLoading) {
    return (
      <div className="rounded-xl border border-slate-200 bg-white p-8 shadow-xs">
        <div className="flex items-center justify-center">
          <div className="h-6 w-32 animate-pulse rounded-full bg-slate-200" />
        </div>
      </div>
    );
  }

  const renderContent = () => {
    if (displaysError) {
      return (
        <div className="p-8">
          <div className="flex flex-col items-center gap-4 text-center">
            <div className="flex items-center gap-2 text-red-600">
              <AlertCircleIcon className="size-5" />
              <span className="text-sm font-medium">{t("common.error_loading_data")}</span>
            </div>
            <p className="text-sm text-slate-500">{displaysError}</p>
            <Button onClick={onRetry} variant="secondary" size="sm">
              {t("common.try_again")}
            </Button>
          </div>
        </div>
      );
    }
    if (displays.length === 0) {
      return (
        <div className="p-8 text-center text-sm text-slate-500">
          {t("workspace.surveys.summary.no_identified_impressions")}
        </div>
      );
    }
    return (
      <>
        <div className="grid min-h-10 grid-cols-4 items-center border-b border-slate-200 bg-slate-100 text-sm font-semibold text-slate-600">
          <div className="col-span-2 px-4 md:px-6">{t("common.user")}</div>
          <div className="col-span-2 px-4 md:px-6">{t("workspace.contacts.survey_viewed_at")}</div>
        </div>
        <div className="max-h-[62vh] overflow-y-auto">
          {displays.map((display) => (
            <div
              key={display.id}
              className="grid grid-cols-4 items-center border-b border-slate-100 py-2 text-xs text-slate-800 last:border-transparent md:text-sm">
              <div className="col-span-2 pl-4 md:pl-6">
                {display.contact ? (
                  <Link
                    className="ph-no-capture break-all text-slate-600 hover:underline"
                    to="/workspaces/$workspaceId/contacts/$contactId"
                    params={{ workspaceId, contactId: display.contact.id }}>
                    {displayContactIdentifier(display)}
                  </Link>
                ) : (
                  <span className="break-all text-slate-600">{t("common.anonymous")}</span>
                )}
              </div>
              <div className="col-span-2 px-4 text-slate-500 md:px-6">
                {timeSince(display.createdAt.toISOString(), locale)}
              </div>
            </div>
          ))}
        </div>
        {hasMore && (
          <div className="flex justify-center border-t border-slate-100 py-4">
            <Button onClick={onLoadMore} variant="secondary" size="sm">
              {t("common.load_more")}
            </Button>
          </div>
        )}
      </>
    );
  };

  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <div className="flex items-center gap-2 rounded-t-xl border-b border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-600">
        <InfoIcon className="size-4 shrink-0" />
        <span>{t("workspace.surveys.summary.impressions_identified_only")}</span>
      </div>
      {renderContent()}
    </div>
  );
};

/** forma2's QuotasSummary: each quota's progress toward its limit. */
export const QuotasSummary = ({ quotas }: { quotas: TSurveySummary["quotas"] }) => {
  const { t } = useTranslation();
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
      <div className="grid min-h-10 grid-cols-6 items-center rounded-t-xl border-b border-slate-200 bg-slate-100 text-sm font-semibold text-slate-500">
        <div className="px-2">{t("common.progress")}</div>
        <div className="col-span-3 px-2">{t("common.label")}</div>
        <div className="px-2 text-right">{t("workspace.surveys.summary.limit")}</div>
        <div className="px-2 text-right md:mr-1 md:pl-6">{t("workspace.surveys.summary.current_count")}</div>
      </div>
      {quotas.length > 0 ? (
        quotas.map((quota) => (
          <div
            key={quota.id}
            className="grid h-[52px] grid-cols-6 border-b border-slate-100 text-xs text-slate-900 md:text-sm">
            <div className="col-span-1 flex h-full items-center justify-center p-2">
              <ProgressBar progress={quota.percentage / 100} barColor="bg-brand-dark" height={2} />
            </div>
            <div className="col-span-3 flex items-center p-2 whitespace-pre-wrap">{quota.name}</div>
            <div className="flex items-center justify-end p-2 whitespace-pre-wrap">{quota.limit}</div>
            <div className="flex items-center justify-end gap-2 p-2 text-right">
              <span className="rounded-xl bg-slate-100 px-2 py-1 text-xs">{quota.percentage}%</span>
              <span>{quota.count}</span>
            </div>
          </div>
        ))
      ) : (
        <div className="grid h-[52px] border-b border-slate-100 text-xs text-slate-900 md:text-sm">
          <div className="flex items-center justify-center p-2">{t("common.no_quotas_found")}</div>
        </div>
      )}
    </div>
  );
};
