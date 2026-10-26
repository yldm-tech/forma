import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { organizationKeys } from "./organizations";

export type ApiKeyPermission = "read" | "write" | "manage";

export interface OrganizationAccess {
  accessControl: { read: boolean; write: boolean };
}

/** Why the server disabled a key: its creator was removed, left, deleted their account or was demoted below manager (api_keys.disabled_reason). */
export type ApiKeyDisabledReason = "creator_removed" | "creator_left" | "creator_deleted" | "creator_demoted";

export interface ApiKey {
  id: string;
  label: string;
  createdAt: string;
  organizationAccess: OrganizationAccess;
  workspacePermissions: { workspaceId: string; workspaceName: string; permission: ApiKeyPermission }[];
  /** The last request the key authenticated, recorded at most every 30 seconds; null when it was never used. */
  lastUsedAt: string | null;
  /** Who created the key; null when their account is gone. formerMember: they no longer belong to the organization. suspended: the organization suspended their access, and the key does not authenticate until it is restored. */
  createdBy: { id: string; name: string; formerMember: boolean; suspended: boolean } | null;
  /** Set together once the key was disabled; a disabled key never authenticates again. */
  disabledAt: string | null;
  disabledReason: ApiKeyDisabledReason | null;
  /** The key's last four characters, kept when it was created; null for a key created before hints were kept. */
  keyHint: string | null;
  /** When the key stops working, chosen when it was created; null for a key that works until it is deleted. */
  expiresAt: string | null;
}

/** A key as its creation returns it: the only time the plaintext key is ever available. */
export interface CreatedApiKey extends ApiKey {
  key: string;
}

export interface ApiKeyInput {
  label: string;
  workspacePermissions: { workspaceId: string; permission: ApiKeyPermission }[];
  organizationAccess: OrganizationAccess;
  /** An ISO 8601 time in the future, or null for a key that never expires. */
  expiresAt: string | null;
}

export const apiKeyKeys = {
  list: (organizationId: string) => [...organizationKeys.detail(organizationId), "api-keys"] as const,
};

export const apiKeysQuery = (organizationId: string) =>
  queryOptions({
    queryKey: apiKeyKeys.list(organizationId),
    queryFn: ({ signal }) =>
      api.get<ApiKey[]>(`/api/app/organizations/${organizationId}/api-keys`, { signal }),
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
    mutationFn: (id: string) =>
      api.delete<undefined>(`/api/app/organizations/${organizationId}/api-keys/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: apiKeyKeys.list(organizationId) }),
  });
};
