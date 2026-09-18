import { mockPosthog } from "@/lib/posthog/__mocks__/posthog-js";
import { readFileSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

const loadClient = async () => {
  vi.resetModules();
  return import("./client");
};

beforeEach(() => {
  vi.clearAllMocks();
  mockPosthog.__loaded = false;
});

afterEach(() => {
  vi.useRealTimers();
});

describe("posthog-js stays out of the entry bundle", () => {
  // The point of this module. A static `import posthog from "posthog-js"` anywhere in application
  // code puts 93 KB brotli into the chunk group of the app layout's unconditionally-rendered
  // clients, where the `POSTHOG_KEY &&` render guard cannot reach it — 290 KB parsed and executed on
  // every authenticated page of an install that has no key. Only a dynamic import is code-split.
  test("the module imports the SDK dynamically, and only for its type otherwise", () => {
    const source = readFileSync(new URL("./client.ts", import.meta.url), "utf8");

    expect(source).toContain('import("posthog-js")');
    // A type-only import is erased at compile time and costs nothing.
    expect(source).toContain('import type { PostHog } from "posthog-js"');
    expect(source).not.toMatch(/^import posthog from "posthog-js";$/m);
  });
});

describe("capturePostHogClientEvent", () => {
  test("drops the event when PostHog was never initialised", async () => {
    // The install this runs on has no POSTHOG_KEY, so this is the normal path, not an edge case.
    const { capturePostHogClientEvent } = await loadClient();

    capturePostHogClientEvent("some_event", { a: 1 });

    expect(mockPosthog.capture).not.toHaveBeenCalled();
  });

  test("captures once PostHog has been initialised", async () => {
    const { capturePostHogClientEvent, initPostHogClient } = await loadClient();
    await initPostHogClient("phc_test", {});

    capturePostHogClientEvent("some_event", { a: 1 });

    expect(mockPosthog.capture).toHaveBeenCalledWith("some_event", { a: 1 });
  });
});

describe("initPostHogClient", () => {
  test("initialises the SDK and exposes it", async () => {
    const { getPostHogClient, initPostHogClient } = await loadClient();
    expect(getPostHogClient()).toBeNull();

    const client = await initPostHogClient("phc_test", { api_host: "/ingest" });

    expect(mockPosthog.init).toHaveBeenCalledWith("phc_test", { api_host: "/ingest" });
    expect(client).toBe(mockPosthog);
    expect(getPostHogClient()).toBe(mockPosthog);
  });

  test("never initialises twice, however many callers race", async () => {
    // A second `init` resets the SDK's state, which would drop the identify that just ran.
    const { initPostHogClient } = await loadClient();

    await Promise.all([
      initPostHogClient("phc_test", {}),
      initPostHogClient("phc_test", {}),
      initPostHogClient("phc_test", {}),
    ]);
    await initPostHogClient("phc_test", {});

    expect(mockPosthog.init).toHaveBeenCalledTimes(1);
  });

  test("does not re-init an SDK that already loaded itself", async () => {
    const { initPostHogClient } = await loadClient();
    mockPosthog.__loaded = true;

    await initPostHogClient("phc_test", {});

    expect(mockPosthog.init).not.toHaveBeenCalled();
  });
});

describe("whenPostHogReady", () => {
  test("runs immediately when PostHog is already initialised", async () => {
    const { initPostHogClient, whenPostHogReady } = await loadClient();
    await initPostHogClient("phc_test", {});
    const action = vi.fn();

    whenPostHogReady(action);

    expect(action).toHaveBeenCalledWith(mockPosthog);
  });

  test("waits for initialisation, then runs once", async () => {
    vi.useFakeTimers();
    const { initPostHogClient, whenPostHogReady } = await loadClient();
    const action = vi.fn();

    whenPostHogReady(action);
    expect(action).not.toHaveBeenCalled();

    await initPostHogClient("phc_test", {});
    await vi.advanceTimersByTimeAsync(100);

    expect(action).toHaveBeenCalledTimes(1);
  });

  test("gives up rather than polling forever when no key is configured", async () => {
    vi.useFakeTimers();
    const { whenPostHogReady } = await loadClient();
    const action = vi.fn();

    whenPostHogReady(action);
    await vi.advanceTimersByTimeAsync(10_000);

    expect(action).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  test("the returned canceller stops the wait, so a remount cannot double-report", async () => {
    vi.useFakeTimers();
    const { initPostHogClient, whenPostHogReady } = await loadClient();
    const action = vi.fn();

    const cancel = whenPostHogReady(action);
    cancel();
    await initPostHogClient("phc_test", {});
    await vi.advanceTimersByTimeAsync(1000);

    expect(action).not.toHaveBeenCalled();
  });
});
