import {
  type InfiniteData,
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useMutationState,
  useQueryClient,
} from "@tanstack/react-query";
import { forgetEditorSurvey } from "../../survey-editor/api";
import { type SurveyListPage, surveysApi } from "../api";
import type { SurveyFilters, SurveyStatus } from "./filters";
import {
  type SurveyListKey,
  flattenSurveyPages,
  removeSurveyFromInfiniteData,
  removedSurveyId,
  surveyKeys,
  surveyMutationKeys,
  updateSurveyInInfiniteData,
} from "./query";

/**
 * Survey ids whose archive, restore or delete is still in flight. The optimistic patch alone cannot keep such a survey out of the list: a fetch that starts after it (a remount, a focus refetch, the next page, another filter's cache entry) resolves with server data that still lists the survey. The mutation stays pending until its invalidation has landed, so hiding these ids at render time covers the whole window (forma2's ENG-2583).
 */
const usePendingSurveyRemovals = (): string[] =>
  useMutationState({
    filters: { mutationKey: surveyMutationKeys.removal(), status: "pending" },
    select: (mutation) => removedSurveyId(mutation.state.variables),
  }).filter((id): id is string => id !== undefined);

/** forma2's useSurveys: the list's pages under the given filters, with surveys being removed left out and the workspace's survey count read from page one. */
export const useSurveys = ({
  workspaceId,
  limit,
  filters,
}: {
  workspaceId: string;
  limit: number;
  filters: SurveyFilters;
}) => {
  const queryKey = surveyKeys.list({ workspaceId, limit, filters });
  const query = useInfiniteQuery({
    queryKey,
    initialPageParam: null as string | null,
    placeholderData: keepPreviousData,
    queryFn: ({ pageParam, signal }) =>
      surveysApi.list(
        { workspaceId, limit, cursor: pageParam, includeTotalCount: pageParam === null, filters },
        signal
      ),
    getNextPageParam: (lastPage) => lastPage.meta.nextCursor ?? undefined,
  });
  const pending = usePendingSurveyRemovals();
  const surveys = flattenSurveyPages(query.data).filter((survey) => !pending.includes(survey.id));
  const workspaceSurveyCount = query.data?.pages[0]?.meta.workspaceSurveyCount ?? null;
  return { ...query, queryKey, surveys, workspaceSurveyCount };
};

/** An action that takes the survey out of the current view (delete, archive, restore): dropped from the cache at once, put back if the server refuses, and every list refetched once it settles so mixed-filter views reconcile. */
const useSurveyRemoval = (
  queryKey: SurveyListKey,
  mutationFn: (surveyId: string) => Promise<unknown>,
  removesFromWorkspace = false
) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationKey: surveyMutationKeys.removal(),
    mutationFn: ({ surveyId }: { surveyId: string }) => mutationFn(surveyId),
    onMutate: async ({ surveyId }) => {
      await queryClient.cancelQueries({ queryKey });
      const previousData = queryClient.getQueryData<InfiniteData<SurveyListPage>>(queryKey);
      queryClient.setQueryData<InfiniteData<SurveyListPage> | undefined>(queryKey, (data) =>
        removeSurveyFromInfiniteData(data, surveyId, { removesFromWorkspace })
      );
      return { previousData };
    },
    onError: (_error, _variables, context) => {
      if (context?.previousData) queryClient.setQueryData(queryKey, context.previousData);
    },
    // Awaited, so the mutation stays pending (and its survey hidden) until the cache holds server data again.
    onSettled: (_data, _error, { surveyId }) => {
      forgetEditorSurvey(queryClient, surveyId);
      return queryClient.invalidateQueries({ queryKey: surveyKeys.lists() });
    },
  });
};

export const useDeleteSurvey = (queryKey: SurveyListKey) =>
  useSurveyRemoval(queryKey, surveysApi.delete, true);
export const useArchiveSurvey = (queryKey: SurveyListKey) => useSurveyRemoval(queryKey, surveysApi.archive);
export const useRestoreSurvey = (queryKey: SurveyListKey) => useSurveyRemoval(queryKey, surveysApi.restore);

/** An edit of a listed survey's fields, shown at once and rolled back if the server refuses it. */
const useSurveyPatch = <TVariables extends { surveyId: string }>(
  queryKey: SurveyListKey,
  mutationFn: (variables: TVariables) => Promise<unknown>,
  patchOf: (variables: TVariables) => Partial<SurveyListPage["data"][number]>
) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onMutate: async (variables: TVariables) => {
      await queryClient.cancelQueries({ queryKey });
      const previousData = queryClient.getQueryData<InfiniteData<SurveyListPage>>(queryKey);
      queryClient.setQueryData<InfiniteData<SurveyListPage> | undefined>(queryKey, (data) =>
        updateSurveyInInfiniteData(data, variables.surveyId, patchOf(variables))
      );
      return { previousData };
    },
    onError: (_error, _variables, context) => {
      if (context?.previousData) queryClient.setQueryData(queryKey, context.previousData);
    },
    onSettled: (_data, _error, variables) => {
      forgetEditorSurvey(queryClient, variables.surveyId);
      return queryClient.invalidateQueries({ queryKey: surveyKeys.lists() });
    },
  });
};

export const useRenameSurvey = (queryKey: SurveyListKey) =>
  useSurveyPatch(
    queryKey,
    ({ surveyId, name }: { surveyId: string; name: string }) => surveysApi.rename(surveyId, name),
    ({ name }) => ({ name })
  );

export const useUpdateSurveyStatus = (queryKey: SurveyListKey) =>
  useSurveyPatch(
    queryKey,
    ({ surveyId, status }: { surveyId: string; status: SurveyStatus }) =>
      surveysApi.updateStatus(surveyId, status),
    ({ status }) => ({ status })
  );

/** forma2's copySurveyToOtherWorkspaceAction from the list; every list is refetched afterwards, since a duplicate lands in this one. */
export const useCopySurvey = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ surveyId, targetWorkspaceId }: { surveyId: string; targetWorkspaceId: string }) =>
      surveysApi.copy(surveyId, targetWorkspaceId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: surveyKeys.lists() }),
  });
};
