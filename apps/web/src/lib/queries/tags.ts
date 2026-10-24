import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { ApiError, api } from "../api";

/** A tag as GET /api/v3/tags returns it, with the number of responses carrying it. */
export interface Tag {
  id: string;
  name: string;
  workspaceId: string;
  createdAt: string;
  updatedAt: string;
  count: number;
}

export const tagKeys = {
  list: (workspaceId: string) => ["tags", workspaceId] as const,
};

export const tagsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: tagKeys.list(workspaceId),
    queryFn: ({ signal }) =>
      api
        .get<{ data: Tag[] }>(`/api/v3/tags?workspaceId=${encodeURIComponent(workspaceId)}`, { signal })
        .then((res) => res.data),
  });

/** forma2's TagError code for a rename onto a name another tag of the workspace has. */
export const TAG_NAME_ALREADY_EXISTS = "tag_name_already_exists";

/** Whether a rename was refused because the name is taken: the v3 problem says so only in invalid_params. */
export const isDuplicateTagNameError = (error: unknown): boolean =>
  error instanceof ApiError && error.invalidParams.some((param) => param.reason === TAG_NAME_ALREADY_EXISTS);

// Every tag mutation refreshes the list, as forma2's hooks invalidated it.
const useTagMutation = <V>(workspaceId: string, mutationFn: (variables: V) => Promise<unknown>) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSettled: () => queryClient.invalidateQueries({ queryKey: tagKeys.list(workspaceId) }),
  });
};

export const useRenameTag = (workspaceId: string) =>
  useTagMutation(workspaceId, ({ tagId, name }: { tagId: string; name: string }) =>
    api.patch<{ data: Tag }>(`/api/v3/tags/${tagId}`, { name })
  );

export const useDeleteTag = (workspaceId: string) =>
  useTagMutation(workspaceId, (tagId: string) =>
    api.delete<{ data: { id: string } }>(`/api/v3/tags/${tagId}`)
  );

export const useMergeTags = (workspaceId: string) =>
  useTagMutation(workspaceId, ({ tagId, newTagId }: { tagId: string; newTagId: string }) =>
    api.post<{ data: { id: string } }>(`/api/v3/tags/${tagId}/merge`, { newTagId })
  );
