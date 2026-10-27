import { type QueryClient, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { surveyKeys } from "../../features/surveys/list/query";
import { api } from "../api";
import type { OrganizationRole } from "../roles";

export interface Organization {
  id: string;
  createdAt: string;
  updatedAt: string;
  name: string;
  isAISmartToolsEnabled: boolean;
  /** The "Allow AI to read responses" switch: whether features that send responses to the AI provider (the open-text summary) may run, on top of AI smart tools. */
  isAIResponseReadingEnabled: boolean;
  /** An IANA zone; null means UTC. */
  displayTimeZone: string | null;
  /** The IANA zone the organization's scheduled surveys open and close in; null means the instance's default. */
  surveySchedulingTimeZone: string | null;
  /** The email logo and survey favicon that replace Forma's, forma2's TOrganizationWhitelabel. */
  whitelabel?: { logoUrl?: string | null; faviconUrl?: string | null } | null;
  /** Null while two-factor authentication is optional. Once an owner requires it, the end of the grace period: members who sign in with a password and have not turned it on are reminded until then and refused the organization from then on. */
  twoFactorEnforcedAt: string | null;
}

/** A user's two-factor status: an authenticator turned on, none although they sign in with a password, or none needed because they sign in through SSO, which a requirement exempts. */
export type TwoFactorStatus = "enabled" | "disabled" | "sso";

/** An organization in the caller's list, with the caller's role in it. */
export interface OrganizationSummary extends Organization {
  role: OrganizationRole;
  /** When the organization suspended the caller's access; null while they have it. */
  suspendedAt: string | null;
}

export interface Membership {
  organizationId: string;
  userId: string;
  accepted: boolean;
  role: OrganizationRole;
  /** When an owner or manager suspended the member's access to the organization; null while they have it. A suspended member keeps only the organization's name, their membership and leaving it. */
  suspendedAt: string | null;
}

/** When scheduled surveys open and close: on the selected day at the instance's local time, in the organization's zone or, without one, the instance's (SURVEY_SCHEDULING_TIME_ZONE). */
export interface SurveyScheduling {
  /** The zone in effect. */
  timeZone: string;
  localHour: number;
  localMinute: number;
  /** The instance's zone, what an organization without its own uses. */
  instanceTimeZone: string;
}

export interface OrganizationDetail extends Organization {
  membership: Membership;
  /** Whether the caller's role may manage members and invites under USER_MANAGEMENT_MINIMUM_ROLE. */
  userManagementAllowed: boolean;
  surveyScheduling: SurveyScheduling;
  /** The caller's own two-factor status. */
  twoFactorStatus: TwoFactorStatus;
  /** The organization's two-factor requirement refuses the caller now: its grace period is over and they have not turned it on. */
  twoFactorBlocked: boolean;
}

export interface Member {
  userId: string;
  name: string;
  email: string;
  accepted: boolean;
  role: OrganizationRole;
  /** The account's own switch, which only the instance's operator turns off; it signs in nowhere while off. */
  isActive: boolean;
  /** When the member's access to this organization was suspended; null while they have it. */
  suspendedAt: string | null;
  /** Sent to owners and managers only. */
  twoFactor?: TwoFactorStatus;
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
  /** The user belongs to an organization the server's authorization has not caught up with yet; the rest of the answer leaves it out. */
  pending?: boolean;
}

export const organizationKeys = {
  all: ["organizations"] as const,
  detail: (id: string) => ["organizations", id] as const,
  navigation: (id: string) => ["organizations", id, "navigation"] as const,
  members: (id: string) => ["organizations", id, "members"] as const,
  retention: (id: string) => ["organizations", id, "retention"] as const,
  retentionPreview: (id: string, windows: RetentionWindows) =>
    ["organizations", id, "retention", "preview", windows] as const,
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

/** Creates an organization in the creator's browser time zone, which the server stores as its display and survey scheduling zone (an unknown zone is ignored there), so its surveys do not open and close at midnight in the instance's zone. */
export const useCreateOrganization = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      api.post<Organization & { workspaceId: string }>("/api/app/organizations", {
        name,
        timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: organizationKeys.all }),
  });
};

export interface OrganizationUpdate {
  name?: string;
  displayTimeZone?: string | null;
  surveySchedulingTimeZone?: string | null;
  isAISmartToolsEnabled?: boolean;
  isAIResponseReadingEnabled?: boolean;
  /** Owners only. Turning it on needs twoFactorGracePeriodDays, and restarts the grace period from now. */
  requireTwoFactor?: boolean;
  twoFactorGracePeriodDays?: number;
}

