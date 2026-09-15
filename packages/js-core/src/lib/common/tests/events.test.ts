import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  FORMA_EVENTS,
  emitFormaEvent,
  offFormaEvent,
  onFormaEvent,
  resetFormaEventSubscribers,
} from "@/lib/common/events";

describe("emitFormaEvent", () => {
  beforeEach(() => {
    // The emitter creates `window.dataLayer` when absent; start every test from that state, and
    // from a subscriber-free registry.
    delete (window as { dataLayer?: unknown }).dataLayer;
    resetFormaEventSubscribers();
  });

  test("every event name carries the forma_ namespace — the string GTM triggers and on() match", () => {
    for (const name of Object.values(FORMA_EVENTS)) {
      expect(name).toMatch(/^forma_/);
    }
  });

  test("creates window.dataLayer when absent and pushes the nested envelope", () => {
    expect(window.dataLayer).toBeUndefined();

    emitFormaEvent(FORMA_EVENTS.responseSubmitted, {
      surveyId: "survey_1",
      responseId: "response_1",
      finished: true,
    });

    // Nested under `forma`, never spread flat: GTM merges pushes, so a flat `finished` or
    // `action` would collide with the host's own dataLayer keys. And the FULL key set every time,
    // nulls included: GTM merges recursively, so an omitted key would leave a previous event's
    // value readable under this event's trigger.
    expect(window.dataLayer).toEqual([
      {
        event: "forma_response_submitted",
        forma: {
          workspaceId: null,
          action: null,
          surveyId: "survey_1",
          responseId: "response_1",
          finished: true,
        },
      },
    ]);
  });

  test("appends to a pre-existing dataLayer, never replaces it — the host's queued events survive", () => {
    const hostEntry = { event: "host_event", cart: "abc" };
    window.dataLayer = [hostEntry];

    emitFormaEvent(FORMA_EVENTS.actionTracked, { action: "clicked_demo" });

    expect(window.dataLayer[0]).toBe(hostEntry);
    expect(window.dataLayer).toHaveLength(2);
    expect(window.dataLayer[1]).toEqual({
      event: "forma_action_tracked",
      forma: {
        workspaceId: null,
        surveyId: null,
        responseId: null,
        finished: null,
        action: "clicked_demo",
      },
    });
  });

  test("a payload key present with value undefined falls back to the null sentinel, not undefined", () => {
    // `responseId` is typed optional by the widened callbacks, so an emit can carry it as an
    // explicit undefined. If that survived the merge it would replace the null sentinel — and GTM's
    // recursive merge would keep an EARLIER event's responseId readable under this event's trigger.
    emitFormaEvent(FORMA_EVENTS.responseSubmitted, {
      surveyId: "survey_1",
      responseId: undefined,
      finished: true,
    });

    expect(window.dataLayer?.[0]).toEqual({
      event: "forma_response_submitted",
      forma: {
        workspaceId: null,
        action: null,
        surveyId: "survey_1",
        responseId: null,
        finished: true,
      },
    });
  });

  test("a non-array dataLayer (a host shim) is replaced instead of throwing on .push", () => {
    (window as { dataLayer?: unknown }).dataLayer = {};

    expect(() => {
      emitFormaEvent(FORMA_EVENTS.surveyShown, { surveyId: "survey_1" });
    }).not.toThrow();

    expect(Array.isArray(window.dataLayer)).toBe(true);
    expect(window.dataLayer).toHaveLength(1);
  });

  test("a throwing dataLayer.push (GTM replaces it with host-owned code) never escapes, and subscribers still fire", () => {
    const poisoned: Record<string, unknown>[] = [];
    poisoned.push = () => {
      throw new Error("host push exploded");
    };
    window.dataLayer = poisoned;

    const handler = vi.fn();
    onFormaEvent(FORMA_EVENTS.surveyShown, handler);

    expect(() => {
      emitFormaEvent(FORMA_EVENTS.surveyShown, { surveyId: "survey_1" });
    }).not.toThrow();

    // The subscription surface still fired: the two are isolated separately.
    expect(handler).toHaveBeenCalledWith({ surveyId: "survey_1" });
  });
});

