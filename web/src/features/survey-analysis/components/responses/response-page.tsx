import type { TResponseWithQuotas } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import { type InfiniteData, useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { errorMessage } from "../../../../lib/errors";
import { tagsQuery } from "../../../../lib/queries/tags";
import { replaceHeadlineRecall } from "../../../../lib/survey/recall";
import { quotasQuery } from "../../../survey-editor/api";
import { analysisApi, analysisKeys, surveyResponsesQuery, toResponseTag } from "../../api";
import { CustomFilter } from "../filter/custom-filter";
import { useResponseFilter } from "../filter/response-filter-context";
import { ResponseTable } from "./response-table";

/**
 * forma2's ResponsePage: the filter bar over the response table, 25 responses a page under the current filter, newest first. A deleted response leaves the loaded pages at once; a tag change reads the response again.
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
  const { filterCriteria } = useResponseFilter();
  const responsesQuery = surveyResponsesQuery(survey.id, filterCriteria);
  const responses = useInfiniteQuery(responsesQuery);
  const tags = useQuery(tagsQuery(survey.workspaceId));
  const quotas = useQuery(quotasQuery(survey.id));
  const surveyWithRecall = useMemo(() => replaceHeadlineRecall(survey, "default"), [survey]);
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

  const onResponsesDeleted = (responseIds: string[]) => {
    updatePages((page) => page.filter((response) => !responseIds.includes(response.id)));
    void invalidateOtherAnalysis();
  };

  const onResponseChanged = async (responseId: string) => {
    const updated = await analysisApi.getResponse(responseId);
    updatePages((page) => page.map((response) => (response.id === responseId ? updated : response)));
    void queryClient.invalidateQueries({ queryKey: tagsQuery(survey.workspaceId).queryKey });
    void invalidateOtherAnalysis();
  };

  return (
    <>
      <div className="flex h-9 gap-1.5">
        <CustomFilter survey={surveyWithRecall} />
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
          fetchNextPage={() => void responses.fetchNextPage()}
          hasMore={responses.hasNextPage}
          isFetchingFirstPage={responses.isPending}
          showQuotasColumn={(quotas.data?.length ?? 0) > 0}
          locale={locale}
          onResponseChanged={(responseId) => void onResponseChanged(responseId)}
          onResponsesDeleted={onResponsesDeleted}
        />
      </div>
    </>
  );
};
