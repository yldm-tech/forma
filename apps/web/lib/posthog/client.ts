"use client";

import type { PostHog } from "posthog-js";

/**
 * The only module in the app that loads `posthog-js`, and it loads it lazily.
 *
 * A static `import posthog from "posthog-js"` puts 93 KB brotli — 290 KB parsed and executed — into
 * the entry bundle of every authenticated route, because the SDK lands in the chunk group of the
 * unconditionally-rendered layout clients. The `POSTHOG_KEY &&` guard in the app layout decides
 * whether the component *renders*; it cannot decide whether the module is *bundled*. On an install
 * with no key — which is this one — that is a third of the app-shell JavaScript downloaded, parsed
 * and executed to do nothing.
 *
 * So the import lives inside `initPostHogClient`, which only `PostHogIdentify` calls and only when
 * it has a key. Every other caller goes through the helpers below and finds `instance` null until
 * then, which is exactly the no-op they already handled.
 */

let instance: PostHog | null = null;
let loading: Promise<PostHog | null> | null = null;

/** Set once PostHog is initialised. Null on an install with no key, forever. */
export const getPostHogClient = (): PostHog | null => instance;

export const initPostHogClient = async (
  posthogKey: string,
  options: Parameters<PostHog["init"]>[1]
): Promise<PostHog | null> => {
  if (instance) return instance;

  // Concurrent callers share one import and one `init`; a second `init` would reset the SDK's state.
  loading ??= import("posthog-js").then(({ default: posthog }) => {
    if (!posthog.__loaded) {
      posthog.init(posthogKey, options);
    }
    instance = posthog;
    return posthog;
  });

  return loading;
};

export type TPostHogClientEventProperties = Record<
  string,
  string | number | boolean | string[] | null | undefined
>;

/**
 * Capture a product event from the browser when PostHog is initialised, and drop it otherwise.
 * PostHog is initialised by `PostHogIdentify` and is absent when `POSTHOG_KEY` is unset. For an
 * event behind a user action the guard is enough: by the time someone clicks, the SDK is loaded.
 */
export const capturePostHogClientEvent = (
  event: string,
  properties?: TPostHogClientEventProperties
): void => {
  instance?.capture(event, properties);
};

// `PostHogIdentify` initialises the SDK from a useEffect in a parent layout, and React runs a child's
// effects before its parent's, so an event captured on mount finds the SDK not yet loaded on a fresh
// page load. Poll briefly, then give up (no POSTHOG_KEY on this host).
const READY_POLL_INTERVAL_MS = 50;
const READY_POLL_TIMEOUT_MS = 5000;

const noop = (): void => undefined;

/**
 * Runs `action` as soon as PostHog is ready, for work that happens on mount rather than on a user
 * action. Runs at once when the SDK is already loaded. Returns a cancel function for effect cleanup,
 * so a component that unmounts, or re-runs its effect under strict mode, never double-reports.
 */
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
  const timeoutId = setTimeout(() => {
    clearInterval(intervalId);
  }, READY_POLL_TIMEOUT_MS);

  return () => {
    clearInterval(intervalId);
    clearTimeout(timeoutId);
  };
};

/**
 * Capture an event as soon as PostHog is ready, for events that fire on mount rather than on a user
 * action. See `whenPostHogReady` for the cancel-function contract.
 */
export const capturePostHogClientEventWhenReady = (
  event: string,
  properties?: TPostHogClientEventProperties
): (() => void) => whenPostHogReady((posthog) => posthog.capture(event, properties));

export type { TPostHogFeatureFlagContext, TPostHogFeatureFlagValue } from "./types";
