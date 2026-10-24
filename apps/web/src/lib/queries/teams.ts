import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { organizationKeys } from "./organizations";

export type TeamRole = "admin" | "contributor";
export type TeamPermission = "read" | "readWrite" | "manage";

export interface UserTeam {
  id: string;
  name: string;
  userRole: TeamRole;
  memberCount: number;
}

export interface OtherTeam {
  id: string;
  name: string;
  memberCount: number;
}

/** The caller's teams with their role in each, and the organization's other teams. */
export interface Teams {
  userTeams: UserTeam[];
  otherTeams: OtherTeam[];
}

export interface TeamDetails {
  id: string;
  name: string;
  organizationId: string;
  members: { userId: string; name: string; role: TeamRole }[];
  workspaces: { workspaceId: string; workspaceName: string; permission: TeamPermission }[];
}

export interface TeamUpdate {
  name: string;
  members: { userId: string; role: TeamRole }[];
  workspaces: { workspaceId: string; permission: TeamPermission }[];
}

export const teamKeys = {
  list: (organizationId: string) => [...organizationKeys.detail(organizationId), "teams"] as const,
  detail: (id: string) => ["teams", id] as const,
};

export const teamsQuery = (organizationId: string) =>
  queryOptions({
    queryKey: teamKeys.list(organizationId),
    queryFn: ({ signal }) => api.get<Teams>(`/api/app/organizations/${organizationId}/teams`, { signal }),
  });

export const teamQuery = (id: string) =>
  queryOptions({
    queryKey: teamKeys.detail(id),
    queryFn: ({ signal }) => api.get<TeamDetails>(`/api/app/teams/${id}`, { signal }),
    staleTime: 0,
  });

export const teamRoleQuery = (id: string) =>
  queryOptions({
    queryKey: [...teamKeys.detail(id), "role"],
    queryFn: ({ signal }) =>
      api.get<{ role: TeamRole | null }>(`/api/app/teams/${id}/role`, { signal }).then((res) => res.role),
    staleTime: 0,
  });

export const useCreateTeam = (organizationId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      api.post<{ id: string }>(`/api/app/organizations/${organizationId}/teams`, { name }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: teamKeys.list(organizationId) }),
  });
};

export const useUpdateTeam = (organizationId: string, id: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: TeamUpdate) => api.put<TeamDetails>(`/api/app/teams/${id}`, input),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: teamKeys.list(organizationId) }),
        queryClient.invalidateQueries({ queryKey: teamKeys.detail(id) }),
      ]),
  });
};

export const useDeleteTeam = (organizationId: string, id: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api.delete<undefined>(`/api/app/teams/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: teamKeys.list(organizationId) }),
  });
};
