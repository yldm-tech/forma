import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import type { TActionClass, TActionClassInput } from "@forma/types/action-classes";
import { api } from "../../lib/api";
import { type Stamped, stamped } from "../../lib/wire";

// A workspace's action classes: the events that trigger app surveys.

export const parseActionClass = (actionClass: Stamped<TActionClass>): TActionClass =>
  stamped<TActionClass>(actionClass);

export const actionClassKeys = {
  list: (workspaceId: string) => ["workspaces", workspaceId, "action-classes"] as const,
  surveys: (actionClassId: string) => ["action-classes", actionClassId, "surveys"] as const,
};

/** The workspace's action classes, oldest first. */
export const actionClassesQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: actionClassKeys.list(workspaceId),
    queryFn: ({ signal }) =>
      api
        .get<{ actionClasses: Stamped<TActionClass>[] }>(
          `/api/app/workspaces/${workspaceId}/action-classes`,
          {
            signal,
          }
        )
        .then((res) => res.actionClasses.map(parseActionClass)),
  });

/** The input of a create or an update; the workspace comes from the path or from the stored action class. */
export type ActionClassInput = Omit<TActionClassInput, "workspaceId">;

export const useCreateActionClass = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ActionClassInput) =>
      api
        .post<Stamped<TActionClass>>(`/api/app/workspaces/${workspaceId}/action-classes`, input)
        .then(parseActionClass),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: actionClassKeys.list(workspaceId) }),
  });
};

export const useUpdateActionClass = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: ActionClassInput }) =>
      api.patch<Stamped<TActionClass>>(`/api/app/action-classes/${id}`, input).then(parseActionClass),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: actionClassKeys.list(workspaceId) }),
  });
};

/** Deletes an action class; the survey triggers that name it go with it. */
export const useDeleteActionClass = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete<unknown>(`/api/app/action-classes/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: actionClassKeys.list(workspaceId) }),
  });
};

/** The names of the surveys an action class triggers, split by whether they are running, leaving out excludeSurveyId. */
export const actionClassSurveysQuery = (actionClassId: string, excludeSurveyId?: string) =>
  queryOptions({
    queryKey: [...actionClassKeys.surveys(actionClassId), excludeSurveyId ?? null],
    queryFn: ({ signal }) => {
      const query = excludeSurveyId ? `?excludeSurveyId=${encodeURIComponent(excludeSurveyId)}` : "";
      return api.get<{ activeSurveys: string[]; inactiveSurveys: string[] }>(
        `/api/app/action-classes/${actionClassId}/surveys${query}`,
        { signal }
      );
    },
  });
