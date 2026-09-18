"use client";

import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import { initialFilters } from "@/modules/survey/list/lib/constants";
import { flattenSurveyPages, surveyKeys } from "@/modules/survey/list/lib/query";
import { TSurveyOverviewFilters } from "@/modules/survey/list/types/survey-overview";
import type { TSurveyListItem } from "@/modules/survey/list/types/survey-overview";
import { type TSurveyListPage, listSurveys } from "../lib/v3-surveys-client";
import { usePendingSurveyRemovals } from "./use-pending-survey-removals";

export const useSurveys = ({
  workspaceId,
  limit,
  filters,
  enabled = true,
  initialPage,
}: {
  workspaceId: string;
  limit: number;
  filters: TSurveyOverviewFilters;
  enabled?: boolean;
  /** Page one with the default filters, read on the server. See `lib/initial-page.ts`. */
  initialPage?: TInitialSurveyPage;
}) => {
  const queryKey = surveyKeys.list({
    workspaceId,
    limit,
    filters,
  });

  // Only when the active filters are the ones the server used. A visitor with stored filters gets a
  // different query key and the ordinary fetch; seeding theirs with default-filter rows would show
  // them results they did not ask for.
  const seed =
    initialPage && isDefaultFilters(filters)
      ? {
          pages: [initialPage],
          pageParams: [null as string | null],
        }
      : undefined;

  const query = useInfiniteQuery({
    queryKey,
    initialPageParam: null as string | null,
    enabled,
    initialData: seed,
    placeholderData: keepPreviousData,
    queryFn: ({ pageParam, signal }) =>
      listSurveys({
        workspaceId,
        limit,
        cursor: pageParam,
        includeTotalCount: pageParam === null,
        filters,
        signal,
      }),
    getNextPageParam: (lastPage) => lastPage.meta.nextCursor ?? undefined,
  });

  // A survey being archived, restored or deleted stays out of the list for the whole operation, even
  // if a refetch lands first and writes it back into the cache (ENG-2583).
  const pendingRemovals = usePendingSurveyRemovals();
  const surveys = flattenSurveyPages(query.data).filter((survey) => !pendingRemovals.includes(survey.id));
  // Read from page one: cursor pages are requested with includeTotalCount=false and carry null.
  const workspaceSurveyCount = query.data?.pages[0]?.meta.workspaceSurveyCount ?? null;

  return {
    ...query,
    queryKey,
    surveys,
    workspaceSurveyCount,
  };
};

export type TInitialSurveyPage = {
  data: TSurveyListItem[];
  meta: TSurveyListPage["meta"];
};

const isDefaultFilters = (filters: TSurveyOverviewFilters): boolean =>
  filters.name === initialFilters.name &&
  filters.sortBy === initialFilters.sortBy &&
  filters.status.length === 0 &&
  filters.type.length === 0;
