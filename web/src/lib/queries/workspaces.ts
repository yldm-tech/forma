import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { OrganizationRole } from "../roles";
import { organizationKeys } from "./organizations";

export interface Workspace {
  id: string;
  createdAt: string;
  updatedAt: string;
  name: string;
  organizationId: string;
}

/** What the caller may do in a workspace, derived from the decisions the endpoints enforce. */
export interface WorkspaceAccess {
  role: OrganizationRole | "";
  canWrite: boolean;
  canManage: boolean;
  isReadOnly: boolean;
}

export interface WorkspaceDetail extends Workspace {
  access: WorkspaceAccess;
}

/** The SPA's own error code for a billing member asking for a workspace; details.redirect is where to send them. */
export const BILLING_ROLE_CODE = "billing_role";

export const workspaceKeys = {
  detail: (id: string) => ["workspaces", id] as const,
  list: (organizationId: string, access: "read" | "write") =>
    [...organizationKeys.detail(organizationId), "workspaces", access] as const,
};

export const workspaceQuery = (id: string) =>
  queryOptions({
    queryKey: workspaceKeys.detail(id),
    queryFn: ({ signal }) => api.get<WorkspaceDetail>(`/api/app/workspaces/${id}`, { signal }),
  });

/** The organization's workspaces the caller can read, or with access "write" the ones they can write to. */
export const workspacesQuery = (organizationId: string, access: "read" | "write" = "read") =>
  queryOptions({
    queryKey: workspaceKeys.list(organizationId, access),
    queryFn: ({ signal }) =>
      api
        .get<{
          workspaces: Workspace[];
        }>(`/api/app/organizations/${organizationId}/workspaces${access === "write" ? "?access=write" : ""}`, { signal })
        .then((res) => res.workspaces),
  });

export const environmentQuery = (environmentId: string) =>
  queryOptions({
    queryKey: ["environments", environmentId],
    queryFn: ({ signal }) =>
      api.get<{ workspaceId: string }>(`/api/app/environments/${encodeURIComponent(environmentId)}`, { signal }),
  });

export const useCreateWorkspace = (organizationId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; teamIds?: string[] }) =>
      api.post<Workspace>(`/api/app/organizations/${organizationId}/workspaces`, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organizationId) }),
  });
};
