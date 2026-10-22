import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import type { ActionClass, ActionInput } from "../action-classes";
import { api } from "../api";

export const actionClassKeys = {
  list: (workspaceId: string) => ["action-classes", workspaceId] as const,
  surveys: (actionClassId: string) => ["action-classes", "surveys", actionClassId] as const,
};

/** The workspace's action classes, oldest first. */
export const actionClassesQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: actionClassKeys.list(workspaceId),
    queryFn: ({ signal }) =>
      api
        .get<{ actionClasses: ActionClass[] }>(`/api/app/workspaces/${workspaceId}/action-classes`, {
          signal,
        })
        .then((res) => res.actionClasses),
  });

/** The names of the surveys an action triggers, split on whether they are running. */
export const actionClassSurveysQuery = (actionClassId: string) =>
  queryOptions({
    queryKey: actionClassKeys.surveys(actionClassId),
    queryFn: ({ signal }) =>
      api.get<{ activeSurveys: string[]; inactiveSurveys: string[] }>(
        `/api/app/action-classes/${actionClassId}/surveys`,
        { signal }
      ),
    staleTime: 0,
  });

const useActionClassMutation = <V>(workspaceId: string, mutationFn: (variables: V) => Promise<unknown>) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: actionClassKeys.list(workspaceId) }),
  });
};

export const useCreateActionClass = (workspaceId: string) =>
  useActionClassMutation(workspaceId, (input: ActionInput) =>
    api.post<ActionClass>(`/api/app/workspaces/${workspaceId}/action-classes`, input)
  );

export const useUpdateActionClass = (workspaceId: string) =>
  useActionClassMutation(workspaceId, ({ id, input }: { id: string; input: ActionInput }) =>
    api.patch<ActionClass>(`/api/app/action-classes/${id}`, input)
  );

export const useDeleteActionClass = (workspaceId: string) =>
  useActionClassMutation(workspaceId, (id: string) =>
    api.delete<ActionClass>(`/api/app/action-classes/${id}`)
  );
