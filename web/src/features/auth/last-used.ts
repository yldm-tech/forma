// forma2's FORMA_LOGGED_IN_WITH_LS: the sign-in method this browser used last, which the sign-in page marks "Last used". Storage can be unavailable (private mode, blocked site data), and the badge is a convenience, so a failure only loses it.

export type SignInMethod = "Email" | "Google" | "Github" | "Azure" | "OpenID";

export const LAST_USED_KEY = "forma-logged-in-with";

export const readLastUsed = (): string => {
  try {
    return window.localStorage.getItem(LAST_USED_KEY) ?? "";
  } catch {
    return "";
  }
};

export const rememberLastUsed = (method: SignInMethod): void => {
  try {
    window.localStorage.setItem(LAST_USED_KEY, method);
  } catch {
    // Storage is unavailable; the badge is simply not shown next time.
  }
};
