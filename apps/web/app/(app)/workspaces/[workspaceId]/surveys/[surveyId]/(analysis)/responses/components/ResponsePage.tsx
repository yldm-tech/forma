"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import toast from "react-hot-toast";
import { useTranslation } from "react-i18next";
import { TSurveyQuota } from "@forma/types/quota";
import { TResponseWithQuotas } from "@forma/types/responses";
import { TSurvey } from "@forma/types/surveys/types";
import { TTag } from "@forma/types/tags";
import { TUser, TUserLocale } from "@forma/types/user";
import { getResponsesAction } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/actions";
import { useResponseFilter } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/components/response-filter-context";
import { ResponseDataView } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/responses/components/ResponseDataView";
import { CustomFilter } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/components/CustomFilter";
import { getFormattedFilters } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/lib/surveys";
import { getFormattedErrorMessage } from "@/lib/utils/helper";
import { replaceHeadlineRecall } from "@/lib/utils/recall";

interface ResponsePageProps {
  survey: TSurvey;
  surveyId: string;
  user?: TUser;
  environmentTags: TTag[];
  responsesPerPage: number;
  locale: TUserLocale;
  isReadOnly: boolean;
  isQuotasAllowed: boolean;
  quotas: TSurveyQuota[];
  initialResponses?: TResponseWithQuotas[];
}

