import {
  type InfiniteData,
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  type TCreateWorkflowInput,
  type TWorkflowListKey,
  type TWorkflowListPage,
  type TWorkflowResource,
  type TWorkflowRunListFilters,
  flattenWorkflowPages,
  flattenWorkflowRunPages,
  removeWorkflowFromInfiniteData,
  workflowKeys,
  workflowRunKeys,
  workflowsApi,
} from "../api";
import type { TWorkflowSortBy, TWorkflowStatus } from "../definition";

// The workflows list's and the runs pages' server state, forma2's modules/workflows/hooks/use-workflows.ts, use-workflow-runs.ts, use-workflow-run.ts and the list's mutation hooks.

/** forma2's useWorkflows: the list's pages under the given filters, newest cursor last. */
export const useWorkflows = ({
  workspaceId,
  limit,
  nameContains,
  statusIn,
  sortBy,
}: {
  workspaceId: string;
  limit: number;
  nameContains: string;
  statusIn?: TWorkflowStatus[];
  sortBy?: TWorkflowSortBy;
}) => {
  const queryKey = workflowKeys.list({ workspaceId, limit, nameContains, statusIn, sortBy });
  const query = useInfiniteQuery({
    queryKey,
    initialPageParam: null as string | null,
    placeholderData: keepPreviousData,
    queryFn: ({ pageParam, signal }) =>
      workflowsApi.list(
        { workspaceId, limit, cursor: pageParam, filters: { nameContains, statusIn, sortBy } },
        signal
      ),
    getNextPageParam: (lastPage) => lastPage.meta.nextCursor ?? undefined,
  });
  return { ...query, queryKey, workflows: flattenWorkflowPages(query.data) };
};

/** forma2's useWorkflowRuns: the runs, newest first, under the given filters, which are part of the key so a change starts a fresh page sequence. */
export const useWorkflowRuns = ({
  workspaceId,
  limit,
  filters,
}: {
  workspaceId: string;
  limit: number;
  filters?: TWorkflowRunListFilters;
}) => {
  const queryKey = workflowRunKeys.list({ workspaceId, limit, filters });
  const query = useInfiniteQuery({
    queryKey,
    initialPageParam: null as string | null,
    placeholderData: keepPreviousData,
    queryFn: ({ pageParam, signal }) =>
      workflowsApi.listRuns({ workspaceId, limit, cursor: pageParam, filters }, signal),
    getNextPageParam: (lastPage) => lastPage.meta.nextCursor ?? undefined,
  });
  return { ...query, queryKey, runs: flattenWorkflowRunPages(query.data) };
};

/** forma2's useWorkflowRun: one run's detail, fetched on demand while the drawer is open and never polled; reopening shows progress. */
export const useWorkflowRun = ({ runId, enabled = true }: { runId: string | null; enabled?: boolean }) =>
  useQuery({
    queryKey: workflowRunKeys.detail(runId ?? ""),
    enabled: enabled && runId !== null,
    queryFn: ({ signal }) => workflowsApi.getRun(runId as string, signal),
  });

export const useCreateWorkflow = () => {
  const queryClient = useQueryClient();
  return useMutation<TWorkflowResource, Error, TCreateWorkflowInput>({
    mutationFn: (input) => workflowsApi.create(input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: workflowKeys.lists() });
    },
  });
};

export const useDuplicateWorkflow = () => {
  const queryClient = useQueryClient();
  return useMutation<TWorkflowResource, Error, { workflowId: string }>({
    mutationFn: ({ workflowId }) => workflowsApi.duplicate(workflowId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: workflowKeys.lists() });
    },
  });
};

// An action that takes the workflow out of the current view (archive, delete): dropped from the cache at once, put back if the server refuses, and every list refetched once it settles.
const useWorkflowRemoval = (
  queryKey: TWorkflowListKey,
  mutationFn: (workflowId: string) => Promise<unknown>
) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ workflowId }: { workflowId: string }) => mutationFn(workflowId),
    onMutate: async ({ workflowId }) => {
      await queryClient.cancelQueries({ queryKey });
      const previousData = queryClient.getQueryData<InfiniteData<TWorkflowListPage>>(queryKey);
      queryClient.setQueryData<InfiniteData<TWorkflowListPage> | undefined>(queryKey, (data) =>
        removeWorkflowFromInfiniteData(data, workflowId)
      );
      return { previousData };
    },
    onError: (_error, _variables, context) => {
      if (context?.previousData) queryClient.setQueryData(queryKey, context.previousData);
    },
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey: workflowKeys.lists() });
    },
  });
};

export const useArchiveWorkflow = ({ queryKey }: { queryKey: TWorkflowListKey }) =>
  useWorkflowRemoval(queryKey, workflowsApi.archive);

export const useDeleteWorkflow = ({ queryKey }: { queryKey: TWorkflowListKey }) =>
  useWorkflowRemoval(queryKey, workflowsApi.remove);

/** Restores an archived workflow. The row is not removed optimistically: it stays with its new status, and whether it remains visible depends on the filters, so the lists are refetched for the truth. */
export const useUnarchiveWorkflow = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ workflowId }: { workflowId: string }) => workflowsApi.unarchive(workflowId),
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey: workflowKeys.lists() });
    },
  });
};
