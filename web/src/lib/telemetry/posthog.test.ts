// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { SENSITIVE_QUERY_PARAMS } from "./runtime-config";

// forma2's lib/posthog/client.test.ts.

const posthog = vi.hoisted(() => ({
  __loaded: false,
  capture: vi.fn(),
  group: vi.fn(),
  identify: vi.fn(),
  init: vi.fn(),
  reset: vi.fn(),
}));

vi.mock("posthog-js", () => ({ default: posthog }));

const load = async () => {
  vi.resetModules();
  return import("./posthog");
};

beforeEach(() => {
  vi.clearAllMocks();
  posthog.__loaded = false;
});

afterEach(() => {
  vi.useRealTimers();
});

const source = Object.values(
  import.meta.glob<string>("./posthog.ts", { query: "?raw", import: "default", eager: true })
).join("");

describe("posthog-js stays out of the entry bundle", () => {
  // A static import anywhere in application code puts the SDK into the bundle every page loads, on installs without POSTHOG_KEY too. Only a dynamic import is split off.
  test("the module imports the SDK dynamically, and only for its types otherwise", () => {
    expect(source).toContain('import("posthog-js")');
    expect(source).toContain('import type { PostHog, PostHogConfig } from "posthog-js"');
    expect(source).not.toMatch(/^import posthog from "posthog-js";$/m);
  });
});

describe("postHogOptions", () => {
  test("are forma2's, sent through the first-party proxy", async () => {
    const { postHogOptions } = await load();

    expect(
      postHogOptions({ key: "phc_test", apiHost: "/ingest", uiHost: "https://eu.i.posthog.com" })
    ).toMatchObject({
      api_host: "/ingest",
      ui_host: "https://eu.i.posthog.com",
      defaults: "2026-01-30",
      capture_exceptions: true,
      session_recording: { blockSelector: "iframe[src*='cdn-plain']" },
    });
  });

  test("keep tokens, addresses and response answers out of PostHog", async () => {
    const { postHogOptions } = await load();

    const options = postHogOptions({
      key: "phc_test",
      apiHost: "/ingest",
      uiHost: "https://eu.i.posthog.com",
    });

    expect(options.mask_personal_data_properties).toBe(true);
    expect(options.custom_personal_data_properties).toEqual(SENSITIVE_QUERY_PARAMS);
    expect(options.mask_all_text).toBe(true);
    expect(options.session_recording?.maskTextSelector).toBe("*");
  });
});

describe("capturePostHogClientEvent", () => {
  test("drops the event when PostHog was never initialised", async () => {
    const { capturePostHogClientEvent } = await load();

    capturePostHogClientEvent("some_event", { a: 1 });

    expect(posthog.capture).not.toHaveBeenCalled();
  });

  test("captures once PostHog has been initialised", async () => {
    const { capturePostHogClientEvent, initPostHogClient } = await load();
    await initPostHogClient("phc_test", {});

    capturePostHogClientEvent("some_event", { a: 1 });

    expect(posthog.capture).toHaveBeenCalledWith("some_event", { a: 1 });
  });
});

describe("initPostHogClient", () => {
  test("initialises the SDK and exposes it", async () => {
    const { getPostHogClient, initPostHogClient } = await load();
    expect(getPostHogClient()).toBeNull();

    const client = await initPostHogClient("phc_test", { api_host: "/ingest" });

    expect(posthog.init).toHaveBeenCalledWith("phc_test", { api_host: "/ingest" });
    expect(client).toBe(posthog);
    expect(getPostHogClient()).toBe(posthog);
  });

  test("never initialises twice, however many callers race", async () => {
    // A second init resets the SDK's state, which would drop the identify that just ran.
    const { initPostHogClient } = await load();

    await Promise.all([
      initPostHogClient("phc_test", {}),
      initPostHogClient("phc_test", {}),
      initPostHogClient("phc_test", {}),
    ]);
    await initPostHogClient("phc_test", {});

    expect(posthog.init).toHaveBeenCalledTimes(1);
  });

  test("does not re-init an SDK that already loaded itself", async () => {
    const { initPostHogClient } = await load();
    posthog.__loaded = true;

    await initPostHogClient("phc_test", {});

    expect(posthog.init).not.toHaveBeenCalled();
  });
});

describe("whenPostHogReady", () => {
  test("runs immediately when PostHog is already initialised", async () => {
    const { initPostHogClient, whenPostHogReady } = await load();
    await initPostHogClient("phc_test", {});
    const action = vi.fn();

    whenPostHogReady(action);

    expect(action).toHaveBeenCalledWith(posthog);
  });

  test("waits for initialisation, then runs once", async () => {
    vi.useFakeTimers();
    const { initPostHogClient, whenPostHogReady } = await load();
    const action = vi.fn();

    whenPostHogReady(action);
    expect(action).not.toHaveBeenCalled();

    await initPostHogClient("phc_test", {});
    await vi.advanceTimersByTimeAsync(100);

    expect(action).toHaveBeenCalledTimes(1);
  });

  test("gives up rather than polling forever when no key is configured", async () => {
    vi.useFakeTimers();
    const { whenPostHogReady } = await load();
    const action = vi.fn();

    whenPostHogReady(action);
    await vi.advanceTimersByTimeAsync(10_000);

    expect(action).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  test("the returned canceller stops the wait, so a remount cannot double-report", async () => {
    vi.useFakeTimers();
    const { initPostHogClient, whenPostHogReady } = await load();
    const action = vi.fn();

    const cancel = whenPostHogReady(action);
    cancel();
    await initPostHogClient("phc_test", {});
    await vi.advanceTimersByTimeAsync(1000);

    expect(action).not.toHaveBeenCalled();
  });

  test("capturePostHogClientEventWhenReady captures a mount event once PostHog starts", async () => {
    vi.useFakeTimers();
    const { capturePostHogClientEventWhenReady, initPostHogClient } = await load();

    capturePostHogClientEventWhenReady("screen_viewed", { surface: "list" });
    await initPostHogClient("phc_test", {});
    await vi.advanceTimersByTimeAsync(50);

    expect(posthog.capture).toHaveBeenCalledTimes(1);
    expect(posthog.capture).toHaveBeenCalledWith("screen_viewed", { surface: "list" });
  });
});