describe("on() / off() subscriptions", () => {
  beforeEach(() => {
    delete (window as { dataLayer?: unknown }).dataLayer;
    resetFormaEventSubscribers();
    vi.restoreAllMocks();
  });

  test("notifies every handler of the emitted event with its payload, and no other event's handlers", () => {
    const first = vi.fn();
    const second = vi.fn();
    const other = vi.fn();

    onFormaEvent(FORMA_EVENTS.surveyShown, first);
    onFormaEvent(FORMA_EVENTS.surveyShown, second);
    onFormaEvent(FORMA_EVENTS.surveyClosed, other);

    emitFormaEvent(FORMA_EVENTS.surveyShown, { surveyId: "survey_1" });

    expect(first).toHaveBeenCalledWith({ surveyId: "survey_1" });
    expect(second).toHaveBeenCalledWith({ surveyId: "survey_1" });
    expect(other).not.toHaveBeenCalled();
  });

  test("registering the same handler twice notifies it once", () => {
    const handler = vi.fn();

    onFormaEvent(FORMA_EVENTS.responseSubmitted, handler);
    onFormaEvent(FORMA_EVENTS.responseSubmitted, handler);

    emitFormaEvent(FORMA_EVENTS.responseSubmitted, { surveyId: "survey_1", finished: false });

    expect(handler).toHaveBeenCalledTimes(1);
  });

  test("the function returned by on() removes the subscription; off() removes only the handler it names", () => {
    const viaReturn = vi.fn();
    const viaOff = vi.fn();
    const kept = vi.fn();

    const unsubscribe = onFormaEvent(FORMA_EVENTS.surveyShown, viaReturn);
    onFormaEvent(FORMA_EVENTS.surveyShown, viaOff);
    onFormaEvent(FORMA_EVENTS.surveyShown, kept);

    unsubscribe();
    offFormaEvent(FORMA_EVENTS.surveyShown, viaOff);
    emitFormaEvent(FORMA_EVENTS.surveyShown, { surveyId: "survey_1" });

    expect(viaReturn).not.toHaveBeenCalled();
    expect(viaOff).not.toHaveBeenCalled();
    expect(kept).toHaveBeenCalledTimes(1);
  });

  test("off() on an unknown handler or event is a no-op", () => {
    const handler = vi.fn();

    expect(() => {
      offFormaEvent(FORMA_EVENTS.surveyShown, handler);
    }).not.toThrow();

    onFormaEvent(FORMA_EVENTS.surveyShown, handler);
    offFormaEvent(FORMA_EVENTS.surveyShown, vi.fn());
    emitFormaEvent(FORMA_EVENTS.surveyShown, { surveyId: "survey_1" });

    expect(handler).toHaveBeenCalledTimes(1);
  });

  test("a handler that unsubscribes itself still receives the event it is handling", () => {
    const handler = vi.fn(() => {
      offFormaEvent(FORMA_EVENTS.surveyShown, handler);
    });

    onFormaEvent(FORMA_EVENTS.surveyShown, handler);

    expect(() => {
      emitFormaEvent(FORMA_EVENTS.surveyShown, { surveyId: "survey_1" });
    }).not.toThrow();
    expect(handler).toHaveBeenCalledTimes(1);

    emitFormaEvent(FORMA_EVENTS.surveyShown, { surveyId: "survey_2" });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  test("a handler that re-arms itself mid-dispatch is dispatched once, not unboundedly", () => {
    // `Set.prototype.forEach` visits entries appended during iteration, so without the snapshot in
    // notifySubscribers the re-arm lands behind the cursor and loops until the page hangs. `other`
    // is load-bearing: it keeps the Set non-empty, so off() does not drop the map entry and on()
    // re-adds into the same Set being iterated.
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const other = vi.fn();
    let calls = 0;
    const handler = (): void => {
      calls += 1;
      if (calls > 2) throw new Error("re-arm re-entered the dispatch in flight");
      offFormaEvent(FORMA_EVENTS.surveyShown, handler);
      onFormaEvent(FORMA_EVENTS.surveyShown, handler);
    };

    onFormaEvent(FORMA_EVENTS.surveyShown, other);
    onFormaEvent(FORMA_EVENTS.surveyShown, handler);

    emitFormaEvent(FORMA_EVENTS.surveyShown, { surveyId: "survey_1" });
    expect(calls).toBe(1);
    expect(errorSpy).not.toHaveBeenCalled();

    // Still subscribed for the next emit — re-arming is the point.
    emitFormaEvent(FORMA_EVENTS.surveyShown, { surveyId: "survey_2" });
    expect(calls).toBe(2);
  });

  test("a throwing handler is logged and stops neither the other handlers nor the dataLayer push", () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const throwing = vi.fn(() => {
      throw new Error("host blew up");
    });
    const healthy = vi.fn();

    onFormaEvent(FORMA_EVENTS.surveyClosed, throwing);
    onFormaEvent(FORMA_EVENTS.surveyClosed, healthy);

    expect(() => {
      emitFormaEvent(FORMA_EVENTS.surveyClosed, { surveyId: "survey_1" });
    }).not.toThrow();

    expect(healthy).toHaveBeenCalledTimes(1);
    expect(window.dataLayer).toHaveLength(1);
    expect(errorSpy).toHaveBeenCalled();
  });

  test("emitting with no subscribers still pushes to the dataLayer", () => {
    expect(() => {
      emitFormaEvent(FORMA_EVENTS.setupSuccessful, { workspaceId: "ws_1" });
    }).not.toThrow();
    expect(window.dataLayer).toHaveLength(1);
  });
});
