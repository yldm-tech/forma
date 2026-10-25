import type { TOverlay, TPlacement } from "@forma/types/common";
import type { TLogo, TWorkspaceConfig, TWorkspaceStyling } from "@forma/types/workspace";
import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { OrganizationRole } from "../roles";
import { type SurveyScheduling, organizationKeys } from "./organizations";

/** One of a workspace's survey languages. */
export interface WorkspaceLanguage {
  id: string;
  createdAt: string;
  updatedAt: string;
  code: string;
  alias: string | null;
  workspaceId: string;
}

/** forma2's TWorkspace as the server sends it. */
export interface Workspace {
  id: string;
  createdAt: string;
  updatedAt: string;
  name: string;
  organizationId: string;
  styling: TWorkspaceStyling;
  recontactDays: number;
  inAppSurveyBranding: boolean;
  linkSurveyBranding: boolean;
  config: TWorkspaceConfig;
  placement: TPlacement;
  clickOutsideClose: boolean;
  overlay: TOverlay;
  languages: WorkspaceLanguage[];
  appSetupCompleted: boolean;
  logo: TLogo | null;
  customHeadScripts: string | null;
}

/** forma2's ZWorkspaceUpdateInput as far as it maps onto stored fields; config is merged onto the stored one. */
export interface WorkspaceUpdate {
  name?: string;
  recontactDays?: number;
  inAppSurveyBranding?: boolean;
  linkSurveyBranding?: boolean;
  config?: TWorkspaceConfig;
  placement?: TPlacement;
  clickOutsideClose?: boolean;
  overlay?: TOverlay;
  styling?: TWorkspaceStyling;
  logo?: TLogo;
  customHeadScripts?: string | null;
}

/** Where the browser goes after deleting a workspace: another workspace of the organization, its onboarding, or the root. */
export interface WorkspaceDeletion {
  workspace: Workspace;
  destination: { workspaceId: string | null; path: string };
}

/** What the app-connection page shows to install the SDK. */
export interface AppConnection {
  workspaceId: string;
  appUrl: string;
  appSetupCompleted: boolean;
}

/** How far the client API trusts the SDK with the workspace's contacts. The identity secret itself is shown only when it is generated. */
export interface SdkIdentity {
  hasSecret: boolean;
  /** An identify without a valid userHash is refused. */
  required: boolean;
  /** Attribute keys the SDK never writes. */
  protectedAttributeKeys: string[];
}

/** A newly generated identity secret, shown this once. */
export interface SdkIdentitySecret extends SdkIdentity {
  secret: string;
}

/** A team with access to the workspace. */
export interface WorkspaceTeam {
  id: string;
  name: string;
  permission: "read" | "readWrite" | "manage";
  memberCount: number;
}

/** What the caller may do in a workspace, derived from the decisions the endpoints enforce. */
export interface WorkspaceAccess {
  role: OrganizationRole | "";
  canWrite: boolean;
  canManage: boolean;
  isReadOnly: boolean;
}

/** The workspace with its settings (forma2's TWorkspace, timestamps as sent) and the caller's access to it. */
export interface WorkspaceDetail extends Workspace {
  access: WorkspaceAccess;
  /** When the workspace's scheduled surveys open and close, for the survey editor. */
  surveyScheduling?: SurveyScheduling;
}

/** The SPA's own error code for a billing member asking for a workspace; details.redirect is where to send them. */
export const BILLING_ROLE_CODE = "billing_role";

export const workspaceKeys = {
  detail: (id: string) => ["workspaces", id] as const,
  appConnection: (id: string) => ["workspaces", id, "app-connection"] as const,
  sdkIdentity: (id: string) => ["workspaces", id, "sdk-identity"] as const,
  teams: (id: string) => ["workspaces", id, "teams"] as const,
  languageSurveys: (id: string, languageId: string) =>
    ["workspaces", id, "languages", languageId, "surveys"] as const,
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
        }>(
          `/api/app/organizations/${organizationId}/workspaces${access === "write" ? "?access=write" : ""}`,
          { signal }
        )
        .then((res) => res.workspaces),
  });

