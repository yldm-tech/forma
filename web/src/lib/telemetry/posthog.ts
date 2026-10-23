import type { PostHog, PostHogConfig } from "posthog-js";
import { type PostHogClientConfig, SENSITIVE_QUERY_PARAMS } from "./runtime-config";

/**
 * Product analytics in the browser, forma2's lib/posthog/client.ts: the only module that loads posthog-js, and it loads it lazily. A static import would put the SDK into the entry bundle of every page on every install, including the ones without POSTHOG_KEY; here it is downloaded only once PostHogIdentify starts it with the key the server configured. Every other caller goes through the helpers below and finds no instance until then, which is the no-op they already handle.
 */

let instance: PostHog | null = null;
let loading: Promise<PostHog | null> | null = null;

/** Set once PostHog is initialised. Null on an install with no key, forever. */
export const getPostHogClient = (): PostHog | null => instance;

/**
 * forma2's options for posthog-js, plus what keeps tokens, email addresses and response answers out of PostHog: the values of SENSITIVE_QUERY_PARAMS are masked in every captured URL, autocapture records no element text, and session recordings mask all text as Sentry's replays do.
 */
export const postHogOptions = (config: PostHogClientConfig): Partial<PostHogConfig> => ({
  api_host: config.apiHost,
  ui_host: config.uiHost,
  defaults: "2026-01-30",
  capture_exceptions: true,
  debug: import.meta.env.DEV,
  mask_all_text: true,
  mask_personal_data_properties: true,
  custom_personal_data_properties: SENSITIVE_QUERY_PARAMS,
  session_recording: {
    blockSelector: "iframe[src*='cdn-plain']",
    maskTextSelector: "*",
  },
});

/** Loads and initialises posthog-js once; concurrent callers share one import and one init, since a second init would reset the SDK's state. */
export const initPostHogClient = async (
  key: string,
  options: Partial<PostHogConfig>
): Promise<PostHog | null> => {
  if (instance) return instance;
  loading ??= import("posthog-js").then(({ default: posthog }) => {
    if (!posthog.__loaded) posthog.init(key, options);
    instance = posthog;
    return posthog;
  });
  return loading;
};

export type PostHogClientEventProperties = Record<
  string,
  string | number | boolean | string[] | null | undefined
>;

/** Captures a product event when PostHog is initialised and drops it otherwise. For an event behind a user action that is enough: by the time someone clicks, the SDK has loaded. */
export const capturePostHogClientEvent = (event: string, properties?: PostHogClientEventProperties): void => {
  instance?.capture(event, properties);
};

// PostHogIdentify starts the SDK from an effect in the authenticated layout, and React runs a child's effects before its parent's, so an event captured on mount finds the SDK not yet loaded on a fresh page load. Poll briefly, then give up (no POSTHOG_KEY on this install).
const READY_POLL_INTERVAL_MS = 50;
const READY_POLL_TIMEOUT_MS = 5000;

const noop = (): void => undefined;

/** Runs action as soon as PostHog is ready, for work that happens on mount rather than on a user action; at once when it already is. Returns a canceller for effect cleanup, so an unmount or a strict-mode re-run never reports twice. */
export const whenPostHogReady = (action: (posthog: PostHog) => void): (() => void) => {
  if (instance) {
    action(instance);
    return noop;
  }
  const intervalId = setInterval(() => {
    if (!instance) return;
    clearTimeout(timeoutId);
    clearInterval(intervalId);
    action(instance);
  }, READY_POLL_INTERVAL_MS);
  const timeoutId = setTimeout(() => clearInterval(intervalId), READY_POLL_TIMEOUT_MS);
  return () => {
    clearInterval(intervalId);
    clearTimeout(timeoutId);
  };
};

/** Captures an event as soon as PostHog is ready, for events that fire on mount; see whenPostHogReady for the canceller. */
export const capturePostHogClientEventWhenReady = (
  event: string,
  properties?: PostHogClientEventProperties
): (() => void) => whenPostHogReady((posthog) => posthog.capture(event, properties));
