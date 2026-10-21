import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { organizationKeys } from "./organizations";

export type ApiKeyPermission = "read" | "write" | "manage";

export interface OrganizationAccess {
  accessControl: { read: boolean; write: boolean };
}

export interface ApiKey {
  id: string;
  label: string;
  createdAt: string;
  organizationAccess: OrganizationAccess;
  workspacePermissions: { workspaceId: string; workspaceName: string; permission: ApiKeyPermission }[];
}

/** A key as its creation returns it: the only time the plaintext key is ever available. */
export interface CreatedApiKey extends ApiKey {
  key: string;
}

export interface ApiKeyInput {
  label: string;
  workspacePermissions: { workspaceId: string; permission: ApiKeyPermission }[];
  organizationAccess: OrganizationAccess;
}

export const apiKeyKeys = {
  list: (organizationId: string) => [...organizationKeys.detail(organizationId), "api-keys"] as const,
};

export const apiKeysQuery = (organizationId: string) =>
  queryOptions({
    queryKey: apiKeyKeys.list(organizationId),
    queryFn: ({ signal }) => api.get<ApiKey[]>(`/api/app/organizations/${organizationId}/api-keys`, { signal }),
  });

export const useCreateApiKey = (organizationId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ApiKeyInput) =>
      api.post<CreatedApiKey>(`/api/app/organizations/${organizationId}/api-keys`, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: apiKeyKeys.list(organizationId) }),
  });
};

export const useRelabelApiKey = (organizationId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, label }: { id: string; label: string }) =>
      api.patch<ApiKey>(`/api/app/organizations/${organizationId}/api-keys/${id}`, { label }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: apiKeyKeys.list(organizationId) }),
  });
};

export const useDeleteApiKey = (organizationId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete<undefined>(`/api/app/organizations/${organizationId}/api-keys/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: apiKeyKeys.list(organizationId) }),
  });
};