export const useCreateWorkspace = (organizationId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; teamIds?: string[] }) =>
      api.post<Workspace>(`/api/app/organizations/${organizationId}/workspaces`, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organizationId) }),
  });
};

export const appConnectionQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: workspaceKeys.appConnection(workspaceId),
    queryFn: ({ signal }) =>
      api.get<AppConnection>(`/api/app/workspaces/${workspaceId}/app-connection`, { signal }),
  });

export const workspaceTeamsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: workspaceKeys.teams(workspaceId),
    queryFn: ({ signal }) => api.get<WorkspaceTeam[]>(`/api/app/workspaces/${workspaceId}/teams`, { signal }),
  });

/** forma2's updateWorkspaceAction. */
export const updateWorkspace = (workspaceId: string, input: WorkspaceUpdate) =>
  api.patch<Workspace>(`/api/app/workspaces/${workspaceId}`, input);

/** updateWorkspace, refreshing the workspace detail with what the server stored. */
export const useUpdateWorkspace = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: WorkspaceUpdate) => updateWorkspace(workspaceId, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: workspaceKeys.detail(workspaceId) }),
  });
};

export const useDeleteWorkspace = (workspaceId: string) =>
  useMutation({
    mutationFn: (confirmationName: string) =>
      api.post<WorkspaceDeletion>(`/api/app/workspaces/${workspaceId}/delete`, { confirmationName }),
  });

/** The names of the surveys that use a language, which forma2 listed before letting it be removed. */
export const languageSurveysQuery = (workspaceId: string, languageId: string) =>
  queryOptions({
    queryKey: workspaceKeys.languageSurveys(workspaceId, languageId),
    queryFn: ({ signal }) =>
      api
        .get<{ surveys: string[] }>(`/api/app/workspaces/${workspaceId}/languages/${languageId}/surveys`, {
          signal,
        })
        .then((res) => res.surveys),
    staleTime: 0,
  });

export const createLanguage = (workspaceId: string, input: { code: string; alias: string | null }) =>
  api.post<WorkspaceLanguage>(`/api/app/workspaces/${workspaceId}/languages`, input);

export const updateLanguage = (workspaceId: string, languageId: string, alias: string | null) =>
  api.patch<WorkspaceLanguage>(`/api/app/workspaces/${workspaceId}/languages/${languageId}`, { alias });

export const deleteLanguage = (workspaceId: string, languageId: string) =>
  api.delete<WorkspaceLanguage>(`/api/app/workspaces/${workspaceId}/languages/${languageId}`);

export const sdkIdentityQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: workspaceKeys.sdkIdentity(workspaceId),
    queryFn: ({ signal }) =>
      api.get<SdkIdentity>(`/api/app/workspaces/${workspaceId}/sdk-identity`, { signal }),
  });

/** Whether verified identities are required and which attributes are protected; a field left out is kept. */
export const useUpdateSdkIdentity = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { required?: boolean; protectedAttributeKeys?: string[] }) =>
      api.patch<SdkIdentity>(`/api/app/workspaces/${workspaceId}/sdk-identity`, input),
    onSuccess: (data) => queryClient.setQueryData(workspaceKeys.sdkIdentity(workspaceId), data),
  });
};

/** A new identity secret, replacing the old one at once. */
export const useRotateSdkIdentitySecret = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<SdkIdentitySecret>(`/api/app/workspaces/${workspaceId}/sdk-identity/secret`),
    onSuccess: ({ secret: _secret, ...view }) =>
      queryClient.setQueryData(workspaceKeys.sdkIdentity(workspaceId), view),
  });
};

/** Removes the identity secret, which also stops requiring verified identities. */
export const useRemoveSdkIdentitySecret = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api.delete<SdkIdentity>(`/api/app/workspaces/${workspaceId}/sdk-identity/secret`),
    onSuccess: (data) => queryClient.setQueryData(workspaceKeys.sdkIdentity(workspaceId), data),
  });
};
