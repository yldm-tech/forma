import type { TResponseWithQuotas } from "@forma/types/responses";
import type { TTag } from "@forma/types/tags";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowDownUpIcon, EyeIcon, MessageSquareTextIcon, TrashIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { EmptyState } from "../../components/ui/empty-state";
import { LoadingSpinner } from "../../components/ui/loading-spinner";
import { TooltipRenderer } from "../../components/ui/tooltip";
import {
  type ContactActivity,
  type ContactDisplay,
  type SurveyRef,
  contactKeys,
} from "../../lib/queries/contacts";
import { replaceHeadlineRecall } from "../../lib/survey/recall";
import { timeSince } from "../../lib/time";
import { analysisSurveyQuery, invalidateSurveyAnalysis } from "../survey-analysis/api";
import { SingleResponseCard } from "../survey-analysis/components/response-card/single-response-card";

type TimelineItem =
  | { type: "display"; data: ContactDisplay; survey: SurveyRef }
  | { type: "response"; data: TResponseWithQuotas; survey: SurveyRef };

/**
 * forma2's ActivityTimeline: the contact's displays and responses, newest first or, toggled, oldest first. A display says which survey was viewed; a response shows the whole response card. Activity on a survey that no longer exists is left out.
 */
export const ActivityTimeline = ({
  workspaceId,
  contactId,
  activity,
  environmentTags,
  locale,
  isReadOnly,
}: {
  workspaceId: string;
  contactId: string;
  activity: ContactActivity;
  environmentTags: TTag[];
  locale: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const [isReversed, setIsReversed] = useState(false);

  const items = useMemo(() => {
    const surveyById = new Map(activity.surveys.map((survey) => [survey.id, survey]));
    const displays: TimelineItem[] = activity.displays.flatMap((display) => {
      const survey = surveyById.get(display.surveyId);
      return survey ? [{ type: "display" as const, data: display, survey }] : [];
    });
    const responses: TimelineItem[] = activity.responses.flatMap((response) => {
      const survey = surveyById.get(response.surveyId);
      return survey ? [{ type: "response" as const, data: response, survey }] : [];
    });
    const merged = [...displays, ...responses].sort(
      (a, b) => b.data.createdAt.getTime() - a.data.createdAt.getTime()
    );
    return isReversed ? merged.reverse() : merged;
  }, [activity, isReversed]);

  return (
    <div className="col-span-3">
      <div className="flex items-center justify-between pb-6">
        <h2 className="text-lg font-bold text-slate-700">{t("common.activity")}</h2>
        <button
          type="button"
          onClick={() => setIsReversed((reversed) => !reversed)}
          aria-label={t("common.activity")}
          aria-pressed={isReversed}
          className="hover:text-brand-dark flex items-center px-1 text-slate-800">
          <ArrowDownUpIcon className="inline size-4" />
        </button>
      </div>
      {items.length === 0 ? (
        <EmptyState text={t("workspace.contacts.no_activity_yet")} />
      ) : (
        <div className="space-y-4">
          {items.map((item) =>
            item.type === "display" ? (
              <DisplayCard
                key={`display-${item.data.id}`}
                display={item.data}
                survey={item.survey}
                workspaceId={workspaceId}
                locale={locale}
              />
            ) : (
              <ResponseSurveyCard
                key={`response-${item.data.id}`}
                response={item.data}
                survey={item.survey}
                workspaceId={workspaceId}
                contactId={contactId}
                environmentTags={environmentTags}
                locale={locale}
                isReadOnly={isReadOnly}
              />
            )
          )}
        </div>
      )}
    </div>
  );
};

const SurveyLink = ({ workspaceId, survey }: { workspaceId: string; survey: SurveyRef }) => (
  <Link
    to="/workspaces/$workspaceId/surveys/$surveyId/summary"
    params={{ workspaceId, surveyId: survey.id }}
    className="block truncate text-sm font-medium text-slate-700 hover:underline">
    {survey.name}
  </Link>
);

/** forma2's DisplayCard: a survey the contact was shown. */
export const DisplayCard = ({
  display,
  survey,
  workspaceId,
  locale,
}: {
  display: ContactDisplay;
  survey: SurveyRef;
  workspaceId: string;
  locale: string;
}) => {
  const { t } = useTranslation();
  return (
    <div className="flex items-center justify-between rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
      <div className="flex items-center gap-3">
        <div className="flex size-8 items-center justify-center rounded-full bg-slate-100">
          <EyeIcon className="size-4 text-slate-600" />
        </div>
        <div>
          <p className="text-xs text-slate-500">{t("workspace.contacts.survey_viewed")}</p>
          <SurveyLink workspaceId={workspaceId} survey={survey} />
        </div>
      </div>
      <time className="text-sm text-slate-500" dateTime={display.createdAt.toISOString()}>
        {timeSince(display.createdAt.toISOString(), locale)}
      </time>
    </div>
  );
};

/** forma2's ResponseSurveyCard: the survey analysis' response card under a header naming the survey the contact answered. */
export const ResponseSurveyCard = ({
  response,
  survey,
  workspaceId,
  contactId,
  environmentTags,
  locale,
  isReadOnly,
}: {
  response: TResponseWithQuotas;
  survey: SurveyRef;
  workspaceId: string;
  contactId: string;
  environmentTags: TTag[];
  locale: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const fullSurvey = useQuery(analysisSurveyQuery(survey.id));
  const surveyWithRecall = useMemo(
    () => (fullSurvey.data ? replaceHeadlineRecall(fullSurvey.data, "default") : undefined),
    [fullSurvey.data]
  );
  const deleteLabel = t("workspace.surveys.responses.delete_response");
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: contactKeys.activity(workspaceId, contactId) });

  if (!surveyWithRecall) {
    return (
      <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
        <LoadingSpinner />
      </div>
    );
  }
  return (
    <SingleResponseCard
      survey={surveyWithRecall}
      response={response}
      environmentTags={environmentTags}
      isReadOnly={isReadOnly}
      locale={locale}
      onResponseChanged={() => void refresh()}
      onResponsesDeleted={() => {
        void refresh();
        void invalidateSurveyAnalysis(queryClient, survey.id);
      }}
      renderHeader={({ onDeleteClick, canResponseBeDeleted }) => (
        <div className="flex items-center justify-between p-4">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-slate-100">
              <MessageSquareTextIcon className="size-4 text-slate-600" />
            </div>
            <div className="min-w-0">
              <p className="text-xs text-slate-500">{t("workspace.contacts.survey_response_created")}</p>
              <SurveyLink workspaceId={workspaceId} survey={survey} />
            </div>
          </div>
          <div className="flex items-center gap-1 text-sm text-slate-500">
            <time className="px-1" dateTime={response.createdAt.toISOString()}>
              {timeSince(response.createdAt.toISOString(), locale)}
            </time>
            {!isReadOnly &&
              (canResponseBeDeleted ? (
                <Button variant="ghost" size="icon" onClick={onDeleteClick} aria-label={deleteLabel}>
                  <TrashIcon className="size-4" />
                </Button>
              ) : (
                <TooltipRenderer
                  tooltipContent={t("workspace.surveys.responses.this_response_is_in_progress")}>
                  <Button
                    variant="ghost"
                    size="icon"
                    disabled
                    className="text-slate-400"
                    aria-label={deleteLabel}>
                    <TrashIcon className="size-4" />
                  </Button>
                </TooltipRenderer>
              ))}
          </div>
        </div>
      )}
    />
  );
};