export const ResponsePage = ({
  survey,
  surveyId,
  user,
  environmentTags,
  responsesPerPage,
  locale,
  isReadOnly,
  isQuotasAllowed,
  quotas,
  initialResponses = [],
}: ResponsePageProps) => {
  const [responses, setResponses] = useState<TResponseWithQuotas[]>(initialResponses);
  const [page, setPage] = useState<number | null>(null);
  const [hasMore, setHasMore] = useState<boolean>(initialResponses.length >= responsesPerPage);
  const [isFetchingFirstPage, setIsFetchingFirstPage] = useState<boolean>(false);
  const { selectedFilter, dateRange, registerAnalysisRefreshHandler } = useResponseFilter();
  const { t } = useTranslation();
  const computedFilters = useMemo(
    () => getFormattedFilters(survey, selectedFilter, dateRange),

    [survey, selectedFilter, dateRange]
  );

  // `survey` is an RSC prop, so every `router.refresh()` (survey status dropdown, share modal, reset
  // survey) hands down a freshly deserialized object and `computedFilters` takes a new identity for
  // unchanged content. Memoized so the serialization happens when the filters actually recompute
  // rather than on every render of this page — each scroll fetch, each row or tag edit.
  const filtersKey = useMemo(() => JSON.stringify(computedFilters), [computedFilters]);

  // The identity everything downstream keys on, held steady while the filter *value* is unchanged.
  // Without it a refresh re-creates `fetchNextPage` and `refetchResponses`, which re-registers the
  // analysis refresh handler and would collapse the infinite-scroll list back to page 1.
  // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on the filter value, not its identity
  const filters = useMemo(() => computedFilters, [filtersKey]);

  const fetchNextPage = useCallback(async () => {
    if (page === null) return;
    const newPage = page + 1;

    let newResponses: TResponseWithQuotas[] = [];

    const getResponsesActionResponse = await getResponsesAction({
      surveyId,
      limit: responsesPerPage,
      offset: (newPage - 1) * responsesPerPage,
      filterCriteria: filters,
    });
    newResponses = getResponsesActionResponse?.data || [];

    if (newResponses.length === 0 || newResponses.length < responsesPerPage) {
      setHasMore(false);
    }
    setResponses([...responses, ...newResponses]);
    setPage(newPage);
  }, [filters, page, responses, responsesPerPage, surveyId]);

  const updateResponseList = (responseIds: string[]) => {
    setResponses((prev) => prev.filter((r) => !responseIds.includes(r.id)));
  };

  const updateResponse = (responseId: string, updatedResponse: TResponseWithQuotas) => {
    setResponses((prev) => prev.map((r) => (r.id === responseId ? updatedResponse : r)));
  };

  const refetchResponses = useCallback(async () => {
    setIsFetchingFirstPage(true);

    try {
      const getResponsesActionResponse = await getResponsesAction({
        surveyId,
        limit: responsesPerPage,
        offset: 0,
        filterCriteria: filters,
      });

      if (getResponsesActionResponse?.serverError) {
        toast.error(getFormattedErrorMessage(getResponsesActionResponse) ?? t("common.something_went_wrong"));
      }

      const freshResponses = getResponsesActionResponse?.data ?? [];
      setResponses(freshResponses);
      setPage(1);
      setHasMore(freshResponses.length >= responsesPerPage);
    } finally {
      setIsFetchingFirstPage(false);
    }
  }, [filters, responsesPerPage, surveyId, t]);

  useEffect(() => {
    return registerAnalysisRefreshHandler(refetchResponses);
  }, [refetchResponses, registerAnalysisRefreshHandler]);

  const surveyMemoized = useMemo(() => {
    return replaceHeadlineRecall(survey, "default");
  }, [survey]);

  // Only fetch if filters are applied (not on initial mount with no filters)
  const hasFilters =
    selectedFilter?.responseStatus !== "all" ||
    (selectedFilter?.filter && selectedFilter.filter.length > 0) ||
    (dateRange.from && dateRange.to);

  // The inputs of the last page-1 fetch, by VALUE — the guard SummaryPage uses, for the same reason. `selectedFilter`/`dateRange` are object literals held in the provider, and a route refresh or the URL-filter hydration hands back fresh identities for identical content. Keyed on their identity alone this effect would re-run, skip the `page === null` branch (page is 1 by then) and refetch offset 0 with the very same filters — discarding the server-seeded `initialResponses` and flashing a spinner.
  const lastFetchedKeyRef = useRef<string | null>(null);

  useEffect(() => {
    const fetchKey = `${surveyId}:${responsesPerPage}:${filtersKey}`;

    const fetchFilteredResponses = async () => {
      try {
        // skip call for initial mount: `initialResponses` already holds this exact unfiltered page 1
        if (page === null && !hasFilters) {
          lastFetchedKeyRef.current = fetchKey;
          setPage(1);
          return;
        }
        if (fetchKey === lastFetchedKeyRef.current) {
          return;
        }
        // Commit the key BEFORE awaiting, so a re-run triggered while the request is in flight skips.
        lastFetchedKeyRef.current = fetchKey;
        setPage(1);
        setIsFetchingFirstPage(true);
        let responses: TResponseWithQuotas[] = [];

        const getResponsesActionResponse = await getResponsesAction({
          surveyId,
          limit: responsesPerPage,
          offset: 0,
          filterCriteria: filters,
        });

        if (getResponsesActionResponse?.serverError) {
          // Roll the key back on failure so re-selecting the same filter retries instead of being deduped against a failed fetch.
          lastFetchedKeyRef.current = null;
        }

        responses = getResponsesActionResponse?.data || [];

        if (responses.length < responsesPerPage) {
          setHasMore(false);
        } else {
          setHasMore(true);
        }
        setResponses(responses);
      } finally {
        setIsFetchingFirstPage(false);
      }
    };
    fetchFilteredResponses();
    // page is intentionally omitted to avoid refetching after the initial page setup.
    // hasFilters is derived from selectedFilter/dateRange which are already deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- effect must run only when the filter value changes, not on page updates it sets internally
  }, [filtersKey, responsesPerPage, selectedFilter, dateRange, surveyId]);

  return (
    <>
      <div className="flex h-9 gap-1.5">
        <CustomFilter survey={surveyMemoized} />
      </div>
      <ResponseDataView
        survey={survey}
        responses={responses}
        user={user}
        environmentTags={environmentTags}
        isReadOnly={isReadOnly}
        fetchNextPage={fetchNextPage}
        hasMore={hasMore}
        updateResponseList={updateResponseList}
        updateResponse={updateResponse}
        isFetchingFirstPage={isFetchingFirstPage}
        locale={locale}
        isQuotasAllowed={isQuotasAllowed}
        quotas={quotas}
      />
    </>
  );
};
