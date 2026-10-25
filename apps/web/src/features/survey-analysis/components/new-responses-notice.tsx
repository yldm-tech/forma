import type { TSurvey } from "@forma/types/surveys/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCcwIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { cn } from "../../../lib/cn";
import { invalidateSurveyAnalysis, surveyLiveResponseCountQuery } from "../api";

/**
 * How many responses a survey in progress received since its analysis was loaded, 0 otherwise. loadedCount is the unfiltered count read with the page; the live count is polled against it, so the charts are not redrawn under the reader: they move only when the reader asks.
 */
export const useNewResponseCount = (survey: TSurvey, loadedCount: number | undefined): number => {
  const isCollecting = survey.status === "inProgress";
  const live = useQuery({
    ...surveyLiveResponseCountQuery(survey.id),
    enabled: isCollecting && loadedCount !== undefined,
  });
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
    // The live region stays in the page so the pill is announced when it appears; empty, it takes no room, not even the gap its parent puts after each child.
    <div
      role="status"
      className={cn("pointer-events-none sticky top-2 z-20 flex justify-center", newCount === 0 && "mb-0")}>
      {newCount > 0 && (
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
      )}
    </div>
  );
};
