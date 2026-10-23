import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import {
  WORKFLOW_CLIENT_EVENTS,
  WORKFLOW_SURFACES,
  resolveWorkflowListSurface,
  summarizeValidationProblems,
} from "./analytics";

// forma2's modules/workflows/lib/analytics.test.ts.

const posthog = vi.hoisted(() => ({ __loaded: false, capture: vi.fn(), init: vi.fn() }));

vi.mock("posthog-js", () => ({ default: posthog }));

/** The tracker and the PostHog client loaded afresh, so each test starts with PostHog not yet started. */
const load = async () => {
  vi.resetModules();
  const [analytics, client] = await Promise.all([
    import("./analytics"),
    import("../../../lib/telemetry/posthog"),
  ]);
  return { ...analytics, initPostHogClient: client.initPostHogClient };
};

beforeEach(() => {
  vi.clearAllMocks();
  posthog.__loaded = false;
});

afterEach(() => {
  vi.useRealTimers();
});

describe("trackWorkflowEvent", () => {
  test("drops a user-action event until PostHog is initialised", async () => {
    const { trackWorkflowEvent } = await load();

    trackWorkflowEvent(WORKFLOW_CLIENT_EVENTS.listFiltered, { sort_by: "updatedAt" });

    expect(posthog.capture).not.toHaveBeenCalled();
  });

  test("captures the event with its properties once loaded", async () => {
    const { initPostHogClient, trackWorkflowEvent } = await load();
    await initPostHogClient("phc_test", {});

    trackWorkflowEvent(WORKFLOW_CLIENT_EVENTS.listFiltered, { sort_by: "updatedAt" });

    expect(posthog.capture).toHaveBeenCalledWith("workflow_list_filtered", { sort_by: "updatedAt" });
  });
});

describe("trackWorkflowEventWhenReady", () => {
  test("holds a mount event until PostHog finishes initialising, then captures it once", async () => {
    vi.useFakeTimers();
    const { initPostHogClient, trackWorkflowEventWhenReady } = await load();

    trackWorkflowEventWhenReady(WORKFLOW_CLIENT_EVENTS.surfaceViewed, { surface: "builder" });
    await vi.advanceTimersByTimeAsync(200);
    expect(posthog.capture).not.toHaveBeenCalled();

    await initPostHogClient("phc_test", {});
    await vi.advanceTimersByTimeAsync(50);
    expect(posthog.capture).toHaveBeenCalledTimes(1);
    expect(posthog.capture).toHaveBeenCalledWith("workflow_surface_viewed", { surface: "builder" });

    await vi.advanceTimersByTimeAsync(10_000);
    expect(posthog.capture).toHaveBeenCalledTimes(1);
  });

  test("cancelling before PostHog is ready drops the event", async () => {
    vi.useFakeTimers();
    const { initPostHogClient, trackWorkflowEventWhenReady } = await load();

    const cancel = trackWorkflowEventWhenReady(WORKFLOW_CLIENT_EVENTS.surfaceViewed, { surface: "list" });
    cancel();
    await initPostHogClient("phc_test", {});
    await vi.advanceTimersByTimeAsync(500);

    expect(posthog.capture).not.toHaveBeenCalled();
  });
});

describe("summarizeValidationProblems", () => {
  test("counts every problem but reports each code once, sorted, and never the field paths", () => {
    const summary = summarizeValidationProblems([
      { code: "step_incomplete", field: "nodes.send-email.config.to" },
      { code: "trigger_survey_unbound", field: "trigger.config.surveyId" },
      { code: "step_incomplete", field: "nodes.send-email.config.subject" },
    ]);

    expect(summary).toEqual({
      problem_count: 3,
      problem_codes: ["step_incomplete", "trigger_survey_unbound"],
    });
    expect(JSON.stringify(summary)).not.toContain("send-email");
  });
});

describe("resolveWorkflowListSurface", () => {
  const settled = {
    showInitialLoading: false,
    isError: false,
    hasActiveFilters: false,
    workflowCount: 3,
  };

  test("loading is not a visit, and neither is an error that replaced the list", () => {
    expect(resolveWorkflowListSurface({ ...settled, showInitialLoading: true, workflowCount: 0 })).toBeNull();
    expect(resolveWorkflowListSurface({ ...settled, isError: true, workflowCount: 0 })).toBeNull();
    // An error with rows still on screen is the list, the way the page still renders them.
    expect(resolveWorkflowListSurface({ ...settled, isError: true })).toBe("list");
  });

  test("an empty list is filtered or not, depending on the filters", () => {
    expect(resolveWorkflowListSurface({ ...settled, workflowCount: 0 })).toBe("list_empty");
    expect(resolveWorkflowListSurface({ ...settled, workflowCount: 0, hasActiveFilters: true })).toBe(
      "list_empty_filtered"
    );
  });

  test("anything with rows is the list", () => {
    expect(resolveWorkflowListSurface(settled)).toBe("list");
  });
});

describe("event contract", () => {
  test("every event name is snake_case with the workflow_ prefix, and none repeats", () => {
    const names = Object.values(WORKFLOW_CLIENT_EVENTS);

    for (const name of names) expect(name).toMatch(/^workflow_[a-z_]+$/);
    expect(new Set(names).size).toBe(names.length);
  });

  test("surfaces are a closed, snake_case set", () => {
    for (const surface of WORKFLOW_SURFACES) expect(surface).toMatch(/^[a-z_]+$/);
    expect(new Set(WORKFLOW_SURFACES).size).toBe(WORKFLOW_SURFACES.length);
  });
});