/** After the organization's survey scheduling zone changed, the server has moved every scheduled publish and close date to the new zone. Cached surveys still hold the old instants, and the editor's never refetches on its own (its baseline would be sent back with the old zone's dates), so they are dropped; workspaces, which tell the editor the zone, are read again. */
export const forgetSurveySchedules = (queryClient: QueryClient): Promise<void> => {
  queryClient.removeQueries({ queryKey: surveyKeys.all, type: "inactive" });
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: surveyKeys.all }),
    queryClient.invalidateQueries({ queryKey: ["workspaces"] }),
  ]).then(() => undefined);
};

export const useUpdateOrganization = (id: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: OrganizationUpdate) => api.patch<Organization>(`/api/app/organizations/${id}`, input),
    onSuccess: (_, input) =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: organizationKeys.all }),
        input.surveySchedulingTimeZone !== undefined ? forgetSurveySchedules(queryClient) : undefined,
      ]),
  });
};

/** The organization's retention windows in days; null keeps that data forever. */
export interface RetentionWindows {
  /** Finished workflow runs (completed, failed, canceled) and their logs. */
  workflowRunRetentionDays: number | null;
  /** Survey impressions that never became a response. */
  displayRetentionDays: number | null;
}

/** What one pass of a sweep did: matched is what a dry run counted, deleted what a real one removed, cappedUnits the parts it left for the next day. */
export interface RetentionPass {
  matched: number;
  deleted: number;
  cappedUnits: number;
}

/** The organization's last retention sweep, under the windows it records. */
export interface RetentionSweep extends RetentionWindows {
  sweptAt: string;
  /** The sweep only counted (each pass's matched) and deleted nothing. */
  dryRun: boolean;
  /** A pass failed: the counts are what the sweep got through before it, and the rest waits for the next sweep. */
  stoppedEarly: boolean;
  workflowRuns: RetentionPass;
  displays: RetentionPass;
}

/** The organization's Data retention card, which only owners read and change. */
export interface Retention extends RetentionWindows {
  /** The instance's RETENTION_SWEEP_DRY_RUN: its sweep only counts. Only the instance's operator changes it. */
  dryRun: boolean;
  /** The next daily sweep, at midnight UTC. */
  nextSweepAt: string;
  /** The last sweep with the windows the organization has now; null when none has run with them. */
  lastSweep: RetentionSweep | null;
}

export const retentionQuery = (id: string) =>
  queryOptions({
    queryKey: organizationKeys.retention(id),
    queryFn: ({ signal }) => api.get<Retention>(`/api/app/organizations/${id}/retention`, { signal }),
  });

/** What the sweep would delete as of now under windows the organization may not have yet; null is a window that keeps forever and is not counted. */
export interface RetentionPreview {
  workflowRuns: number | null;
  displays: number | null;
}

export const retentionPreviewQuery = (id: string, windows: RetentionWindows) =>
  queryOptions({
    queryKey: organizationKeys.retentionPreview(id, windows),
    queryFn: ({ signal }) => {
      const params = new URLSearchParams();
      if (windows.workflowRunRetentionDays !== null)
        params.set("workflowRunRetentionDays", String(windows.workflowRunRetentionDays));
      if (windows.displayRetentionDays !== null)
        params.set("displayRetentionDays", String(windows.displayRetentionDays));
      return api.get<RetentionPreview>(
        `/api/app/organizations/${id}/retention/preview?${params.toString()}`,
        {
          signal,
        }
      );
    },
  });

/** Replaces both windows; the answer is the card as it now is. */
export const useUpdateRetention = (id: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (windows: RetentionWindows) =>
      api.put<Retention>(`/api/app/organizations/${id}/retention`, windows),
    onSuccess: (retention) => queryClient.setQueryData(organizationKeys.retention(id), retention),
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

/** Suspends a member's access to the organization, or restores it; the server answers with the membership as it now is. */
export const useSetMemberSuspension = (organizationId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, suspended }: { userId: string; suspended: boolean }) => {
      const path = `/api/app/organizations/${organizationId}/members/${userId}/suspension`;
      return suspended ? api.post<Membership>(path) : api.delete<Membership>(path);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organizationId) }),
  });
};
