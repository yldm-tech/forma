// The browser's telemetry settings. The Go server writes them into index.html from SENTRY_DSN, SENTRY_ENVIRONMENT and POSTHOG_KEY (server/internal/webui/webui.go, withRuntimeConfig), as forma2's root layout did, so one build serves every deployment. An install without them gets neither global, and neither SDK is downloaded.

/** forma2's TSentryClientRuntimeConfig. */
export interface SentryClientConfig {
  dsn: string;
  release?: string;
  environment?: string;
}

/** posthog-js's project key, the first-party /ingest proxy it sends through, and PostHog's app for links. */
export interface PostHogClientConfig {
  key: string;
  apiHost: string;
  uiHost: string;
}

declare global {
  interface Window {
    __formaSentryClientConfig?: SentryClientConfig;
    __formaPostHogConfig?: PostHogClientConfig;
  }
}

/** The Sentry settings, when the server configured a DSN. */
export const sentryClientConfig = (): SentryClientConfig | undefined => {
  const config = window.__formaSentryClientConfig;
  return config?.dsn ? config : undefined;
};

/** The PostHog settings, when the server configured a project key. */
export const postHogClientConfig = (): PostHogClientConfig | undefined => {
  const config = window.__formaPostHogConfig;
  return config?.key ? config : undefined;
};

/**
 * Query parameters whose values never leave the browser in telemetry: the one-time tokens of invitations, password resets and email verification, the email address some auth pages carry, the sign-in return path (which can hold any of those), and the pending OAuth authorization the consent page answers.
 */
export const SENSITIVE_QUERY_PARAMS = [
  "token",
  "inviteToken",
  "email",
  "callbackUrl",
  "request_id",
  "redirect_uri",
  "code",
  "state",
];

/** What a masked value is replaced with, posthog-js's own marker. */
export const MASKED = "<MASKED>";

/** The URL with the values of SENSITIVE_QUERY_PARAMS replaced by MASKED; everything else, the fragment included, is left as it was written. */
export const maskSensitiveQuery = (url: string): string => {
  const queryStart = url.indexOf("?");
  if (queryStart < 0) return url;
  const hashStart = url.indexOf("#", queryStart);
  const queryEnd = hashStart < 0 ? url.length : hashStart;
  const pairs = url.slice(queryStart + 1, queryEnd).split("&");
  const masked = pairs.map((pair) => {
    const separator = pair.indexOf("=");
    const rawKey = separator < 0 ? pair : pair.slice(0, separator);
    let key = rawKey;
    try {
      key = decodeURIComponent(rawKey.replaceAll("+", " "));
    } catch {
      // A malformed escape is compared as written.
    }
    return SENSITIVE_QUERY_PARAMS.includes(key) ? `${rawKey}=${MASKED}` : pair;
  });
  return url.slice(0, queryStart + 1) + masked.join("&") + url.slice(queryEnd);
};
