import type { TSurvey } from "@forma/types/surveys/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCcwIcon } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import {
  analysisKeys,
  invalidateSurveyAnalysis,
  surveyLiveResponseCountQuery,
  surveyResponseCountQuery,
} from "../api";

/**
 * How many responses a survey in progress received since its analysis was loaded, 0 otherwise. loadedCount is the unfiltered count read with the page; the live count is polled against it, so the charts are not redrawn under the reader: they move only when the reader asks.
 *
 * The page's own data can be read again without that refresh (another filter, a tab whose data was not cached); the responses that read brings in are then on screen, so loadedCount is read again after it, and they are no longer offered as new.
 */
export const useNewResponseCount = (survey: TSurvey, loadedCount: number | undefined): number => {
  const queryClient = useQueryClient();
  const isCollecting = survey.status === "inProgress";
  const live = useQuery({
    ...surveyLiveResponseCountQuery(survey.id),
    enabled: isCollecting && loadedCount !== undefined,
  });

  useEffect(() => {
    if (!isCollecting) return;
    const pagePrefix = analysisKeys.all(survey.id);
    return queryClient.getQueryCache().subscribe((event) => {
      if (event.type !== "updated" || event.action.type !== "success" || event.action.manual) return;
      const key = event.query.queryKey;
      const isPageData =
        pagePrefix.every((part, i) => key[i] === part) &&
        (key[pagePrefix.length] === "summary" || key[pagePrefix.length] === "responses");
      // A further page of the response list does not bring in the responses that arrived since: they are at its top.
      if (!isPageData || event.query.state.fetchMeta?.fetchMore) return;
      void queryClient.invalidateQueries({
        queryKey: surveyResponseCountQuery(survey.id, {}).queryKey,
        exact: true,
      });
    });
  }, [queryClient, survey.id, isCollecting]);

  if (!isCollecting || live.data === undefined || loadedCount === undefined) return 0;
  return Math.max(0, live.data - loadedCount);
};

/** The "2 new responses · Refresh results" pill over the summary and responses of a survey in progress, which reloads the analysis when clicked. It stays at the top of the view while the page scrolls, and is announced when it appears. */
export const NewResponsesNotice = ({
  survey,
  loadedCount,
}: {
  survey: TSurvey;
  loadedCount: number | undefined;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const newCount = useNewResponseCount(survey, loadedCount);
  return (
    // The live region stays in the page so the pill is announced when it appears. It takes no room, not even the gap its parent puts after each child: the pill floats over the content below it, so appearing never moves the charts under the reader.
    <div role="status" className="pointer-events-none sticky top-2 z-20 mb-0 h-0">
      {newCount > 0 && (
        <div className="absolute inset-x-0 top-0 flex justify-center">
          <Button
            variant="outline"
            size="sm"
            className="pointer-events-auto rounded-full shadow-md"
            onClick={() => void invalidateSurveyAnalysis(queryClient, survey.id)}>
            <RefreshCcwIcon />
            {t("workspace.analysis.new_responses_pill", { count: newCount })}
            <span aria-hidden>·</span>
            {t("workspace.analysis.refresh")}
          </Button>
        </div>
      )}
    </div>
  );
};
