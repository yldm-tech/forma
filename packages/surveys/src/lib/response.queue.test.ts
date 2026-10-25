// @vitest-environment happy-dom
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { err, ok } from "@forma/types/error-handlers";
import { RESPONSE_ALREADY_FINISHED_ERROR_CODE } from "@forma/types/errors";
import { TResponseUpdate } from "@forma/types/responses";
import { TResponseErrorCodesEnum } from "@/types/response-error-codes";
import type { PendingResponseEntry } from "./offline-storage";
import { RECOVERY_BASE_DELAY_MS, ResponseQueue, delay } from "./response-queue";
import { SurveyState } from "./survey-state";

// Suppress noisy console output from retry logic during tests
beforeAll(() => {
  vi.spyOn(console, "error").mockImplementation(() => {});
  vi.spyOn(console, "log").mockImplementation(() => {});
});
afterAll(() => {
  vi.restoreAllMocks();
});

vi.mock("./offline-storage", () => ({
  addPendingResponse: vi.fn().mockResolvedValue(1),
  countPendingResponsesStrict: vi.fn().mockResolvedValue(0),
  getPendingResponsesStrict: vi.fn().mockResolvedValue([]),
  removePendingResponseStrict: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("./api-client", () => ({
  ApiClient: vi.fn(function ApiClient() {
    return {
      updateResponse: vi.fn(),
      createResponse: vi.fn(),
    };
  }),
}));

const getSurveyState: () => SurveyState = () => ({
  responseId: null,
  displayId: "display1",
  userId: "user1",
  contactId: "contact1",
  surveyId: "survey1",
  singleUseId: "single1",
  pinAuthToken: null,
  shouldCreateResponseFromState: false,
  responseAcc: { finished: false, data: {}, ttc: {}, variables: {} },
  updateResponseId: vi.fn(),
  updateDisplayId: vi.fn(),
  updateUserId: vi.fn(),
  updateContactId: vi.fn(),
  enableBootstrapResponseCreate: vi.fn(),
  disableBootstrapResponseCreate: vi.fn(),
  accumulateResponse: vi.fn(),
  isResponseFinished: vi.fn(),
  clear: vi.fn(),
  copy: vi.fn(),
  setSurveyId: vi.fn(),
});

const getConfig = (overrides = {}) => ({
  appUrl: "http://localhost",
  workspaceId: "ws1",
  retryAttempts: 2,
  setSurveyState: vi.fn(),
  onResponseSendingFailed: vi.fn(),
  onResponseSendingFinished: vi.fn(),
  ...overrides,
});

const responseUpdate: TResponseUpdate = {
  data: { foo: "bar" },
  hiddenFields: {},
  finished: true,
};

const flushPromises = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

describe("delay", () => {
  test("resolves after specified ms", async () => {
    const start = Date.now();
    await delay(50);
    expect(Date.now() - start).toBeGreaterThanOrEqual(49); // Using 49 to account for execution time
  });
});

describe("ResponseQueue", () => {
  let surveyState: SurveyState;
  let config: ReturnType<typeof getConfig>;
  let queue: ResponseQueue;
  let apiMock: any;

  beforeEach(() => {
    surveyState = getSurveyState();
    config = getConfig();
    queue = new ResponseQueue(config, surveyState);
    apiMock = queue.api;
    vi.clearAllMocks();
  });

  test("constructor initializes properties", () => {
    expect(queue.config).toBe(config);
    expect(queue.api).toBeDefined();
    expect(queue["surveyState"]).toBe(surveyState);
    expect(queue.queue).toEqual([]);
  });

  test("setResponseRecaptchaToken sets token", () => {
    queue.setResponseRecaptchaToken("token123");
    expect(queue["responseRecaptchaToken"]).toBe("token123");
  });

  test("add accumulates response, sets survey state, and processes queue", async () => {
    vi.spyOn(queue, "processQueue").mockImplementation(() => Promise.resolve({ success: true }));
    queue.add(responseUpdate);
    expect(surveyState.accumulateResponse).toHaveBeenCalledWith(responseUpdate);
    expect(config.setSurveyState).toHaveBeenCalledWith(surveyState);
    expect(queue.queue[0]).toBe(responseUpdate);
    expect(queue.processQueue).toHaveBeenCalled();
  });

  test("processQueue does nothing if request in progress or queue empty", async () => {
    queue["isRequestInProgress"] = true;
    await queue.processQueue();
    queue["isRequestInProgress"] = false;
    queue.queue.length = 0;
    await queue.processQueue();
    expect(true).toBe(true); // just to ensure no errors
  });

  test("processQueue sends response and removes from queue on success", async () => {
    queue.queue.push(responseUpdate);
    vi.spyOn(queue, "sendResponse").mockResolvedValue(ok(true));
    await queue.processQueue();
    expect(queue.queue).toHaveLength(0);
    expect(queue["isRequestInProgress"]).toBe(false);
  });

  // These specs keep the real sendResponse()/sendResponseWithRetry() classification+retry path and
  // only mock the lower-level api.updateResponse dependency, so terminal-vs-retryable behavior and
  // the error passthrough are actually exercised. responseId is set so the real update path runs
  // (the ENG-1319 scenario is a PUT update rejected server-side).
  test("processQueue retries and calls onResponseSendingFailed on recaptcha error", async () => {
    surveyState.responseId = "resp1";
    queue.queue.push(responseUpdate);

    apiMock.updateResponse.mockResolvedValue({
      ok: false,
      error: {
        code: "internal_server_error",
        message: "An error occurred while sending the response.",
        status: 500,
        details: { code: "recaptcha_verification_failed" },
      },
    });

    await queue.processQueue();
    expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
      responseUpdate,
      TResponseErrorCodesEnum.RecaptchaError
    );
    expect(queue["isRequestInProgress"]).toBe(false);
  });

  test("processQueue retries and calls onResponseSendingFailed after max attempts", async () => {
    surveyState.responseId = "resp1";
    queue.queue.push(responseUpdate);
    apiMock.updateResponse.mockResolvedValue({
      ok: false,
      error: {
        code: "internal_server_error",
        message: "An error occurred while sending the response.",
        status: 500,
      },
    });
    await queue.processQueue();
    // A first attempt, then one retry per backoff step: the last backoff is followed by a last attempt.
    expect(apiMock.updateResponse).toHaveBeenCalledTimes(config.retryAttempts + 1);
    expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
      responseUpdate,
      TResponseErrorCodesEnum.ResponseSendingError
    );
    expect(queue["isRequestInProgress"]).toBe(false);
  }, 10000);

  test("processQueue does not retry 400 'already finished', drops item, surfaces ResponseAlreadyCompleted", async () => {
    surveyState.responseId = "resp1";
    queue.queue.push(responseUpdate);
    apiMock.updateResponse.mockResolvedValue({
      ok: false,
      error: {
        code: "bad_request",
        message: "Response is already finished",
        status: 400,
      },
    });

    const result = await queue.processQueue();

    expect(result.success).toBe(false);
    expect(apiMock.updateResponse).toHaveBeenCalledTimes(1); // no exponential-backoff retries
    expect(queue.queue).toHaveLength(0); // dead item dropped so manual Retry can't loop
    expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
      responseUpdate,
      TResponseErrorCodesEnum.ResponseAlreadyCompleted
    );
    expect(queue["isRequestInProgress"]).toBe(false);
  });

  test("processQueue detects already-completed via the stable details.code marker, ignoring message wording", async () => {
    surveyState.responseId = "resp1";
    queue.queue.push(responseUpdate);
    apiMock.updateResponse.mockResolvedValue({
      ok: false,
      error: {
        code: "bad_request",
        // Deliberately NOT the English "already finished" wording — detection must rely on the marker.
        message: "Réponse déjà terminée",
        status: 400,
        details: { code: RESPONSE_ALREADY_FINISHED_ERROR_CODE },
      },
    });

    await queue.processQueue();

    expect(apiMock.updateResponse).toHaveBeenCalledTimes(1);
    expect(queue.queue).toHaveLength(0);
    expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
      responseUpdate,
      TResponseErrorCodesEnum.ResponseAlreadyCompleted
    );
  });

  test("processQueue treats 409 as already-completed and drops the item", async () => {
    surveyState.responseId = "resp1";
    queue.queue.push(responseUpdate);
    apiMock.updateResponse.mockResolvedValue({
      ok: false,
      error: { code: "bad_request", message: "conflict", status: 409 },
    });

    await queue.processQueue();

    expect(apiMock.updateResponse).toHaveBeenCalledTimes(1);
    expect(queue.queue).toHaveLength(0);
    expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
      responseUpdate,
      TResponseErrorCodesEnum.ResponseAlreadyCompleted
    );
  });

  test("processQueue drops other 4xx client errors without retry and surfaces a non-retryable code", async () => {
    surveyState.responseId = "resp1";
    queue.queue.push(responseUpdate);
    apiMock.updateResponse.mockResolvedValue({
      ok: false,
      error: { code: "bad_request", message: "Validation failed", status: 400 },
    });

    await queue.processQueue();

    expect(apiMock.updateResponse).toHaveBeenCalledTimes(1);
    expect(queue.queue).toHaveLength(0);
    // Non-retryable code (dropped item) — NOT ResponseSendingError, which would render a dead Retry CTA.
    expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
      responseUpdate,
      TResponseErrorCodesEnum.ResponseSendingErrorPermanent
    );
  });

  test("processQueue keeps retrying 404 (not treated as terminal) and leaves item in queue", async () => {
    surveyState.responseId = "resp1";
    queue.queue.push(responseUpdate);
    apiMock.updateResponse.mockResolvedValue({
      ok: false,
      error: { code: "not_found", message: "Response not found", status: 404 },
    });

    await queue.processQueue();

    expect(apiMock.updateResponse).toHaveBeenCalledTimes(config.retryAttempts + 1); // retried, not dropped on first 4xx
    expect(queue.queue).toHaveLength(1);
    expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
      responseUpdate,
      TResponseErrorCodesEnum.ResponseSendingError
    );
  }, 10000);

  test("processQueue calls onResponseSendingFinished if finished", async () => {
    queue.queue.push({ ...responseUpdate, finished: true });
    vi.spyOn(queue, "sendResponse").mockResolvedValue(ok(true));
    await queue.processQueue();
    expect(config.onResponseSendingFinished).toHaveBeenCalled();
  });

  test("sendResponse calls updateResponse if responseId exists", async () => {
    surveyState.responseId = "resp1";
    apiMock.updateResponse.mockResolvedValue({ ok: true, data: { quotaFull: false } });
    const result = await queue.sendResponse(responseUpdate);
    expect(apiMock.updateResponse).toHaveBeenCalled();
    expect(result.ok).toBe(true);
  });

  test("sendResponse calls createResponse if no responseId, updates responseId", async () => {
    apiMock.createResponse.mockResolvedValue({ ok: true, data: { id: "newid" } });
    const result = await queue.sendResponse(responseUpdate);
    expect(apiMock.createResponse).toHaveBeenCalled();
    expect(surveyState.updateResponseId).toHaveBeenCalledWith("newid");
    expect(surveyState.disableBootstrapResponseCreate).toHaveBeenCalled();
    expect(config.setSurveyState).toHaveBeenCalledWith(surveyState);
    expect(result.ok).toBe(true);
  });

  test("sendResponse uses accumulated response state when bootstrap create is enabled", async () => {
    surveyState.shouldCreateResponseFromState = true;
    surveyState.responseAcc = {
      finished: true,
      data: { q1: "saved", q2: "latest" },
      ttc: { q1: 100, q2: 200 },
      variables: { locale: "en" },
      language: "en",
      meta: { url: "https://example.com" },
      endingId: "ending-1",
    };
    apiMock.createResponse.mockResolvedValue({ ok: true, data: { id: "newid" } });

    await queue.sendResponse({
      data: { q2: "latest" },
      hiddenFields: { hidden: "value" },
      finished: false,
      language: "de",
      endingId: null,
    });

    expect(apiMock.createResponse).toHaveBeenCalledWith(
      expect.objectContaining({
        finished: true,
        data: { q1: "saved", q2: "latest", hidden: "value" },
        ttc: { q1: 100, q2: 200 },
        variables: { locale: "en" },
        language: "en",
        meta: { url: "https://example.com" },
        endingId: "ending-1",
        displayId: "display1",
      })
    );
  });

  test("sendResponse notifies when a responseId is created", async () => {
    const onResponseCreated = vi.fn();
    queue = new ResponseQueue(getConfig({ onResponseCreated }), surveyState);
    apiMock = queue.api;
    apiMock.createResponse.mockResolvedValue({ ok: true, data: { id: "newid" } });

    await queue.sendResponse(responseUpdate);

    expect(onResponseCreated).toHaveBeenCalledWith("newid");
  });

  test("sendResponse returns err if createResponse fails", async () => {
    apiMock.createResponse.mockResolvedValue({ ok: false, error: { code: "internal_server_error" } });
    const result = await queue.sendResponse(responseUpdate);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("internal_server_error");
    }
  });

  test("sendResponse handles unexpected errors", async () => {
    apiMock.createResponse.mockRejectedValue(new Error("Unexpected error"));
    const result = await queue.sendResponse(responseUpdate);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("internal_server_error");
    }
  });

  test("updateSurveyState updates surveyState", () => {
    const newState = getSurveyState();
    queue.updateSurveyState(newState);
    expect(queue["surveyState"]).toBe(newState);
  });

  test("processQueueAsync returns success false if queue empty", async () => {
    const result = await queue.processQueue();
    expect(result.success).toBe(false);
  });

  test("processQueueAsync returns success false if request in progress", async () => {
    queue["isRequestInProgress"] = true;
    const result = await queue.processQueue();
    expect(result.success).toBe(false);
  });

  test("processQueueAsync returns success true on successful send", async () => {
    queue.queue.push(responseUpdate);
    vi.spyOn(queue, "sendResponse").mockResolvedValue(ok(true));
    const result = await queue.processQueue();
    expect(result.success).toBe(true);
    expect(queue.queue).toHaveLength(0);
  });

  test("processQueueAsync returns success false after max attempts", async () => {
    queue.queue.push(responseUpdate);
    vi.spyOn(queue, "sendResponse").mockResolvedValue(
      err({
        code: "internal_server_error",
        message: "An error occurred while sending the response.",
        status: 500,
      })
    );
    const result = await queue.processQueue();
    expect(result.success).toBe(false);
    expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
      responseUpdate,
      TResponseErrorCodesEnum.ResponseSendingError
    );
  });

  test("processQueueAsync returns success false on recaptcha error", async () => {
    queue.queue.push(responseUpdate);
    vi.spyOn(queue, "sendResponse").mockResolvedValue(
      err({
        code: "internal_server_error",
        message: "An error occurred while sending the response.",
        status: 500,
        details: {
          code: "recaptcha_verification_failed",
        },
      })
    );
    const result = await queue.processQueue();
    expect(result.success).toBe(false);
    expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
      responseUpdate,
      TResponseErrorCodesEnum.RecaptchaError
    );
  });

  test("getUnsentData returns empty object when queue is empty", () => {
    const unsentData = queue.getUnsentData();
    expect(unsentData).toEqual({});
  });

  test("getUnsentData returns data from single item in queue", () => {
    queue.queue.push({ data: { q1: "answer1" }, hiddenFields: {}, finished: false });
    const unsentData = queue.getUnsentData();
    expect(unsentData).toEqual({ q1: "answer1" });
  });

  test("getUnsentData aggregates data from multiple items in queue", () => {
    queue.queue.push({ data: { q1: "answer1" }, hiddenFields: {}, finished: false });
    queue.queue.push({ data: { q2: "answer2" }, hiddenFields: {}, finished: false });
    queue.queue.push({ data: { q3: "answer3" }, hiddenFields: {}, finished: true });
    const unsentData = queue.getUnsentData();
    expect(unsentData).toEqual({ q1: "answer1", q2: "answer2", q3: "answer3" });
  });

  test("getUnsentData overwrites duplicate keys with latest value", () => {
    queue.queue.push({ data: { q1: "answer1" }, hiddenFields: {}, finished: false });
    queue.queue.push({ data: { q1: "updated_answer1", q2: "answer2" }, hiddenFields: {}, finished: false });
    const unsentData = queue.getUnsentData();
    expect(unsentData).toEqual({ q1: "updated_answer1", q2: "answer2" });
  });

  // --- Offline persistence tests ---

  test("add processes the in-memory queue when IndexedDB persistence rejects", async () => {
    const { addPendingResponse } = await import("./offline-storage");
    const error = new Error("quota exceeded");
    vi.mocked(addPendingResponse).mockRejectedValueOnce(error);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    const processSpy = vi.spyOn(offlineQueue, "processQueue").mockResolvedValue({ success: true });

    offlineQueue.add(responseUpdate);
    await flushPromises();

    expect(processSpy).toHaveBeenCalled();
    expect(console.error).toHaveBeenCalledWith(
      "Forma: Failed to persist pending response to IndexedDB",
      expect.objectContaining({ error, surveyId: "s1" })
    );
  });

  test("add processes the in-memory queue when IndexedDB persistence is unavailable", async () => {
    const { addPendingResponse } = await import("./offline-storage");
    vi.mocked(addPendingResponse).mockResolvedValueOnce(-1);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    const processSpy = vi.spyOn(offlineQueue, "processQueue").mockResolvedValue({ success: true });

    offlineQueue.add(responseUpdate);
    await flushPromises();

    expect(processSpy).toHaveBeenCalled();
    expect(offlineQueue["pendingDbIds"].has(responseUpdate)).toBe(false);
  });

  test("processQueue holds the answers back while offline in offline mode", async () => {
    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, holdWhileOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push(responseUpdate);
    const sendSpy = vi.spyOn(offlineQueue, "sendResponse");
    Object.defineProperty(navigator, "onLine", { value: false, configurable: true });
    try {
      const result = await offlineQueue.processQueue();
      expect(result.success).toBe(false);
      expect(sendSpy).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(navigator, "onLine", { value: true, configurable: true });
    }
  });

  // Progress persistence alone (a link survey that saves progress, F1) stores the answers but sends them as a survey without it does, so a final answer that cannot be sent is reported rather than waiting silently.
  test("processQueue still sends while offline when answers are only persisted", async () => {
    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push(responseUpdate);
    const sendSpy = vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(ok(true));
    Object.defineProperty(navigator, "onLine", { value: false, configurable: true });
    try {
      const result = await offlineQueue.processQueue();
      expect(result.success).toBe(true);
      expect(sendSpy).toHaveBeenCalledWith(responseUpdate);
    } finally {
      Object.defineProperty(navigator, "onLine", { value: true, configurable: true });
    }
  });

  test("processQueue returns false when isSyncing is true", async () => {
    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push(responseUpdate);
    offlineQueue["isSyncing"] = true;
    const result = await offlineQueue.processQueue();
    expect(result.success).toBe(false);
  });

  // An IndexedDB that cannot be read (storage blocked in a frame, a browser without it) must not stop the answers: with progress saving on by default, that would be every answer on such a browser.
  test("processQueue sends from memory when counting IndexedDB entries rejects", async () => {
    const { countPendingResponsesStrict } = await import("./offline-storage");
    const error = new Error("count failed");
    vi.mocked(countPendingResponsesStrict).mockRejectedValueOnce(error);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push(responseUpdate);
    const sendSpy = vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(ok(true));

    const result = await offlineQueue.processQueue();

    expect(result.success).toBe(true);
    expect(sendSpy).toHaveBeenCalledWith(responseUpdate);
    expect(offlineQueue.queue).toHaveLength(0);
    expect(console.error).toHaveBeenCalledWith(
      "Forma: Failed to count pending responses in IndexedDB",
      expect.objectContaining({ error, surveyId: "s1" })
    );
  });

  test("processQueue defers to sync when multiple IDB entries exist", async () => {
    const { countPendingResponsesStrict } = await import("./offline-storage");
    vi.mocked(countPendingResponsesStrict).mockResolvedValue(3);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push({ data: { q1: "answer" }, finished: false });

    const syncSpy = vi.spyOn(offlineQueue, "syncPersistedResponses").mockResolvedValue({
      success: true,
      syncedCount: 3,
    });

    const result = await offlineQueue.processQueue();
    expect(result.success).toBe(false);
    expect(syncSpy).toHaveBeenCalled();
    expect(offlineQueue["isRequestInProgress"]).toBe(false);
  });

  test("processQueue logs rejected background sync attempts", async () => {
    const { countPendingResponsesStrict } = await import("./offline-storage");
    const error = new Error("sync failed");
    vi.mocked(countPendingResponsesStrict).mockResolvedValueOnce(3);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push({ data: { q1: "answer" }, finished: false });
    vi.spyOn(offlineQueue, "syncPersistedResponses").mockRejectedValueOnce(error);

    const result = await offlineQueue.processQueue();
    await flushPromises();

    expect(result.success).toBe(false);
    expect(console.error).toHaveBeenCalledWith(
      "Forma: Failed to sync persisted responses in background",
      expect.objectContaining({ error, surveyId: "s1" })
    );
  });

  test("processQueue bails out if syncPersistedResponses starts during countPendingResponsesStrict await", async () => {
    const { countPendingResponsesStrict } = await import("./offline-storage");
    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    // Simulate syncPersistedResponses starting during the async gap
    vi.mocked(countPendingResponsesStrict).mockImplementation(async () => {
      // While countPendingResponsesStrict is resolving, isSyncing becomes true
      offlineQueue["isSyncing"] = true;
      return 1;
    });

    offlineQueue.queue.push({ data: { q1: "answer" }, finished: false });

    const sendSpy = vi.spyOn(offlineQueue as any, "sendResponseWithRetry");

    const result = await offlineQueue.processQueue();
    expect(result.success).toBe(false);
    expect(sendSpy).not.toHaveBeenCalled();
  });

  test("processQueue sends directly when it is the only IDB entry", async () => {
    const { countPendingResponsesStrict } = await import("./offline-storage");
    vi.mocked(countPendingResponsesStrict).mockResolvedValue(1);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push({ data: { q1: "answer" }, finished: false });

    vi.spyOn(offlineQueue as any, "sendResponseWithRetry").mockImplementation(async () => {
      offlineQueue.queue.shift();
      return { success: true };
    });

    const result = await offlineQueue.processQueue();
    expect(result.success).toBe(true);
  });

  test("processQueue keeps pendingDbIds until IndexedDB removal resolves", async () => {
    const { countPendingResponsesStrict, removePendingResponseStrict } = await import("./offline-storage");
    vi.mocked(countPendingResponsesStrict).mockResolvedValueOnce(1);

    let resolveRemove!: () => void;
    vi.mocked(removePendingResponseStrict).mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveRemove = resolve;
        })
    );

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push(responseUpdate);
    offlineQueue["pendingDbIds"].set(responseUpdate, 10);
    vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(ok(true));

    const resultPromise = offlineQueue.processQueue();
    await flushPromises();

    expect(removePendingResponseStrict).toHaveBeenCalledWith(10);
    expect(offlineQueue["pendingDbIds"].has(responseUpdate)).toBe(true);

    resolveRemove();
    const result = await resultPromise;

    expect(result.success).toBe(true);
    expect(offlineQueue["pendingDbIds"].has(responseUpdate)).toBe(false);
  });

  test("processQueue keeps pendingDbIds and clears request lock when IndexedDB removal rejects", async () => {
    const { countPendingResponsesStrict, removePendingResponseStrict } = await import("./offline-storage");
    const error = new Error("delete failed");
    vi.mocked(countPendingResponsesStrict).mockResolvedValueOnce(1);
    vi.mocked(removePendingResponseStrict).mockRejectedValueOnce(error);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push(responseUpdate);
    offlineQueue["pendingDbIds"].set(responseUpdate, 10);
    vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(ok(true));

    const result = await offlineQueue.processQueue();

    expect(result.success).toBe(true);
    expect(offlineQueue["pendingDbIds"].has(responseUpdate)).toBe(true);
    expect(offlineQueue["isRequestInProgress"]).toBe(false);
    expect(console.error).toHaveBeenCalledWith(
      "Forma: Failed to remove sent response from IndexedDB",
      expect.objectContaining({ error, surveyId: "s1" })
    );
  });

  test("processQueue ignores already-sent rows while IndexedDB cleanup is pending", async () => {
    const { countPendingResponsesStrict, removePendingResponseStrict } = await import("./offline-storage");
    const firstUpdate = { ...responseUpdate, data: { step: "first" } };
    const secondUpdate = { ...responseUpdate, data: { step: "second" } };
    const error = new Error("delete failed");

    vi.mocked(countPendingResponsesStrict).mockResolvedValueOnce(1).mockResolvedValueOnce(2);
    vi.mocked(removePendingResponseStrict)
      .mockRejectedValueOnce(error)
      .mockRejectedValueOnce(error)
      .mockResolvedValueOnce(undefined);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    const sendSpy = vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(ok(true));
    const syncSpy = vi.spyOn(offlineQueue, "syncPersistedResponses");

    offlineQueue.queue.push(firstUpdate);
    offlineQueue["pendingDbIds"].set(firstUpdate, 10);
    await offlineQueue.processQueue();

    offlineQueue.queue.push(secondUpdate);
    offlineQueue["pendingDbIds"].set(secondUpdate, 11);
    const result = await offlineQueue.processQueue();

    expect(result.success).toBe(true);
    expect(syncSpy).not.toHaveBeenCalled();
    expect(sendSpy).toHaveBeenCalledTimes(2);
    expect(sendSpy).toHaveBeenNthCalledWith(1, firstUpdate);
    expect(sendSpy).toHaveBeenNthCalledWith(2, secondUpdate);
    expect(removePendingResponseStrict).toHaveBeenNthCalledWith(2, 10);
    expect(removePendingResponseStrict).toHaveBeenNthCalledWith(3, 11);
  });

  test("loadPersistedQueue returns 0 when persistOffline is disabled", async () => {
    const count = await queue.loadPersistedQueue();
    expect(count).toBe(0);
  });

  test("loadPersistedQueue delegates to countPendingResponsesStrict", async () => {
    const { countPendingResponsesStrict } = await import("./offline-storage");
    vi.mocked(countPendingResponsesStrict).mockResolvedValue(3);
    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    const count = await offlineQueue.loadPersistedQueue();
    expect(count).toBe(3);
    expect(countPendingResponsesStrict).toHaveBeenCalledWith("s1");
  });

  test("getPendingCount returns 0 when counting IndexedDB entries rejects", async () => {
    const { countPendingResponsesStrict } = await import("./offline-storage");
    const error = new Error("count failed");
    vi.mocked(countPendingResponsesStrict).mockRejectedValueOnce(error);
    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );

    const count = await offlineQueue.getPendingCount();

    expect(count).toBe(0);
    expect(console.error).toHaveBeenCalledWith(
      "Forma: Failed to count pending responses in IndexedDB",
      expect.objectContaining({ error, surveyId: "s1" })
    );
  });

  test("getPendingCount returns 0 when persistOffline is disabled", async () => {
    const count = await queue.getPendingCount();
    expect(count).toBe(0);
  });

  test("syncPersistedResponses returns early when persistOffline is disabled", async () => {
    const result = await queue.syncPersistedResponses();
    expect(result).toEqual({ success: true, syncedCount: 0 });
  });

  test("syncPersistedResponses returns early when already syncing", async () => {
    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue["isSyncing"] = true;
    const result = await offlineQueue.syncPersistedResponses();
    expect(result).toEqual({ success: false, syncedCount: 0 });
  });

  test("syncPersistedResponses returns early when a processQueue request is in flight", async () => {
    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue["isRequestInProgress"] = true;
    const result = await offlineQueue.syncPersistedResponses();
    expect(result).toEqual({ success: false, syncedCount: 0 });
  });

  test("syncPersistedResponses returns failure when reading IndexedDB rejects", async () => {
    const { getPendingResponsesStrict } = await import("./offline-storage");
    const error = new Error("read failed");
    vi.mocked(getPendingResponsesStrict).mockRejectedValueOnce(error);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );

    const result = await offlineQueue.syncPersistedResponses();

    expect(result).toEqual({ success: false, syncedCount: 0 });
    expect(offlineQueue["isSyncing"]).toBe(false);
    expect(console.error).toHaveBeenCalledWith(
      "Forma: Failed to sync persisted responses from IndexedDB",
      expect.objectContaining({ error, surveyId: "s1" })
    );
  });

  test("syncPersistedResponses sends entries and clears queue on success", async () => {
    const { getPendingResponsesStrict, removePendingResponseStrict } = await import("./offline-storage");
    vi.mocked(getPendingResponsesStrict).mockResolvedValue([
      {
        id: 10,
        surveyId: "s1",
        responseUpdate,
        surveyStateSnapshot: {
          responseId: null,
          displayId: "d1",
          surveyId: "s1",
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: { finished: false, data: {} },
        },
        createdAt: Date.now(),
      },
    ]);

    const offlineState = getSurveyState();
    const offlineQueue = new ResponseQueue(getConfig({ persistOffline: true, surveyId: "s1" }), offlineState);
    offlineQueue.queue.push(responseUpdate);
    offlineQueue["pendingDbIds"].set(responseUpdate, 10);

    vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(ok(true));

    const result = await offlineQueue.syncPersistedResponses();
    expect(result).toEqual({ success: true, syncedCount: 1 });
    expect(removePendingResponseStrict).toHaveBeenCalledWith(10);
    expect(offlineQueue.queue).toHaveLength(0);
    expect(offlineQueue["isSyncing"]).toBe(false);
  });

  test("syncPersistedResponses removes synced queue items by IndexedDB id", async () => {
    const { getPendingResponsesStrict, removePendingResponseStrict } = await import("./offline-storage");
    const inFlightUpdate = { ...responseUpdate, data: { step: "in-flight" } };
    const syncedUpdate = { ...responseUpdate, data: { step: "synced" } };

    vi.mocked(getPendingResponsesStrict).mockResolvedValueOnce([
      {
        id: 10,
        surveyId: "s1",
        responseUpdate: syncedUpdate,
        surveyStateSnapshot: {
          responseId: null,
          displayId: "d1",
          surveyId: "s1",
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: { finished: false, data: {} },
        },
        createdAt: Date.now(),
      },
    ]);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push(inFlightUpdate, syncedUpdate);
    offlineQueue["pendingDbIds"].set(syncedUpdate, 10);

    vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(ok(true));
    vi.spyOn(offlineQueue, "processQueue").mockResolvedValue({ success: false });

    const result = await offlineQueue.syncPersistedResponses();

    expect(result).toEqual({ success: true, syncedCount: 1 });
    expect(removePendingResponseStrict).toHaveBeenCalledWith(10);
    expect(offlineQueue.queue).toEqual([inFlightUpdate]);
    expect(offlineQueue["pendingDbIds"].has(syncedUpdate)).toBe(false);
  });

  test("syncPersistedResponses returns failure when removing a synced IndexedDB entry rejects", async () => {
    const { getPendingResponsesStrict, removePendingResponseStrict } = await import("./offline-storage");
    const error = new Error("delete failed");
    vi.mocked(getPendingResponsesStrict).mockResolvedValueOnce([
      {
        id: 10,
        surveyId: "s1",
        responseUpdate,
        surveyStateSnapshot: {
          responseId: null,
          displayId: "d1",
          surveyId: "s1",
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: { finished: false, data: {} },
        },
        createdAt: Date.now(),
      },
    ]);
    vi.mocked(removePendingResponseStrict).mockRejectedValueOnce(error);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push(responseUpdate);
    offlineQueue["pendingDbIds"].set(responseUpdate, 10);
    vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(ok(true));

    const result = await offlineQueue.syncPersistedResponses();

    expect(result).toEqual({ success: false, syncedCount: 0 });
    expect(offlineQueue.queue).toHaveLength(0);
    expect(offlineQueue["isSyncing"]).toBe(false);
    expect(console.error).toHaveBeenCalledWith(
      "Forma: Failed to remove synced response from IndexedDB",
      expect.objectContaining({ error, surveyId: "s1" })
    );
  });

  test("syncPersistedResponses stops on server error", async () => {
    const { getPendingResponsesStrict } = await import("./offline-storage");
    vi.mocked(getPendingResponsesStrict).mockResolvedValue([
      {
        id: 10,
        surveyId: "s1",
        responseUpdate,
        surveyStateSnapshot: {
          responseId: null,
          displayId: "d1",
          surveyId: "s1",
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: { finished: false, data: {} },
        },
        createdAt: Date.now(),
      },
    ]);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(
      err({ code: "internal_server_error", message: "fail", status: 500 })
    );

    const result = await offlineQueue.syncPersistedResponses();
    expect(result).toEqual({ success: false, syncedCount: 0 });
    expect(offlineQueue["isSyncing"]).toBe(false);
  });

  test("syncPersistedResponses retries 404 as createResponse by resetting responseId", async () => {
    const { getPendingResponsesStrict, removePendingResponseStrict } = await import("./offline-storage");
    vi.mocked(getPendingResponsesStrict).mockResolvedValue([
      {
        id: 10,
        surveyId: "s1",
        responseUpdate,
        surveyStateSnapshot: {
          responseId: "r1",
          displayId: "d1",
          surveyId: "s1",
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: { finished: false, data: {} },
        },
        createdAt: Date.now(),
      },
    ]);

    // Use a state that actually mutates on updateResponseId/updateDisplayId
    const offlineState = getSurveyState();
    offlineState.updateResponseId = vi.fn((id: string) => {
      offlineState.responseId = id;
    });
    offlineState.updateDisplayId = vi.fn((id: string) => {
      offlineState.displayId = id;
    });

    const offlineQueue = new ResponseQueue(getConfig({ persistOffline: true, surveyId: "s1" }), offlineState);

    // First call: 404 (updateResponse fails), second call: retry as createResponse succeeds
    vi.spyOn(offlineQueue, "sendResponse")
      .mockResolvedValueOnce(err({ code: "not_found", message: "not found", status: 404 }))
      .mockResolvedValueOnce(ok(true));

    const result = await offlineQueue.syncPersistedResponses();
    expect(result).toEqual({ success: true, syncedCount: 1 });
    expect(offlineQueue.sendResponse).toHaveBeenCalledTimes(2);
    expect(removePendingResponseStrict).toHaveBeenCalledWith(10);
    // responseId should have been reset to null for the retry
    expect(offlineState.responseId).toBeNull();
  });

  test("syncPersistedResponses removes terminal 409 entries and continues", async () => {
    const { getPendingResponsesStrict, removePendingResponseStrict } = await import("./offline-storage");
    vi.mocked(getPendingResponsesStrict).mockResolvedValue([
      {
        id: 10,
        surveyId: "s1",
        responseUpdate,
        surveyStateSnapshot: {
          responseId: null,
          displayId: "d1",
          surveyId: "s1",
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: { finished: false, data: {} },
        },
        createdAt: Date.now(),
      },
      {
        id: 11,
        surveyId: "s1",
        responseUpdate,
        surveyStateSnapshot: {
          responseId: null,
          displayId: "d1",
          surveyId: "s1",
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: { finished: false, data: {} },
        },
        createdAt: Date.now(),
      },
    ]);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    vi.spyOn(offlineQueue, "sendResponse")
      .mockResolvedValueOnce(err({ code: "bad_request", message: "already completed", status: 409 }))
      .mockResolvedValueOnce(ok(true));

    const result = await offlineQueue.syncPersistedResponses();
    expect(result).toEqual({ success: true, syncedCount: 1 });
    expect(removePendingResponseStrict).toHaveBeenCalledWith(10);
    expect(removePendingResponseStrict).toHaveBeenCalledWith(11);
  });

  test("syncPersistedResponses keeps recoverable 4xx entries", async () => {
    const { getPendingResponsesStrict, removePendingResponseStrict } = await import("./offline-storage");
    vi.mocked(getPendingResponsesStrict).mockResolvedValueOnce([
      {
        id: 10,
        surveyId: "s1",
        responseUpdate,
        surveyStateSnapshot: {
          responseId: null,
          displayId: "d1",
          surveyId: "s1",
          singleUseId: null,
          userId: null,
          contactId: null,
          responseAcc: { finished: false, data: {} },
        },
        createdAt: Date.now(),
      },
    ]);

    const offlineQueue = new ResponseQueue(
      getConfig({ persistOffline: true, surveyId: "s1" }),
      getSurveyState()
    );
    offlineQueue.queue.push(responseUpdate);
    offlineQueue["pendingDbIds"].set(responseUpdate, 10);

    vi.spyOn(offlineQueue, "sendResponse").mockResolvedValue(
      err({ code: "unauthorized", message: "unauthorized", status: 401 })
    );

    const result = await offlineQueue.syncPersistedResponses();

    expect(result).toEqual({ success: false, syncedCount: 0 });
    expect(removePendingResponseStrict).not.toHaveBeenCalledWith(10);
    expect(offlineQueue.queue).toEqual([responseUpdate]);
    expect(offlineQueue["pendingDbIds"].get(responseUpdate)).toBe(10);
    expect(offlineQueue["isSyncing"]).toBe(false);
  });

  // R-01 and R-02: a survey keeps one queue, and an answer that cannot be sent waits in it instead of being lost or ending the survey.
  describe("delivery over a slow or broken connection", () => {
    const serverError = {
      ok: false,
      error: { code: "internal_server_error", message: "Server error", status: 500 },
    };
    const accepted = { ok: true, data: { quotaFull: false } };

    afterEach(() => {
      vi.useRealTimers();
    });

    test("a second queue for the same survey is not blocked by the first queue's request in flight", async () => {
      const stateA = getSurveyState();
      stateA.responseId = "resp-a";
      const stateB = getSurveyState();
      stateB.responseId = "resp-b";
      const queueA = new ResponseQueue(getConfig({ surveyId: "s1" }), stateA);
      const queueB = new ResponseQueue(getConfig({ surveyId: "s1" }), stateB);
      const apiA = queueA.api as any;
      const apiB = queueB.api as any;
      let releaseA!: () => void;
      apiA.updateResponse.mockImplementation(
        () =>
          new Promise((resolve) => {
            releaseA = () => resolve(accepted);
          })
      );
      apiB.updateResponse.mockResolvedValue(accepted);

      queueA.add({ data: { q1: "a" }, finished: false });
      await flushPromises();
      queueB.add({ data: { q1: "b" }, finished: false });
      await flushPromises();

      expect(apiB.updateResponse).toHaveBeenCalledWith(
        expect.objectContaining({ responseId: "resp-b", data: { q1: "b" } })
      );
      expect(queueB.queue).toHaveLength(0);

      releaseA();
      await flushPromises();
      expect(queueA.queue).toHaveLength(0);
    });

    test("answers submitted while a slow request is in flight are all sent, in order", async () => {
      vi.useFakeTimers();
      surveyState.responseId = "resp1";
      apiMock.updateResponse.mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve(accepted), 1000))
      );

      queue.add({ data: { q1: "one" }, finished: false });
      queue.add({ data: { q2: "two" }, finished: false });
      queue.add({ data: { q3: "three" }, finished: true });
      await vi.advanceTimersByTimeAsync(3500);

      expect(apiMock.updateResponse.mock.calls.map(([update]: [TResponseUpdate]) => update.data)).toEqual([
        { q1: "one" },
        { q2: "two" },
        { q3: "three" },
      ]);
      expect(queue.queue).toHaveLength(0);
      expect(config.onResponseSendingFinished).toHaveBeenCalledTimes(1);
      expect(config.onResponseSendingFailed).not.toHaveBeenCalled();
    });

    test("a card in the middle of the survey that cannot be sent does not fail the survey, and goes out when the browser is back online", async () => {
      vi.useFakeTimers();
      surveyState.responseId = "resp1";
      const onResponseSendingDelayedChange = vi.fn();
      queue = new ResponseQueue(getConfig({ onResponseSendingDelayedChange }), surveyState);
      const api = queue.api as any;
      api.updateResponse.mockResolvedValue(serverError);

      queue.add({ data: { q1: "one" }, finished: false });
      await vi.advanceTimersByTimeAsync(0);
      expect(onResponseSendingDelayedChange).toHaveBeenLastCalledWith(true);

      // The whole burst (a first attempt and two retries after 1 s and 2 s) fails.
      await vi.advanceTimersByTimeAsync(3000);
      expect(api.updateResponse).toHaveBeenCalledTimes(3);
      expect(queue.config.onResponseSendingFailed).not.toHaveBeenCalled();
      expect(queue.queue).toHaveLength(1);

      api.updateResponse.mockResolvedValue(accepted);
      globalThis.window.dispatchEvent(new Event("online"));
      await vi.advanceTimersByTimeAsync(0);

      expect(api.updateResponse).toHaveBeenCalledTimes(4);
      expect(queue.queue).toHaveLength(0);
      expect(onResponseSendingDelayedChange).toHaveBeenLastCalledWith(false);
    });

    test("an unsent card keeps being retried in the background on a growing backoff", async () => {
      vi.useFakeTimers();
      surveyState.responseId = "resp1";
      apiMock.updateResponse.mockResolvedValue(serverError);

      queue.add({ data: { q1: "one" }, finished: false });
      await vi.advanceTimersByTimeAsync(3000);
      expect(apiMock.updateResponse).toHaveBeenCalledTimes(3);

      await vi.advanceTimersByTimeAsync(RECOVERY_BASE_DELAY_MS);
      expect(apiMock.updateResponse).toHaveBeenCalledTimes(4);

      // The next round waits twice as long.
      await vi.advanceTimersByTimeAsync(RECOVERY_BASE_DELAY_MS);
      expect(apiMock.updateResponse).toHaveBeenCalledTimes(4);

      apiMock.updateResponse.mockResolvedValue(accepted);
      await vi.advanceTimersByTimeAsync(RECOVERY_BASE_DELAY_MS);
      expect(apiMock.updateResponse).toHaveBeenCalledTimes(5);
      expect(queue.queue).toHaveLength(0);
      expect(config.onResponseSendingFailed).not.toHaveBeenCalled();
    });

    test("coming back online ends the backoff wait of a burst in flight", async () => {
      vi.useFakeTimers();
      surveyState.responseId = "resp1";
      apiMock.updateResponse.mockResolvedValueOnce(serverError).mockResolvedValue(accepted);

      queue.add({ data: { q1: "one" }, finished: false });
      await vi.advanceTimersByTimeAsync(0);
      expect(apiMock.updateResponse).toHaveBeenCalledTimes(1);

      globalThis.window.dispatchEvent(new Event("online"));
      await vi.advanceTimersByTimeAsync(0);

      expect(apiMock.updateResponse).toHaveBeenCalledTimes(2);
      expect(queue.queue).toHaveLength(0);
    });

    test("a final submission that cannot be sent is reported, and Retry sends everything still queued", async () => {
      vi.useFakeTimers();
      surveyState.responseId = "resp1";
      apiMock.updateResponse.mockResolvedValue(serverError);

      queue.add({ data: { q1: "one" }, finished: false });
      queue.add({ data: { q2: "two" }, finished: true });
      await vi.advanceTimersByTimeAsync(3000);

      expect(config.onResponseSendingFailed).toHaveBeenCalledWith(
        expect.objectContaining({ data: { q1: "one" } }),
        TResponseErrorCodesEnum.ResponseSendingError
      );
      expect(queue.getUnsentData()).toEqual({ q1: "one", q2: "two" });

      apiMock.updateResponse.mockClear();
      apiMock.updateResponse.mockResolvedValue(accepted);
      const result = await queue.processQueue();
      await vi.advanceTimersByTimeAsync(0);

      expect(result.success).toBe(true);
      expect(apiMock.updateResponse.mock.calls.map(([update]: [TResponseUpdate]) => update.data)).toEqual([
        { q1: "one" },
        { q2: "two" },
      ]);
      expect(queue.queue).toHaveLength(0);
      expect(config.onResponseSendingFinished).toHaveBeenCalledTimes(1);
    });

    // With offline support every answer is also stored in IndexedDB, and once two are stored processQueue leaves the sending to syncPersistedResponses.
    describe("with offline support", () => {
      let rows: PendingResponseEntry[];

      beforeEach(async () => {
        rows = [];
        let nextId = 0;
        const storage = await import("./offline-storage");
        vi.mocked(storage.addPendingResponse).mockImplementation(async (entry) => {
          nextId += 1;
          rows.push({ ...entry, id: nextId });
          return nextId;
        });
        vi.mocked(storage.countPendingResponsesStrict).mockImplementation(async () => rows.length);
        vi.mocked(storage.getPendingResponsesStrict).mockImplementation(async () =>
          rows.map((row) => ({ ...row }))
        );
        vi.mocked(storage.removePendingResponseStrict).mockImplementation(async (id) => {
          rows = rows.filter((row) => row.id !== id);
        });
      });

      afterEach(async () => {
        const storage = await import("./offline-storage");
        vi.mocked(storage.addPendingResponse).mockReset().mockResolvedValue(1);
        vi.mocked(storage.countPendingResponsesStrict).mockReset().mockResolvedValue(0);
        vi.mocked(storage.getPendingResponsesStrict).mockReset().mockResolvedValue([]);
        vi.mocked(storage.removePendingResponseStrict).mockReset().mockResolvedValue(undefined);
      });

      const offlineQueueWith = (overrides = {}) => {
        surveyState.responseId = "resp1";
        const offlineConfig = getConfig({ persistOffline: true, surveyId: "s1", ...overrides });
        const offlineQueue = new ResponseQueue(offlineConfig, surveyState);
        return { offlineConfig, offlineQueue, api: offlineQueue.api as any };
      };

      test("a final submission sent together with an earlier unsent card ends the survey and clears the delayed notice", async () => {
        vi.useFakeTimers();
        const onResponseSendingDelayedChange = vi.fn();
        const { offlineConfig, offlineQueue, api } = offlineQueueWith({ onResponseSendingDelayedChange });
        api.updateResponse.mockResolvedValue(serverError);

        offlineQueue.add({ data: { q1: "one" }, finished: false });
        await vi.advanceTimersByTimeAsync(3000);
        expect(api.updateResponse).toHaveBeenCalledTimes(3);
        expect(onResponseSendingDelayedChange).toHaveBeenLastCalledWith(true);

        // The server is back; the respondent finishes before the background retry is due.
        api.updateResponse.mockResolvedValue(accepted);
        offlineQueue.add({ data: { q2: "two" }, finished: true });
        await vi.advanceTimersByTimeAsync(0);

        expect(
          api.updateResponse.mock.calls.slice(3).map(([update]: [TResponseUpdate]) => update.data)
        ).toEqual([{ q1: "one" }, { q2: "two" }]);
        expect(rows).toHaveLength(0);
        expect(offlineQueue.queue).toHaveLength(0);
        expect(offlineConfig.onResponseSendingFinished).toHaveBeenCalledTimes(1);
        expect(onResponseSendingDelayedChange).toHaveBeenLastCalledWith(false);
        expect(offlineConfig.onResponseSendingFailed).not.toHaveBeenCalled();

        // Nothing is left to retry.
        await vi.advanceTimersByTimeAsync(RECOVERY_BASE_DELAY_MS * 4);
        expect(api.updateResponse).toHaveBeenCalledTimes(5);
      });

      test("stored answers that cannot be sent keep being retried in the background", async () => {
        vi.useFakeTimers();
        const onResponseSendingDelayedChange = vi.fn();
        const { offlineConfig, offlineQueue, api } = offlineQueueWith({ onResponseSendingDelayedChange });
        api.updateResponse.mockResolvedValue(serverError);

        offlineQueue.add({ data: { q1: "one" }, finished: false });
        await vi.advanceTimersByTimeAsync(3000);
        offlineQueue.add({ data: { q2: "two" }, finished: true });
        await vi.advanceTimersByTimeAsync(0);
        expect(api.updateResponse).toHaveBeenCalledTimes(4);
        expect(rows).toHaveLength(2);

        api.updateResponse.mockResolvedValue(accepted);
        await vi.advanceTimersByTimeAsync(RECOVERY_BASE_DELAY_MS);

        expect(api.updateResponse).toHaveBeenCalledTimes(6);
        expect(rows).toHaveLength(0);
        expect(offlineQueue.queue).toHaveLength(0);
        expect(offlineConfig.onResponseSendingFinished).toHaveBeenCalledTimes(1);
        expect(onResponseSendingDelayedChange).toHaveBeenLastCalledWith(false);
      });

      test("an answer given while stored answers are being synced is sent after them", async () => {
        const { offlineQueue, api } = offlineQueueWith();
        let releaseFirst!: () => void;
        api.updateResponse
          .mockImplementationOnce(
            () =>
              new Promise((resolve) => {
                releaseFirst = () => resolve(accepted);
              })
          )
          .mockResolvedValue(accepted);
        offlineQueue.queue.push(
          { data: { q1: "one" }, finished: false },
          { data: { q2: "two" }, finished: false }
        );
        rows.push(
          {
            id: 101,
            surveyId: "s1",
            responseUpdate: offlineQueue.queue[0],
            surveyStateSnapshot: { ...offlineQueue["serializeSurveyState"]() },
            createdAt: Date.now(),
          },
          {
            id: 102,
            surveyId: "s1",
            responseUpdate: offlineQueue.queue[1],
            surveyStateSnapshot: { ...offlineQueue["serializeSurveyState"]() },
            createdAt: Date.now(),
          }
        );
        offlineQueue["pendingDbIds"].set(offlineQueue.queue[0], 101);
        offlineQueue["pendingDbIds"].set(offlineQueue.queue[1], 102);

        const sync = offlineQueue.syncPersistedResponses();
        await flushPromises();
        offlineQueue.add({ data: { q3: "three" }, finished: true });
        await flushPromises();
        releaseFirst();
        await sync;
        await flushPromises();
        await flushPromises();

        expect(api.updateResponse.mock.calls.map(([update]: [TResponseUpdate]) => update.data)).toEqual([
          { q1: "one" },
          { q2: "two" },
          { q3: "three" },
        ]);
        expect(rows).toHaveLength(0);
        expect(offlineQueue.queue).toHaveLength(0);
      });
    });
  });
});
