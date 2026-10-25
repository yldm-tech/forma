import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { forgetWorkspace } from "../storage";
import { organizationKeys } from "./organizations";

export type IdentityProvider = "email" | "sso";

/** The signed-in user, GET /api/app/auth/me. */
export interface Me {
  id: string;
  name: string;
  email: string;
  emailVerified: boolean;
  locale: string;
  identityProvider: IdentityProvider;
  twoFactorEnabled: boolean;
  createdAt: string;
  lastLoginAt: string | null;
}

/** What the instance offers, GET /api/app/auth/options. */
export interface AuthOptions {
  emailAuthEnabled: boolean;
  signupEnabled: boolean;
  passwordResetEnabled: boolean;
  emailVerificationRequired: boolean;
  smtpConfigured: boolean;
  turnstileSiteKey?: string;
  ssoProviders: string[];
  oidcDisplayName?: string;
  termsUrl?: string;
  privacyUrl?: string;
  isFormaCloud: boolean;
}

export interface NotificationSurvey {
  id: string;
  name: string;
  workspaceId: string;
  workspaceName: string;
  alert: boolean;
}

export interface NotificationOrganization {
  id: string;
  name: string;
  autoSubscribe: boolean;
  surveys: NotificationSurvey[];
}

/** The two kinds of switch on the notifications page, named as the unsubscribe links in emails name them. */
export type NotificationType = "alert" | "unsubscribedOrganizationIds";

export interface TwoFactorSetup {
  secret: string;
  keyUri: string;
  dataUri: string;
  backupCodes: string[];
}

/** One of the user's sessions, GET /api/app/auth/sessions. ipAddress and userAgent are what the browser sent when it signed in. */
export interface UserSession {
  id: string;
  /** The session of this browser. */
  current: boolean;
  createdAt: string;
  lastActiveAt: string;
  expiresAt: string;
  ipAddress: string | null;
  userAgent: string | null;
}

export type SignOutReason =
  | "user_initiated"
  | "account_deletion"
  | "email_change"
  | "session_timeout"
  | "forced_logout"
  | "password_reset";

export const authKeys = {
  me: ["auth", "me"] as const,
  options: ["auth", "options"] as const,
  notifications: ["auth", "notifications"] as const,
  sessions: ["auth", "sessions"] as const,
};

export const meQuery = queryOptions({
  queryKey: authKeys.me,
  queryFn: ({ signal }) => api.get<Me>("/api/app/auth/me", { signal }),
});

export const authOptionsQuery = queryOptions({
  queryKey: authKeys.options,
  queryFn: ({ signal }) => api.get<AuthOptions>("/api/app/auth/options", { signal }),
  staleTime: 5 * 60_000,
});

export const notificationsQuery = queryOptions({
  queryKey: authKeys.notifications,
  queryFn: ({ signal }) =>
    api
      .get<{ organizations: NotificationOrganization[] }>("/api/app/auth/me/notifications", { signal })
      .then((res) => res.organizations),
});

/** The user's unexpired sessions, the current one first. */
export const sessionsQuery = queryOptions({
  queryKey: authKeys.sessions,
  queryFn: ({ signal }) =>
    api.get<{ sessions: UserSession[] }>("/api/app/auth/sessions", { signal }).then((res) => res.sessions),
});

/**
 * Ends the session (the server records why) and forgets the remembered workspace, as forma2's useSignOut did with clearWorkspaceId. The caller navigates afterwards; the cached data of the signed-out user is dropped so the next user of this tab cannot see it.
 */
export const signOut = async (
  queryClient: ReturnType<typeof useQueryClient>,
  options: { reason?: SignOutReason; organizationId?: string } = {}
): Promise<void> => {
  forgetWorkspace();
  await api.post("/api/app/auth/sign-out", {
    reason: options.reason ?? "user_initiated",
    ...(options.organizationId ? { organizationId: options.organizationId } : {}),
  });
  queryClient.clear();
};

export const useUpdateProfile = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { name?: string; locale?: string }) => api.patch<Me>("/api/app/auth/me", input),
    onSuccess: (me) => queryClient.setQueryData(authKeys.me, me),
  });
};

export const useRequestEmailChange = () =>
  useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      api.post<undefined>("/api/app/auth/email/change", input),
  });

/** Replaces the password after checking the current one. The server signs every other session out, so the sessions list is refetched. */
export const useChangePassword = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { currentPassword: string; newPassword: string }) =>
      api.post<undefined>("/api/app/auth/password/change", input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: authKeys.sessions }),
  });
};

/** Signs one of the user's other sessions out. */
export const useRevokeSession = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete<undefined>(`/api/app/auth/sessions/${encodeURIComponent(id)}`),
    onSettled: () => queryClient.invalidateQueries({ queryKey: authKeys.sessions }),
  });
};

/** Signs every session but this browser's out. */
export const useRevokeOtherSessions = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<{ revoked: number }>("/api/app/auth/sessions/revoke-others"),
    onSettled: () => queryClient.invalidateQueries({ queryKey: authKeys.sessions }),
  });
};

export const useRequestPasswordResetLink = () =>
  useMutation({ mutationFn: () => api.post<{ success: boolean }>("/api/app/auth/password/reset-link") });

export const useSetNotification = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { type: NotificationType; id: string; enabled: boolean }) =>
      api.patch<undefined>("/api/app/auth/me/notifications", input),
    onSettled: () => queryClient.invalidateQueries({ queryKey: authKeys.notifications }),
  });
};

export const useSetupTwoFactor = () =>
  useMutation({
    mutationFn: (password: string) =>
      api.post<TwoFactorSetup>("/api/app/auth/two-factor/setup", { password }),
  });

export const useEnableTwoFactor = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (code: string) => api.post<{ message: string }>("/api/app/auth/two-factor/enable", { code }),
    // Enabling signs every other session out, and meets the two-factor requirement of the user's organizations, whose answers say whether it refuses them.
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: authKeys.me }),
        queryClient.invalidateQueries({ queryKey: authKeys.sessions }),
        queryClient.invalidateQueries({ queryKey: organizationKeys.all }),
      ]),
  });
};

export const useDisableTwoFactor = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { password: string; code?: string; backupCode?: string }) =>
      api.post<{ message: string }>("/api/app/auth/two-factor/disable", input),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: authKeys.me }),
        queryClient.invalidateQueries({ queryKey: organizationKeys.all }),
      ]),
  });
};

export const useDeleteAccount = () =>
  useMutation({
    mutationFn: (password: string) =>
      api.post<{ redirectUrl: string }>("/api/app/auth/account/delete", { password }),
  });

export const useRequestAccountDeletion = () =>
  useMutation({
    mutationFn: () => api.post<{ emailSent: boolean }>("/api/app/auth/account/delete/request"),
  });

/** Redeems the deletion link mailed by useRequestAccountDeletion, from the same user's session. */
export const useConfirmAccountDeletion = () =>
  useMutation({
    mutationFn: (token: string) =>
      api.post<{ redirectUrl: string }>("/api/app/auth/account/delete/confirm", { token }),
  });
