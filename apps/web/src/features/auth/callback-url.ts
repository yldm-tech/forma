// Where the auth screens send the browser afterwards. The SPA is served from WEBAPP_URL, so the page's own origin is the origin forma2 validated callback URLs against.

const ABSOLUTE_URL = /^[a-zA-Z][a-zA-Z\d+\-.]*:/;

/**
 * forma2's getValidatedCallbackUrl: a root-relative path, or an absolute http(s) URL on the app's origin, without credentials and without a pathname starting with "//" (which a consumer that keeps only path, search and hash would turn into an off-origin navigation). Returns the absolute URL, or null.
 */
export const validatedCallbackUrl = (url: string | null | undefined, origin: string): string | null => {
  if (!url) return null;
  const isAbsolute = ABSOLUTE_URL.test(url);
  if (!isAbsolute && !url.startsWith("/")) return null;
  let parsed: URL;
  try {
    parsed = isAbsolute ? new URL(url) : new URL(url, origin);
  } catch {
    return null;
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null;
  if (parsed.origin !== new URL(origin).origin) return null;
  if (parsed.username || parsed.password) return null;
  if (parsed.pathname.startsWith("//")) return null;
  return parsed.toString();
};

/** The in-app path a validated callback URL points at, or "/" when there is none: forma2's getRelativeCallbackUrl. */
export const callbackPath = (url: string | null | undefined, origin: string): string => {
  const validated = validatedCallbackUrl(url, origin);
  if (!validated) return "/";
  const parsed = new URL(validated);
  return `${parsed.pathname}${parsed.search}${parsed.hash}` || "/";
};

/** The invite token carried by a callback URL that returns to /invite?token=..., which the sign-in page passes on to its sign-up link. */
export const inviteTokenFromCallbackUrl = (url: string | null | undefined, origin: string): string | null => {
  const validated = validatedCallbackUrl(url, origin);
  return validated ? new URL(validated).searchParams.get("token") : null;
};

/** The page that accepts an invite. */
export const inviteUrl = (inviteToken: string, origin: string): string => {
  const url = new URL("/invite", origin);
  url.searchParams.set("token", inviteToken);
  return url.toString();
};

/** forma2's getSsoReturnToUrl: the callback an SSO sign-in lands on, tagged with the page it started from. */
export const ssoReturnToUrl = (returnToUrl: string | undefined, source: "signin" | "signup"): string => {
  const url = new URL(returnToUrl ?? "/", "http://localhost");
  url.searchParams.set("source", source);
  if (!returnToUrl || returnToUrl.startsWith("/")) return `${url.pathname}${url.search}${url.hash}`;
  return url.toString();
};
