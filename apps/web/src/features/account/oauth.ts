// The consent page's and the connected clients card's helpers, forma2's oauth-client-metadata.ts and consent-redirect.ts.

/** The host of a URL, or null when it is not one. */
export const hostOf = (value?: string | null): string | null => {
  if (!value) return null;
  try {
    return new URL(value).host;
  } catch {
    return null;
  }
};

/** Whether a host (with its port) is the user's own machine, where only a client they started can be listening. */
export const isLocalhostHost = (host: string | null): boolean => {
  if (!host) return false;
  if (host.startsWith("[::1]")) return host === "[::1]" || host.startsWith("[::1]:");
  const [hostname] = host.split(":");
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "::1";
};

const SCOPE_LABELS: Record<string, string> = {
  openid: "auth.oauth.scopes.openid",
  profile: "auth.oauth.scopes.profile",
  email: "auth.oauth.scopes.email",
  offline_access: "auth.oauth.scopes.offline_access",
  "surveys:read": "auth.oauth.scopes.surveys_read",
  "surveys:write": "auth.oauth.scopes.surveys_write",
  "workflows:read": "auth.oauth.scopes.workflows_read",
  "workflows:write": "auth.oauth.scopes.workflows_write",
  "responses:read": "auth.oauth.scopes.responses_read",
};

/** A scope as the user reads it; an unknown scope is shown as it is. */
export const scopeLabel = (scope: string, t: (key: string) => string): string => {
  const key = SCOPE_LABELS[scope];
  return key ? t(key) : scope;
};

// The browser is sent to the answer's URL, so a scheme that would run in this page is refused. Native MCP clients redirect to their own schemes and to loopback, so the safe set cannot be listed; the server already matched the URL against the client's registration.
const DANGEROUS_PROTOCOLS = new Set(["javascript:", "data:", "vbscript:", "file:", "blob:"]);

const asRedirect = (value: unknown): string | null => {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  if (trimmed === "") return null;
  let protocol: string;
  try {
    protocol = new URL(trimmed).protocol;
  } catch {
    return null;
  }
  return DANGEROUS_PROTOCOLS.has(protocol.toLowerCase()) ? null : trimmed;
};

/** Where a consent answer sends the browser: its url, else its redirect_uri, if either is a safe absolute URL. */
export const consentRedirectUrl = (data: unknown): string | null => {
  if (typeof data !== "object" || data === null) return null;
  const record = data as Record<string, unknown>;
  return asRedirect(record.url) ?? asRedirect(record.redirect_uri);
};
