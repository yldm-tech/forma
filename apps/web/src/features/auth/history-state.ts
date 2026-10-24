import type { HistoryState } from "@tanstack/react-router";

// The address a sign-up used travels to the notice page in the history entry's state, never in the URL: forma2 encrypted it into a token for the same reason, to keep it out of the address bar, history sync and server logs. The state survives a reload of that entry.
const SIGNUP_EMAIL = "signupEmail";

export const withSignupEmail =
  (email: string) =>
  (previous: HistoryState): HistoryState =>
    ({ ...previous, [SIGNUP_EMAIL]: email }) as HistoryState;

export const signupEmailFrom = (state: HistoryState | undefined): string | undefined => {
  const value = (state as Record<string, unknown> | undefined)?.[SIGNUP_EMAIL];
  return typeof value === "string" ? value : undefined;
};
