import { type QueryClient, hashKey, queryOptions } from "@tanstack/react-query";
import { ApiError, api } from "../../lib/api";

// The /api/app/auth and invite endpoints the signed-out screens call (apps/server/internal/auth/handler.go, sso_handler.go, twofactor_handler.go and apps/server/internal/invite/handler.go).

export type SSOProvider = "google" | "github" | "azuread" | "openid" | "saml";

/** GET /api/app/auth/options: what this instance offers on the sign-in and sign-up pages. */
export interface AuthOptions {
  emailAuthEnabled: boolean;
  signupEnabled: boolean;
  passwordResetEnabled: boolean;
  emailVerificationRequired: boolean;
  smtpConfigured: boolean;
  turnstileSiteKey?: string;
  ssoProviders: SSOProvider[];
  oidcDisplayName?: string;
  /** TERMS_URL and PRIVACY_URL, linked under the sign-up form when set. */
  termsUrl?: string;
  privacyUrl?: string;
  /** Forma Cloud offers the product update emails at sign-up instead of the security ones. */
  isFormaCloud: boolean;
  /** Whether Turnstile or reCAPTCHA keys are configured, so the survey editor offers spam protection. */
  spamProtectionAvailable: boolean;
  /** The service spam protection uses: Turnstile whenever it is configured, otherwise reCAPTCHA; absent when neither is. */
  spamProtectionProvider?: "turnstile" | "recaptcha";
}

/** GET /api/app/auth/me. */
export interface SessionUser {
  id: string;
  name: string;
  email: string;
  emailVerified: boolean;
  locale: string;
  identityProvider: string;
  hasPassword: boolean;
  twoFactorEnabled: boolean;
  createdAt: string;
  lastLoginAt: string | null;
}

export type SignInStatus = "signed_in" | "two_factor_required";

export type InviteStatus = "valid" | "not_found" | "expired" | "email_mismatch";

/** What an invite link is. email, organizationName and inviterName come with a working link (valid, or another account's on email_mismatch), never with an unknown or expired one. */
export interface InviteInspection {
  status: InviteStatus;
  email?: string;
  organizationName?: string;
  inviterName?: string;
}

export interface SignupInput {
  name: string;
  email: string;
  password: string;
  inviteToken?: string;
  userLocale?: string;
  turnstileToken?: string;
  subscribeToSecurityUpdates?: boolean;
  subscribeToProductUpdates?: boolean;
  /** The browser's IANA zone, which the organization an uninvited sign-up creates displays dates and schedules surveys in; the server drops one it cannot load. */
  timeZone?: string;
}

export const authKeys = {
  session: ["auth", "session"] as const,
  options: ["auth", "options"] as const,
};

/** The signed-in user, or null when the browser has no valid session. */
export const sessionQuery = queryOptions({
  queryKey: authKeys.session,
  queryFn: async ({ signal }): Promise<SessionUser | null> => {
    try {
      return await api.get<SessionUser>("/api/app/auth/me", { signal });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) return null;
      throw error;
    }
  },
});

export const authOptionsQuery = queryOptions({
  queryKey: authKeys.options,
  queryFn: ({ signal }) => api.get<AuthOptions>("/api/app/auth/options", { signal }),
});

/** Drops everything cached for the previous session after anything that signs the browser in or out, so the next guard asks the server again and no screen shows another user's data: the shell's own copy of GET /api/app/auth/me and every organization and workspace query go too. Only the instance's sign-in options outlive it. */
export const resetSession = (queryClient: QueryClient) =>
  queryClient.removeQueries({ predicate: (query) => query.queryHash !== hashKey(authKeys.options) });

export const authApi = {
  signIn: (email: string, password: string) =>
    api.post<{ status: SignInStatus }>("/api/app/auth/sign-in", { email, password }),
  verifyTotp: (code: string) =>
    api.post<{ status: SignInStatus }>("/api/app/auth/two-factor/verify-totp", { code }),
  verifyBackupCode: (code: string) =>
    api.post<{ status: SignInStatus }>("/api/app/auth/two-factor/verify-backup-code", { code }),
  signOut: () => api.post<void>("/api/app/auth/sign-out"),
  signup: (input: SignupInput) => api.post<{ success: boolean }>("/api/app/auth/signup", input),
  resendVerification: (email: string) =>
    api.post<{ success: boolean }>("/api/app/auth/verification/resend", { email }),
  verifyEmail: (token: string) =>
    api.post<{ signedIn: boolean }>("/api/app/auth/verification/verify", { token }),
  forgotPassword: (email: string) =>
    api.post<{ success: boolean }>("/api/app/auth/password/forgot", { email }),
  resetPassword: (token: string, password: string) =>
    api.post<{ success: boolean }>("/api/app/auth/password/reset", { token, password }),
  confirmEmailChange: (token: string) => api.post<{ email: string }>("/api/app/auth/email/verify", { token }),
  startSSO: (provider: SSOProvider, callbackUrl: string) =>
    api.post<{ url: string }>("/api/app/auth/sso/sign-in", { provider, callbackUrl }),
  completeSSORecovery: (token: string) =>
    api.post<{ redirectUrl: string; status: SignInStatus }>("/api/app/auth/sso/recovery/complete", { token }),
  inspectInvite: (token: string) => api.post<InviteInspection>("/api/app/invites/inspect", { token }),
  acceptInvite: (token: string) => api.post<{ organizationId: string }>("/api/app/invites/accept", { token }),
};

/** What an invite link is for the visitor; keyed by the signed-in user, whose address decides email_mismatch. A 4xx (a malformed token) is the server's final answer. */
export const inviteInspectionQuery = (token: string, userId: string | null) =>
  queryOptions({
    queryKey: ["invites", "inspect", token, userId] as const,
    queryFn: () => authApi.inspectInvite(token),
    staleTime: Infinity,
  });
