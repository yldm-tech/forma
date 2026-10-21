import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { OrganizationRole } from "../roles";

export interface Organization {
  id: string;
  createdAt: string;
  updatedAt: string;
  name: string;
  isAISmartToolsEnabled: boolean;
  /** An IANA zone; null means UTC. */
  displayTimeZone: string | null;
}

/** An organization in the caller's list, with the caller's role in it. */
export interface OrganizationSummary extends Organization {
  role: OrganizationRole;
}

export interface Membership {
  organizationId: string;
  userId: string;
  accepted: boolean;
  role: OrganizationRole;
}

export interface OrganizationDetail extends Organization {
  membership: Membership;
  /** Whether the caller's role may manage members and invites under USER_MANAGEMENT_MINIMUM_ROLE. */
  userManagementAllowed: boolean;
}

export interface Member {
  userId: string;
  name: string;
  email: string;
  accepted: boolean;
  role: OrganizationRole;
  isActive: boolean;
}

/** Where /organizations/{id} sends the browser. */
export interface OrganizationNavigation {
  path: string;
  canCreateWorkspace: boolean;
}

/** Where the root page sends the browser: a path, or (with none) the workspaces the user can open, in organization order. */
export interface Home {
  path?: string;
  workspaceIds: string[];
}

export const organizationKeys = {
  all: ["organizations"] as const,
  detail: (id: string) => ["organizations", id] as const,
  navigation: (id: string) => ["organizations", id, "navigation"] as const,
  members: (id: string) => ["organizations", id, "members"] as const,
};

export const homeQuery = queryOptions({
  queryKey: ["home"],
  queryFn: ({ signal }) => api.get<Home>("/api/app/home", { signal }),
  staleTime: 0,
});

export const organizationsQuery = queryOptions({
  queryKey: organizationKeys.all,
  queryFn: ({ signal }) =>
    api
      .get<{ organizations: OrganizationSummary[] }>("/api/app/organizations", { signal })
      .then((res) => res.organizations),
});

export const organizationQuery = (id: string) =>
  queryOptions({
    queryKey: organizationKeys.detail(id),
    queryFn: ({ signal }) => api.get<OrganizationDetail>(`/api/app/organizations/${id}`, { signal }),
  });

export const organizationNavigationQuery = (id: string) =>
  queryOptions({
    queryKey: organizationKeys.navigation(id),
    queryFn: ({ signal }) =>
      api.get<OrganizationNavigation>(`/api/app/organizations/${id}/navigation`, { signal }),
    staleTime: 0,
  });

export const membersQuery = (id: string) =>
  queryOptions({
    queryKey: organizationKeys.members(id),
    queryFn: ({ signal }) =>
      api
        .get<{ members: Member[] }>(`/api/app/organizations/${id}/members`, { signal })
        .then((res) => res.members),
  });

export const useCreateOrganization = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      api.post<Organization & { workspaceId: string }>("/api/app/organizations", { name }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: organizationKeys.all }),
  });
};

export interface OrganizationUpdate {
  name?: string;
  displayTimeZone?: string | null;
  isAISmartToolsEnabled?: boolean;
}

export const useUpdateOrganization = (id: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: OrganizationUpdate) => api.patch<Organization>(`/api/app/organizations/${id}`, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: organizationKeys.all }),
  });
};

export const useDeleteOrganization = (id: string) =>
  useMutation({ mutationFn: () => api.delete<undefined>(`/api/app/organizations/${id}`) });

export const useLeaveOrganization = (id: string) =>
  useMutation({ mutationFn: () => api.post<undefined>(`/api/app/organizations/${id}/leave`) });

export const useChangeMemberRole = (organizationId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: OrganizationRole }) =>
      api.patch<Membership>(`/api/app/organizations/${organizationId}/members/${userId}`, { role }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organizationId) }),
  });
};

export const useRemoveMember = (organizationId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) =>
      api.delete<undefined>(`/api/app/organizations/${organizationId}/members/${userId}`),
    onSettled: () => queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organizationId) }),
  });
};
