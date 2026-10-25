import type { TResponseWithQuotas } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import { type InfiniteData, useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocation, useRouter } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { errorMessage } from "../../../../lib/errors";
import { tagsQuery } from "../../../../lib/queries/tags";
import { replaceHeadlineRecall } from "../../../../lib/survey/recall";
import { quotasQuery } from "../../../survey-editor/api";
import {
  analysisApi,
  analysisKeys,
  invalidateSurveyAnalysis,
  surveyResponseCountQuery,
  surveyResponsesQuery,
  toResponseTag,
} from "../../api";
import { hasActiveFilters } from "../../lib/filter-state";
import { AnalysisEmptyState } from "../analysis-empty-state";
import { CustomFilter } from "../filter/custom-filter";
import { useResponseFilter } from "../filter/response-filter-context";
import { ResponseKeywordSearch, hasSearchableAnswers } from "../filter/response-keyword-search";
import { ResponseTable } from "./response-table";

/**
 * forma2's ResponsePage: the filter bar over the response table, 25 responses a page under the current filter, newest first, with how many of the matching responses are loaded. A deleted response leaves the loaded pages at once, and the pages are then read again; a tag change reads the response again. With nothing to show it says why: no response yet (share the survey) or none matching the filter (clear it).
 */
export const ResponsePage = ({
  survey,
  locale,
  isReadOnly,
}: {
  survey: TSurvey;
  locale: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const router = useRouter();
  const location = useLocation();
  const { filterCriteria, selectedFilter, dateRange, resetState } = useResponseFilter();
  const isFiltered = hasActiveFilters(selectedFilter, dateRange);
  const responsesQuery = surveyResponsesQuery(survey.id, filterCriteria);
  const responses = useInfiniteQuery(responsesQuery);
  // The count under the current filter; the delete and tag handlers below invalidate it with the other analysis reads.
  const matchingCount = useQuery(surveyResponseCountQuery(survey.id, filterCriteria));
  const tags = useQuery(tagsQuery(survey.workspaceId));
  const quotas = useQuery(quotasQuery(survey.id));
  const surveyWithRecall = useMemo(() => replaceHeadlineRecall(survey, "default"), [survey]);
  const canSearch = useMemo(() => hasSearchableAnswers(survey), [survey]);
  const loaded = useMemo(() => responses.data?.pages.flat() ?? [], [responses.data]);
  const environmentTags = useMemo(() => (tags.data ?? []).map(toResponseTag), [tags.data]);

  const updatePages = (update: (page: TResponseWithQuotas[]) => TResponseWithQuotas[]) =>
    queryClient.setQueryData<InfiniteData<TResponseWithQuotas[], number>>(responsesQuery.queryKey, (data) =>
      data ? { ...data, pages: data.pages.map(update) } : data
    );

  // The counts, the summary and the response lists of other filters are stale after a change; this list is updated in place rather than read again from its first page.
  const invalidateOtherAnalysis = () =>
    queryClient.invalidateQueries({
      queryKey: analysisKeys.all(survey.id),
      predicate: (query) => JSON.stringify(query.queryKey) !== JSON.stringify(responsesQuery.queryKey),
    });

  // The pages are offsets, so a delete leaves the next page's offset and whether there is one stale (a short page reads as the last): the loaded pages are read again as well, the same number of them.
  const onResponsesDeleted = (responseIds: string[]) => {
    updatePages((page) => page.filter((response) => !responseIds.includes(response.id)));
    void invalidateSurveyAnalysis(queryClient, survey.id);
  };

  const onResponseChanged = async (responseId: string) => {
    const updated = await analysisApi.getResponse(responseId);
    updatePages((page) => page.map((response) => (response.id === responseId ? updated : response)));
    void queryClient.invalidateQueries({ queryKey: tagsQuery(survey.workspaceId).queryKey });
    void invalidateOtherAnalysis();
  };

  // The share modal of the header opens from the ?share param, as it does after publishing.
  const openShare = () =>
    void router.navigate({
      href: `${location.pathname}${router.options.stringifySearch({ ...location.search, share: true })}`,
    });

  const clearFiltersButton = (
    <Button variant="secondary" size="sm" onClick={resetState}>
      {t("common.clear_filters")}
    </Button>
  );

  const emptyState = isFiltered ? (
    <AnalysisEmptyState
      title={t("workspace.surveys.responses.no_responses_match_filters")}
      action={clearFiltersButton}
    />
  ) : (
    <AnalysisEmptyState
      title={t("workspace.surveys.responses.no_responses_yet")}
      action={
        !survey.archivedAt && (
          <Button size="sm" onClick={openShare}>
            {t("workspace.surveys.summary.share_survey")}
          </Button>
        )
      }
    />
  );

  const total = matchingCount.data;
  // A delete that empties the loaded rows while responses remain shows the placeholder rows until the pages are read again, not an empty state that contradicts the count.
  const isLoadingRows =
    responses.isPending || (loaded.length === 0 && responses.isFetching && (total ?? 0) > 0);
  const showCount = !isLoadingRows && total !== undefined && total > 0;

  const countSummary = showCount && (
    <div className="flex items-center gap-3 text-sm text-slate-600">
      <p aria-live="polite">
        {t("workspace.surveys.responses.showing_count", {
          loaded: Math.min(loaded.length, total),
          total,
        })}
      </p>
      {isFiltered && clearFiltersButton}
    </div>
  );

  return (
    <>
      <div className="flex min-h-9 gap-1.5">
        <CustomFilter survey={surveyWithRecall} search={canSearch && <ResponseKeywordSearch />} />
      </div>
      {responses.isError && (
        <p role="alert" className="text-sm text-red-600">
          {errorMessage(t, responses.error)}
        </p>
      )}
      <div className="w-full">
        <ResponseTable
          survey={survey}
          responses={loaded}
          environmentTags={environmentTags}
          isReadOnly={isReadOnly}
          fetchNextPage={() => responses.fetchNextPage().then((result) => result.data?.pages.flat() ?? [])}
          hasMore={responses.hasNextPage}
          isFetchingFirstPage={isLoadingRows}
          totalCount={total}
          countSummary={countSummary}
          emptyState={emptyState}
          showQuotasColumn={(quotas.data?.length ?? 0) > 0}
          locale={locale}
          onResponseChanged={(responseId) => void onResponseChanged(responseId)}
          onResponsesDeleted={onResponsesDeleted}
        />
      </div>
    </>
  );
};
