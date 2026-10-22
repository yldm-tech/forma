import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { OrganizationRole } from "../roles";
import { organizationKeys } from "./organizations";

export interface Invite {
  id: string;
  email: string;
  name: string | null;
  role: OrganizationRole;
  expiresAt: string;
  createdAt: string;
}

/** What the members page offers the caller. */
export interface UserManagementAccess {
  canManageUsers: boolean;
  adminTeamIds: string[];
  inviteDisabled: boolean;
  billingRoleAllowed: boolean;
}

export interface Invitee {
  email: string;
  name: string;
  role: OrganizationRole;
  teamIds: string[];
}

export const inviteKeys = {
  list: (organizationId: string) => [...organizationKeys.detail(organizationId), "invites"] as const,
  access: (organizationId: string) =>
    [...organizationKeys.detail(organizationId), "user-management"] as const,
};

export const invitesQuery = (organizationId: string) =>
  queryOptions({
    queryKey: inviteKeys.list(organizationId),
    queryFn: ({ signal }) =>
      api.get<Invite[]>(`/api/app/organizations/${organizationId}/invites`, { signal }),
  });

export const userManagementQuery = (organizationId: string) =>
  queryOptions({
    queryKey: inviteKeys.access(organizationId),
    queryFn: ({ signal }) =>
      api.get<UserManagementAccess>(`/api/app/organizations/${organizationId}/user-management`, { signal }),
  });

const useInvalidateInvites = (organizationId: string) => {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: inviteKeys.list(organizationId) });
};

export const useCreateInvite = (organizationId: string) => {
  const invalidate = useInvalidateInvites(organizationId);
  return useMutation({
    mutationFn: (invitee: Invitee) =>
      api.post<{ id: string }>(`/api/app/organizations/${organizationId}/invites`, invitee),
    onSuccess: invalidate,
  });
};

export const useResendInvite = (organizationId: string) => {
  const invalidate = useInvalidateInvites(organizationId);
  return useMutation({
    mutationFn: (inviteId: string) =>
      api.post<Invite>(`/api/app/organizations/${organizationId}/invites/${inviteId}/resend`),
    onSuccess: invalidate,
  });
};

export const useInviteLink = (organizationId: string) => {
  const invalidate = useInvalidateInvites(organizationId);
  return useMutation({
    mutationFn: (inviteId: string) =>
      api.post<{ inviteToken: string; inviteUrl: string }>(`/api/app/invites/${inviteId}/link`),
    onSuccess: invalidate,
  });
};

export const useChangeInviteRole = (organizationId: string) => {
  const invalidate = useInvalidateInvites(organizationId);
  return useMutation({
    mutationFn: ({ inviteId, role }: { inviteId: string; role: OrganizationRole }) =>
      api.patch<undefined>(`/api/app/invites/${inviteId}`, { role }),
    onSettled: invalidate,
  });
};

export const useDeleteInvite = (organizationId: string) => {
  const invalidate = useInvalidateInvites(organizationId);
  return useMutation({
    mutationFn: (inviteId: string) => api.delete<undefined>(`/api/app/invites/${inviteId}`),
    onSettled: invalidate,
  });
};

/** The onboarding owner invite of the setup flow. */
export const useSetupInvite = (organizationId: string) =>
  useMutation({
    mutationFn: (input: { email: string; name: string }) =>
      api.post<{ id: string }>(`/api/app/organizations/${organizationId}/setup-invites`, input),
  });
