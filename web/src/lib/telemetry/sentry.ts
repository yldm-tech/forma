import type * as SentrySDK from "@sentry/react";
import type { Breadcrumb, BrowserOptions, Event } from "@sentry/react";
import { ApiError } from "../api";
import { maskSensitiveQuery, sentryClientConfig } from "./runtime-config";

/**
 * Browser error reporting, forma2's init-client-sentry.ts with @sentry/react. It runs only where the server configured SENTRY_DSN, and only there is the SDK downloaded: it is imported dynamically, so an install without a DSN never loads it.
 *
 * The bootstrap awaits initSentry before the first render, so errors thrown while rendering are captured. The server writes the configuration into the page ahead of the bundle (module scripts run after the document is parsed), so forma2's late-assignment hook for a config that arrives after the bundle has no case to handle here.
 */

let sdk: typeof SentrySDK | undefined;
let loading: Promise<void> | undefined;

/** Errors the app expects and shows to the user: an API answer below 500 (not found, forbidden, invalid input, rate limited), forma2's isExpectedError. The server reports its own 5xx answers; the browser reports them too, with the replay that led there. */
export const isExpectedError = (error: unknown): boolean => error instanceof ApiError && error.status < 500;

/** The record with the URLs in the named string fields masked. */
const maskFields = <T extends object>(record: T, fields: string[]): T => {
  const out = { ...record } as Record<string, unknown>;
  for (const field of fields) {
    const value = out[field];
    if (typeof value === "string") out[field] = maskSensitiveQuery(value);
  }
  return out as T;
};

/** A breadcrumb with the tokens and addresses in the URLs it carries masked: navigations (from, to) and requests (url). */
export const scrubBreadcrumb = (breadcrumb: Breadcrumb): Breadcrumb =>
  breadcrumb.data ? { ...breadcrumb, data: maskFields(breadcrumb.data, ["from", "to", "url"]) } : breadcrumb;

/** An error or replay event with the page URL, the referrer, its breadcrumbs and a replay's visited URLs masked. */
export const scrubEvent = <T extends Event>(event: T): T => {
  const out: T = { ...event };
  if (event.request) {
    out.request = maskFields(event.request, ["url"]);
    if (event.request.headers) out.request.headers = maskFields(event.request.headers, ["Referer"]);
  }
  if (event.breadcrumbs) out.breadcrumbs = event.breadcrumbs.map(scrubBreadcrumb);
  const urls = (event as { urls?: unknown }).urls;
  if (Array.isArray(urls)) {
    (out as { urls?: unknown }).urls = urls.map((url) =>
      typeof url === "string" ? maskSensitiveQuery(url) : url
    );
  }
  return out;
};

/** A replay's own navigation and request spans carry their URLs as description and previous; the DOM recording itself is masked by maskAllText and blockAllMedia. */
export const scrubRecordingEvent = <T>(event: T): T => {
  const data = (event as { data?: { tag?: unknown; payload?: Record<string, unknown> } }).data;
  const payload = data?.payload;
  if (data?.tag !== "performanceSpan" || !payload) return event;
  const masked = maskFields(payload, ["description"]);
  const spanData = payload.data;
  if (spanData && typeof spanData === "object") {
    masked.data = maskFields(spanData as Record<string, unknown>, ["previous"]);
  }
  return { ...event, data: { ...data, payload: masked } };
};

/** forma2's Sentry.init options: no tracing, no default PII, no client reports, replays of every errored session and a tenth of the rest with all text masked and media blocked. */
export const sentryOptions = (
  sentry: typeof SentrySDK,
  config: NonNullable<ReturnType<typeof sentryClientConfig>>
): BrowserOptions => ({
  dsn: config.dsn,
  release: config.release,
  environment: config.environment,
  // No tracing while Sentry does not support OpenTelemetry 2 (getsentry/sentry-javascript#15737), as in forma2.
  tracesSampleRate: 0,
  debug: false,
  sendDefaultPii: false,
  sendClientReports: false,
  replaysOnErrorSampleRate: 1.0,
  replaysSessionSampleRate: 0.1,
  integrations: [
    sentry.replayIntegration({
      maskAllText: true,
      blockAllMedia: true,
      beforeAddRecordingEvent: scrubRecordingEvent,
    }),
  ],
  beforeBreadcrumb: scrubBreadcrumb,
  beforeSend: (event, hint) => (isExpectedError(hint.originalException) ? null : event),
});

/** Starts the SDK once, when the server configured a DSN; without one it does nothing. It never rejects. */
export const initSentry = (): Promise<void> => {
  const config = sentryClientConfig();
  if (!config) return Promise.resolve();
  loading ??= import("@sentry/react").then(
    (sentry) => {
      sentry.init(sentryOptions(sentry, config));
      // Event processors also run for replay events, whose visited URLs beforeSend never sees.
      sentry.addEventProcessor((event) => scrubEvent(event));
      sdk = sentry;
    },
    // The app starts without error reporting rather than not at all when the SDK's chunk cannot be fetched.
    (error: unknown) => console.warn("Sentry could not be loaded", error)
  );
  return loading;
};

/** Reports an error the UI caught (a route's error screen), unless it is one the app expects; a no-op where Sentry is not configured. */
export const reportError = (error: unknown): void => {
  if (isExpectedError(error)) return;
  sdk?.captureException(error);
};
