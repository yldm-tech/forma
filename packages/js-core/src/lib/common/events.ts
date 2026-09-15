/**
 * The SDK's outbound event bus (ENG-1846 / ENG-1814): lifecycle signals the host page can react to —
 * fire GA/GTM tags, link session replays, run frequency capping off what was actually shown, and
 * (most importantly) know when the SDK is ready, which is what makes a consent-delayed `setup()`
 * integrable without polling for `window.forma`.
 *
 * One name set, two surfaces:
 *
 * 1. **`forma.on(name, handler)`** — the subscription surface for host JavaScript. Handlers are
 *    isolated (a throwing host handler is logged, never propagated into the SDK's critical paths)
 *    and can be registered before `setup()`.
 * 2. **A `window.dataLayer.push` — via the standard GTM idiom** (`window.dataLayer = window.dataLayer
 *    || []`). GTM triggers can only match dataLayer pushes, and the idiom survives load order: if GTM
 *    initialises after us it drains what is already queued. Cost, accepted knowingly: a site with no
 *    GTM gets a `dataLayer` array that accumulates small event objects with nothing draining it.
 *
 * The same underscored name is used on both surfaces — the constants hold the full literal so the
 * string a customer's GTM trigger (or `on()` call) matches is greppable here.
 *
 * The payload is nested under a `forma` key on the dataLayer (instead of spread flat) because
 * GTM's Data Layer Variables read a *merged* model: a flat `action` or `finished` would persist
 * across pushes and collide with the host's own keys — `action` in particular is one of the most
 * common keys in any ecommerce dataLayer. `on()` handlers receive the payload object directly.
 *
 * No PII: payloads carry ids and the `finished` boolean, never response content or scores.
 */

/**
 * What each event carries. The single source of truth for both surfaces, so the dataLayer push and
 * the `on()` handler types cannot drift apart.
 */
export interface TFormaEventPayloads {
  forma_setup_successful: { workspaceId: string };
  forma_action_tracked: { action: string };
  forma_survey_shown: { surveyId: string };
  /**
   * Fired on the first answer (`finished: false`, once per survey) and again when the finished
   * response has been sent (`finished: true`). `responseId` is the server-acknowledged id — real,
   * not client-minted — absent only offline/preview.
   */
  forma_response_submitted: { surveyId: string; responseId?: string; finished: boolean };
  /** Dismissed OR finished — the display is over either way; a `forma_response_submitted` with
   * `finished: true` for the same `surveyId` is what tells those apart. Correlate on the id, not on
   * arrival order: that event is ack-gated, so it can follow this one, or never arrive at all if the
   * response fails to save. Fires once per rendered survey. */
  forma_survey_closed: { surveyId: string };
}

export type TFormaEventName = keyof TFormaEventPayloads;

export const FORMA_EVENTS = {
  setupSuccessful: "forma_setup_successful",
  actionTracked: "forma_action_tracked",
  surveyShown: "forma_survey_shown",
  responseSubmitted: "forma_response_submitted",
  surveyClosed: "forma_survey_closed",
} as const satisfies Record<string, TFormaEventName>;

/**
 * Every key the event contract can carry, `null` where an event does not set it. The dataLayer push
 * always carries the FULL set because GTM's data model merges pushes recursively — a partial push
 * would let a previous event's `forma.responseId` or `forma.finished` bleed into a later
 * event's reads (survey A's `responseId` resolving under survey B's `forma_survey_shown`).
 * Pushing `null` overwrites the merged value; omitting the key would not. `on()` handlers have no
 * merge semantics, so they receive only the event's own keys.
 */
const EMPTY_DATALAYER_PAYLOAD: Record<string, null> = {
  workspaceId: null,
  surveyId: null,
  responseId: null,
  finished: null,
  action: null,
};

// Handlers are stored type-erased: a Set cannot hold differently-parameterised function types, and
// the typed `onFormaEvent` signature is what guarantees a handler only ever receives the
// payload of the event it subscribed to.
const subscribers = new Map<TFormaEventName, Set<(payload: unknown) => void>>();

/**
 * Subscribe to one event. Works before `setup()` (the registry is module state, no SDK boot
 * required) and survives `logout()`. Returns the matching unsubscribe function.
 */
export const onFormaEvent = <E extends TFormaEventName>(
  event: E,
  handler: (payload: TFormaEventPayloads[E]) => void
): (() => void) => {
  const handlers = subscribers.get(event) ?? new Set<(payload: unknown) => void>();
  handlers.add(handler as (payload: unknown) => void);
  subscribers.set(event, handlers);

  return () => {
    offFormaEvent(event, handler);
  };
};

export const offFormaEvent = <E extends TFormaEventName>(
  event: E,
  handler: (payload: TFormaEventPayloads[E]) => void
): void => {
  const handlers = subscribers.get(event);
  if (!handlers) return;

  handlers.delete(handler as (payload: unknown) => void);
  if (handlers.size === 0) {
    subscribers.delete(event);
  }
};

/** Test-only: drop every subscription so suites start from a clean registry. */
export const resetFormaEventSubscribers = (): void => {
  subscribers.clear();
};

const notifySubscribers = (event: TFormaEventName, payload: unknown): void => {
  const handlers = subscribers.get(event);
  if (!handlers?.size) return;

  // Iterate a snapshot, not the live Set: `Set.prototype.forEach` visits entries appended during
  // iteration, so a host handler that re-arms itself (`off()` then `on()` inside its own callback)
  // would be appended behind the cursor and dispatched again, unboundedly — and the catch below
  // would just keep going.
  [...handlers].forEach((handler) => {
    try {
      handler(payload);
    } catch (error) {
      console.error(`Forma: a "${event}" event handler threw`, error);
    }
  });
};

export const emitFormaEvent = <E extends TFormaEventName>(
  event: E,
  payload: TFormaEventPayloads[E]
): void => {
  // js-core is imported by SSR bundles; emitting is meaningless off the browser.
  if (typeof window === "undefined") return;

  // Both surfaces run host-owned code — subscriber handlers, and `dataLayer.push`, which GTM
  // replaces with its own function once it loads. The emitter's call sites sit at the head of
  // SDK-critical paths (display bookkeeping, the response queue's ack), so a host-page throw here
  // must never escape: it would cost us display state or mark a persisted response as failed. Each
  // surface is isolated separately so one failing cannot silence the other.
  try {
    // `Array.isArray`, not `?? []`: a host page that set `window.dataLayer = {}` (a hand-rolled
    // shim, or another vendor reusing the name) survives nullish coalescing and then throws on
    // `.push`. GTM's own snippet only ever creates an array, and its loaded state keeps the array
    // and swaps the `push` method, so `Array.isArray` stays true on every real GTM page.
    if (!Array.isArray(window.dataLayer)) window.dataLayer = [];
    // Undefined-valued keys are stripped before the merge: a key PRESENT with value `undefined`
    // (e.g. `responseId` in preview mode) would replace the `null` sentinel in the spread —
    // re-opening the recursive-merge bleed the sentinel exists to stop.
    const definedPayload = Object.fromEntries(
      Object.entries<unknown>(payload).filter(([, value]) => value !== undefined)
    );
    window.dataLayer.push({ event, forma: { ...EMPTY_DATALAYER_PAYLOAD, ...definedPayload } });
  } catch (error) {
    console.error(`Forma: failed to push "${event}" to the dataLayer`, error);
  }

  notifySubscribers(event, payload);
};
