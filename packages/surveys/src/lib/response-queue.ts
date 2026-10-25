import { Result, err, ok } from "@forma/types/error-handlers";
import { ApiErrorResponse, RESPONSE_ALREADY_FINISHED_ERROR_CODE } from "@forma/types/errors";
import { TQuotaFullResponse } from "@forma/types/quota";
import { TResponseUpdate } from "@forma/types/responses";
import { RECAPTCHA_VERIFICATION_ERROR_CODE } from "@/lib/constants";
import { TResponseErrorCodesEnum } from "@/types/response-error-codes";
import { ApiClient } from "./api-client";
import {
  type PendingResponseEntry,
  type SerializedSurveyState,
  addPendingResponse,
  countPendingResponsesStrict,
  getPendingResponsesStrict,
  removePendingResponseStrict,
} from "./offline-storage";
import { SurveyState } from "./survey-state";

// An id that is absent, null or empty is no id.
const normalizeId = (id: string | null | undefined): string | null =>
  id === undefined || id === "" ? null : id;

type TRespondentIds = Pick<SerializedSurveyState, "singleUseId" | "contactId" | "userId">;

/** Whether two sets of ids name the same respondent: the same single-use link, personal (contact) link and user id. */
export const isSameRespondent = (a: TRespondentIds, b: TRespondentIds): boolean =>
  normalizeId(a.singleUseId) === normalizeId(b.singleUseId) &&
  normalizeId(a.contactId) === normalizeId(b.contactId) &&
  normalizeId(a.userId) === normalizeId(b.userId);

// Two ids that are both known must be equal; an id not known yet on one side (the display or response was still being created when the row was stored) does not tell the sessions apart.
const isSameIfBothKnown = (a: string | null | undefined, b: string | null | undefined): boolean => {
  const left = normalizeId(a);
  const right = normalizeId(b);
  return left === null || right === null || left === right;
};

interface QueueConfig {
  appUrl: string;
  workspaceId: string;
  retryAttempts: number;
  /** Stores every answer in IndexedDB until the server has it, so answers a closed or reloaded page could not send are sent on the next visit (syncPersistedResponses). */
  persistOffline?: boolean;
  /** The survey's offline mode: while the browser reports it is offline, answers wait in IndexedDB without a send being attempted, and a failed send is not reported. Only takes effect with persistOffline. */
  holdWhileOffline?: boolean;
  /**
   * Answers an earlier page stored in IndexedDB are sent each with the ids stored with it (its own response, display, single-use link, contact and user) and never change this page's response, unless they belong to the response this page goes on with (resumeEarlierResponses). Without it, a stored answer of another respondent, or of a visit whose progress was discarded, would point this page's answers at that earlier response.
   */
  isolateEarlierResponses?: boolean;
  surveyId?: string;
  onResponseCreated?: (responseId: string) => void;
  onResponseSendingFailed?: (responseUpdate: TResponseUpdate, errorCode?: TResponseErrorCodesEnum) => void;
  onResponseSendingFinished?: () => void;
  /**
   * Called with `true` when a send attempt fails for a reason that may pass (the network or the server), and with `false` once a send succeeds again. The queue keeps the answers meanwhile; a survey shows a non-blocking notice and lets the respondent carry on.
   */
  onResponseSendingDelayedChange?: (isDelayed: boolean) => void;
  onQuotaFull?: (quotaInfo: TQuotaFullResponse) => void;
  setSurveyState?: (state: SurveyState) => void;
}

interface SyncOutcome {
  // Every stored row was either delivered or dropped as permanently rejected.
  drained: boolean;
  // A row could not be sent because of the network or the server; it stays stored.
  sendFailed: boolean;
  // A delivered row was the final submission.
  sentFinished: boolean;
  quotaFullResponse?: TQuotaFullResponse;
}

export const delay = (ms: number): Promise<void> => {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
};

// After a burst of retries has failed, the queue tries again on its own for as long as the page is open: at once when the browser reports it is back online, otherwise after a backoff that starts at RECOVERY_BASE_DELAY_MS and doubles up to RECOVERY_MAX_DELAY_MS.
export const RECOVERY_BASE_DELAY_MS = 5_000;
export const RECOVERY_MAX_DELAY_MS = 60_000;

/**
 * One queue per response being collected. Its locks belong to the instance: a survey keeps a single queue for its whole life, so the in-flight request and the answers waiting behind it are always held by the same object. Locks shared by surveyId let a second instance (a remount, or a second embed of the same survey) see "a request is in flight" and leave its own answers queued with nobody left to send them.
 */
export class ResponseQueue {
  readonly queue: TResponseUpdate[] = [];
  readonly config: QueueConfig;
  private surveyState: SurveyState;
  readonly api: ApiClient;
  private responseRecaptchaToken?: string;
  // Maps queued response objects to their IndexedDB ids for cleanup after successful sends.
  private readonly pendingDbIds: Map<TResponseUpdate, number> = new Map();
  private isSyncing = false;
  private isRequestInProgress = false;
  // IndexedDB rows already sent whose removal failed; skipped by the next sync so they are not sent twice.
  private readonly processedDbIdsPendingRemoval = new Set<number>();
  private isDelayed = false;
  private recoveryTimer: ReturnType<typeof setTimeout> | null = null;
  private recoveryRound = 0;
  private isListeningForOnline = false;
  // Ends the backoff wait of the burst in flight, so a send can go out as soon as the connection is back.
  private wakeBackoff: (() => void) | null = null;
  // When this queue was created: IndexedDB rows stored before it were left by an earlier page.
  private readonly createdAt = Date.now();
  // The stored session this page goes on with (resumeEarlierResponses). Earlier rows of any other session are sent on their own (isolateEarlierResponses).
  private resumedSnapshot: SerializedSurveyState | null = null;

  constructor(config: QueueConfig, surveyState: SurveyState) {
    this.config = config;
    this.surveyState = surveyState;
    this.api = new ApiClient({
      appUrl: config.appUrl,
      workspaceId: config.workspaceId,
    });
  }

  setResponseRecaptchaToken(token?: string) {
    this.responseRecaptchaToken = token;
  }

  private serializeSurveyState(): SerializedSurveyState {
    return {
      responseId: this.surveyState.responseId,
      displayId: this.surveyState.displayId,
      surveyId: this.surveyState.surveyId,
      singleUseId: this.surveyState.singleUseId,
      userId: this.surveyState.userId,
      contactId: this.surveyState.contactId,
      responseAcc: { ...this.surveyState.responseAcc },
    };
  }

  private logOfflinePersistenceError(message: string, error: unknown) {
    console.error(`Forma: ${message}`, {
      error,
      surveyId: this.config.surveyId,
    });
  }

  private processQueueInBackground(context: string, retryAttempts?: number) {
    void this.processQueue(retryAttempts).catch((error) => {
      this.logOfflinePersistenceError(context, error);
    });
  }

  private syncPersistedResponsesInBackground(context: string) {
    void this.syncPersistedResponses().catch((error) => {
      this.logOfflinePersistenceError(context, error);
    });
  }

  private async countPersistedResponses(): Promise<number> {
    if (!this.config.surveyId) return 0;

    try {
      const pendingCount = await countPendingResponsesStrict(this.config.surveyId);
      return Math.max(0, pendingCount - this.processedDbIdsPendingRemoval.size);
    } catch (error) {
      this.logOfflinePersistenceError("Failed to count pending responses in IndexedDB", error);
      throw error;
    }
  }

  private removePendingDbId(dbId: number) {
    for (const [responseUpdate, pendingDbId] of this.pendingDbIds.entries()) {
      if (pendingDbId === dbId) {
        this.pendingDbIds.delete(responseUpdate);
        return;
      }
    }
  }

  private async removePendingResponseFromIndexedDB(
    dbId: number,
    context: string,
    options: { removeTrackedDbIdOnSuccess?: boolean } = {}
  ): Promise<boolean> {
    try {
      await removePendingResponseStrict(dbId);
      this.processedDbIdsPendingRemoval.delete(dbId);
      if (options.removeTrackedDbIdOnSuccess ?? true) {
        this.removePendingDbId(dbId);
      }
      return true;
    } catch (error) {
      this.processedDbIdsPendingRemoval.add(dbId);
      this.logOfflinePersistenceError(context, error);
      return false;
    }
  }

  private async retryProcessedDbIdCleanup() {
    for (const dbId of Array.from(this.processedDbIdsPendingRemoval)) {
      await this.removePendingResponseFromIndexedDB(
        dbId,
        "Failed to remove processed response from IndexedDB"
      );
    }
  }

  private removeProcessedQueueItemsByDbId(processedDbIds: Set<number>) {
    if (processedDbIds.size === 0) return;

    for (let index = this.queue.length - 1; index >= 0; index--) {
      const item = this.queue[index];
      const pendingDbId = this.pendingDbIds.get(item);

      if (pendingDbId !== undefined && processedDbIds.has(pendingDbId)) {
        this.queue.splice(index, 1);
      }
    }

    for (const [item, pendingDbId] of this.pendingDbIds.entries()) {
      if (processedDbIds.has(pendingDbId)) {
        this.pendingDbIds.delete(item);
      }
    }
  }

  /**
   * 4xx client errors are permanent: the server is reachable and is rejecting the request.
   * Re-sending the identical payload will keep failing, so these must not be retried
   * (neither automatic backoff nor manual Retry) and the item must be dropped from the queue.
   *
   * Excluded (kept for retry) — the semantically transient/recoverable 4xx codes:
   *   401 (auth may recover after a token refresh),
   *   404 (the offline sync path treats it as "create never reached the server" and retries
   *        as a fresh createResponse),
   *   408 (Request Timeout) and 425 (Too Early) — explicitly retryable,
   *   429 (rate limited — transient).
   */
  private static readonly RETRYABLE_CLIENT_ERROR_STATUSES = new Set([401, 404, 408, 425, 429]);

  private isTerminalClientError(error?: ApiErrorResponse): boolean {
    if (!error) return false;
    return (
      error.status >= 400 &&
      error.status < 500 &&
      !ResponseQueue.RETRYABLE_CLIENT_ERROR_STATUSES.has(error.status)
    );
  }

  /**
   * The response can no longer be updated because it was already finalized out-of-band
   * (e.g. a second tab / email link scanner finished the same single-use response).
   *
   * Primary signal is the stable, locale-independent marker the server sets in details.code.
   * The 409 status and the legacy English-message check are kept as fallbacks so detection
   * still works against older servers that predate the marker.
   */
  private isResponseAlreadyCompletedError(error?: ApiErrorResponse): boolean {
    if (!error) return false;
    return (
      error.details?.code === RESPONSE_ALREADY_FINISHED_ERROR_CODE ||
      error.status === 409 ||
      (error.status === 400 &&
        typeof error.message === "string" &&
        error.message.includes("already finished"))
    );
  }

  private getTerminalClientErrorCode(error?: ApiErrorResponse): TResponseErrorCodesEnum {
    // Terminal 4xx items are dropped from the queue, so a manual Retry is pointless — return a
    // non-retryable code (never ResponseSendingError, which renders the retryable "servers
    // cannot be reached" UI).
    return this.isResponseAlreadyCompletedError(error)
      ? TResponseErrorCodesEnum.ResponseAlreadyCompleted
      : TResponseErrorCodesEnum.ResponseSendingErrorPermanent;
  }

  add(responseUpdate: TResponseUpdate) {
    // update survey state
    this.surveyState.accumulateResponse(responseUpdate);
    if (this.config.setSurveyState) {
      this.config.setSurveyState(this.surveyState);
    }
    // add response to queue
    this.queue.push(responseUpdate);

    // persist to IndexedDB if offline persistence is enabled,
    // then start processing only after the DB write completes
    if (this.config.persistOffline && this.config.surveyId) {
      void addPendingResponse({
        surveyId: this.config.surveyId,
        responseUpdate,
        surveyStateSnapshot: this.serializeSurveyState(),
        createdAt: Date.now(),
      })
        .then((dbId) => {
          if (dbId > 0) {
            this.pendingDbIds.set(responseUpdate, dbId);
          }
          this.processQueueInBackground("Failed to process queue after persisting response");
        })
        .catch((error) => {
          this.logOfflinePersistenceError("Failed to persist pending response to IndexedDB", error);
          this.processQueueInBackground("Failed to process queue after IndexedDB persistence failure");
        });
    } else {
      this.processQueueInBackground("Failed to process queue after adding response");
    }
  }

  /**
   * Sends the queued answers in order, retrying a failed send `retryAttempts` times with exponential backoff. An answer that still cannot be sent stays queued: the queue keeps retrying in the background, and only a failed final submission is reported through `onResponseSendingFailed`.
   */
  async processQueue(retryAttempts = this.config.retryAttempts): Promise<{ success: boolean }> {
    if (this.isRequestInProgress || this.queue.length === 0) {
      return { success: false };
    }

    // In offline mode, don't attempt to send while offline — data is safe in IndexedDB.
    if (this.isHoldingWhileOffline()) {
      return { success: false };
    }

    // Don't send while syncPersistedResponses is draining IndexedDB — it handles everything.
    if (this.isSyncing) {
      return { success: false };
    }

    // When offline support is active and there are multiple pending entries in
    // IndexedDB, defer to syncPersistedResponses which drains them in order.
    // This prevents processQueue and syncPersistedResponses from racing to
    // create the same response concurrently (duplicate POSTs).
    if (this.config.persistOffline && this.config.surveyId) {
      let pendingCount: number;
      try {
        await this.retryProcessedDbIdCleanup();
        pendingCount = await this.countPersistedResponses();
      } catch {
        // IndexedDB cannot be read (storage blocked in this frame, a browser without it): it cannot hand the answers to syncPersistedResponses either, so the queue in memory is sent as it is rather than not at all.
        pendingCount = 0;
      }

      // Re-check after await — another processQueue/sync may have started during the yield
      if (this.isSyncing || this.isRequestInProgress || this.queue.length === 0) {
        return { success: false };
      }

      if (pendingCount > 1) {
        this.syncPersistedResponsesInBackground("Failed to sync persisted responses in background");
        return { success: false };
      }
    }

    const responseUpdate = this.queue[0];
    this.isRequestInProgress = true;
    // This run supersedes a scheduled background retry; a failure below schedules the next one.
    this.clearRecoveryTimer();
    const result = await this.sendResponseWithRetry(responseUpdate, retryAttempts);

    if (result.success) {
      // Remove from IndexedDB on successful send
      const dbId = this.pendingDbIds.get(responseUpdate);
      if (dbId !== undefined) {
        await this.removePendingResponseFromIndexedDB(dbId, "Failed to remove sent response from IndexedDB");
      }

      this.handleSuccessfulResponse(responseUpdate, result.quotaFullResponse);
      return { success: true };
    } else {
      // In offline mode, a send that failed because the browser went offline is not a failure
      if (this.isHoldingWhileOffline()) {
        this.isRequestInProgress = false;
        this.startRecovery();
        return { success: false };
      }

      // Permanent 4xx rejection: drop the dead item so the manual Retry button can't
      // re-send the identical payload forever.
      if (result.isTerminalClientError) {
        this.queue.shift();
        const dbId = this.pendingDbIds.get(responseUpdate);
        if (dbId !== undefined) {
          await this.removePendingResponseFromIndexedDB(
            dbId,
            "Failed to remove rejected response from IndexedDB"
          );
        }
      }

      if (result.isTerminalClientError || result.isRecaptchaError) {
        // Sending the same payload again cannot succeed.
        this.stopRecovery();
      } else {
        this.startRecovery();
        // A card in the middle of the survey does not interrupt the respondent: its answers stay queued and go out with the next successful send. Only the final submission, the one the ending card waits for, is reported as failed.
        if (!this.queue.some((item) => item.finished)) {
          this.isRequestInProgress = false;
          return { success: false };
        }
      }

      this.handleFailedResponse(responseUpdate, result.isRecaptchaError, result.errorCode);
      return { success: false };
    }
  }

  private isHoldingWhileOffline(): boolean {
    return (
      Boolean(this.config.persistOffline && this.config.holdWhileOffline) &&
      typeof navigator !== "undefined" &&
      !navigator.onLine
    );
  }

  private setDelayed(isDelayed: boolean) {
    if (this.isDelayed === isDelayed) return;
    this.isDelayed = isDelayed;
    this.config.onResponseSendingDelayedChange?.(isDelayed);
  }

  private readonly handleOnline = () => {
    if (this.wakeBackoff) {
      this.wakeBackoff();
      return;
    }
    this.clearRecoveryTimer();
    this.processQueueInBackground("Failed to process queue after coming back online");
  };

  private clearRecoveryTimer() {
    if (this.recoveryTimer !== null) {
      clearTimeout(this.recoveryTimer);
      this.recoveryTimer = null;
    }
  }

  private listenForOnline() {
    if (globalThis.window !== undefined && !this.isListeningForOnline) {
      globalThis.window.addEventListener("online", this.handleOnline);
      this.isListeningForOnline = true;
    }
  }

  /** Keeps trying to deliver what is queued: on the browser's `online` event and on a capped backoff. */
  private startRecovery() {
    this.listenForOnline();
    if (this.recoveryTimer !== null) return;
    const wait = Math.min(RECOVERY_BASE_DELAY_MS * 2 ** this.recoveryRound, RECOVERY_MAX_DELAY_MS);
    this.recoveryRound += 1;
    this.recoveryTimer = setTimeout(() => {
      this.recoveryTimer = null;
      // A single attempt per round: the rounds themselves are the backoff.
      this.processQueueInBackground("Failed to process queue in a background retry", 0);
    }, wait);
  }

  private stopRecovery() {
    this.clearRecoveryTimer();
    this.recoveryRound = 0;
    if (this.isListeningForOnline) {
      globalThis.window.removeEventListener("online", this.handleOnline);
      this.isListeningForOnline = false;
    }
  }

  private waitBeforeRetry(ms: number): Promise<void> {
    return new Promise((resolve) => {
      const timer = setTimeout(() => this.wakeBackoff?.(), ms);
      this.wakeBackoff = () => {
        clearTimeout(timer);
        this.wakeBackoff = null;
        resolve();
      };
    });
  }

  /**
   * Returns the count of persisted pending responses without loading full data.
   * Does NOT populate the in-memory queue — syncPersistedResponses reads directly from IndexedDB.
   */
  async loadPersistedQueue(): Promise<number> {
    return this.getPendingCount();
  }

  async getPendingCount(): Promise<number> {
    if (!this.config.persistOffline || !this.config.surveyId) return 0;
    try {
      await this.retryProcessedDbIdCleanup();
      return await this.countPersistedResponses();
    } catch {
      return 0;
    }
  }

  /**
   * Drains all persisted pending responses by sending them to the server in order.
   * Reads directly from IndexedDB to avoid object-identity issues with the in-memory queue.
   *
   * responseId propagation: If a snapshot has responseId=null, the first sendResponse call
   * will create a new response and set surveyState.responseId. Subsequent entries with
   * null responseId in their snapshot will correctly use the now-set responseId because
   * we only restore from snapshot when it has a non-null value.
   */
  async syncPersistedResponses(
    onProgress?: (synced: number, total: number) => void
  ): Promise<{ success: boolean; syncedCount: number }> {
    if (!this.config.persistOffline || !this.config.surveyId) {
      return { success: true, syncedCount: 0 };
    }

    // Concurrency guard: prevent duplicate syncs from online/offline flicker
    if (this.isSyncing) return { success: false, syncedCount: 0 };

    // If processQueue already has a request in flight, don't start syncing —
    // let it finish first to avoid both paths creating the same response.
    if (this.isRequestInProgress) return { success: false, syncedCount: 0 };

    this.isSyncing = true;
    let syncedCount = 0;
    // What the sync achieved, settled once the lock is released (see settleSync).
    const outcome: SyncOutcome = { drained: false, sendFailed: false, sentFinished: false };

    try {
      await this.retryProcessedDbIdCleanup();
      const entries = await getPendingResponsesStrict(this.config.surveyId);
      const entriesToSync = entries.filter((entry) => {
        return entry.id === undefined || !this.processedDbIdsPendingRemoval.has(entry.id);
      });
      if (entriesToSync.length === 0) {
        outcome.drained = true;
        return { success: true, syncedCount: 0 };
      }

      const processedDbIds = new Set<number>();
      const detachedStates = new Map<string, SurveyState>();

      for (const entry of entriesToSync) {
        // A row of another session goes to its own response; the ending and quota of this page are not its business.
        const isDetached = this.isDetachedEntry(entry);
        const state = isDetached
          ? this.detachedStateFor(entry.surveyStateSnapshot, detachedStates)
          : this.surveyState;
        const result = await this.sendStoredEntry(entry, state);

        if (result.ok) {
          if (entry.responseUpdate.finished && !isDetached) {
            outcome.sentFinished = true;
          }
          if (this.isQuotaFullResponse(result.data) && !isDetached) {
            outcome.quotaFullResponse = result.data;
          }
          if (entry.id !== undefined) {
            const removed = await this.removePendingResponseFromIndexedDB(
              entry.id,
              "Failed to remove synced response from IndexedDB",
              { removeTrackedDbIdOnSuccess: false }
            );
            processedDbIds.add(entry.id);
            if (!removed) {
              this.removeProcessedQueueItemsByDbId(processedDbIds);
              return { success: false, syncedCount };
            }
          }
        } else {
          if (entry.id !== undefined && result.error && this.isTerminalClientError(result.error)) {
            // Permanent 4xx client error (e.g., 400 "already finished" or 409 stale) —
            // this entry can never be accepted, remove it and continue with the next one.
            const removed = await this.removePendingResponseFromIndexedDB(
              entry.id,
              "Failed to remove stale response from IndexedDB",
              { removeTrackedDbIdOnSuccess: false }
            );
            processedDbIds.add(entry.id);
            if (!removed) {
              this.removeProcessedQueueItemsByDbId(processedDbIds);
              return { success: false, syncedCount };
            }
            continue;
          }

          // Server/network error — stop syncing, remaining entries stay for next attempt
          outcome.sendFailed = true;
          this.removeProcessedQueueItemsByDbId(processedDbIds);
          return { success: false, syncedCount };
        }

        syncedCount++;
        onProgress?.(syncedCount, entriesToSync.length);
      }

      this.removeProcessedQueueItemsByDbId(processedDbIds);
      outcome.drained = true;

      return { success: true, syncedCount };
    } catch (error) {
      this.logOfflinePersistenceError("Failed to sync persisted responses from IndexedDB", error);
      return { success: false, syncedCount };
    } finally {
      this.isSyncing = false;
      this.settleSync(outcome, syncedCount);
    }
  }

  /**
   * Called when the page goes on with a response an earlier visit stored (`snapshot` is that visit's stored state): the rows stored for that response are sent as this page's answers, with this page's response. Rows of any other session stay isolated (isolateEarlierResponses).
   */
  resumeEarlierResponses(snapshot: SerializedSurveyState) {
    this.resumedSnapshot = snapshot;
  }

  /**
   * Sends, once, the answers earlier pages stored in IndexedDB for this survey, each with the ids stored with it, and removes them whether or not the server took them. For a survey that no longer saves progress: what earlier respondents left on this device must not stay on it, and their answers get one chance to arrive.
   */
  async sendEarlierResponsesOnce(): Promise<void> {
    if (!this.config.surveyId) return;

    let entries: PendingResponseEntry[];
    try {
      entries = await getPendingResponsesStrict(this.config.surveyId);
    } catch (error) {
      this.logOfflinePersistenceError("Failed to read stored responses from IndexedDB", error);
      return;
    }

    const states = new Map<string, SurveyState>();
    for (const entry of entries) {
      await this.sendStoredEntry(entry, this.detachedStateFor(entry.surveyStateSnapshot, states));
      if (entry.id !== undefined) {
        await this.removePendingResponseFromIndexedDB(
          entry.id,
          "Failed to remove stored response from IndexedDB"
        );
      }
    }
  }

  // A row stored before this queue existed belongs to another session unless it is one of the response this page resumed.
  private isDetachedEntry(entry: PendingResponseEntry): boolean {
    if (!this.config.isolateEarlierResponses || entry.createdAt >= this.createdAt) return false;
    const resumed = this.resumedSnapshot;
    if (!resumed) return true;
    const stored = entry.surveyStateSnapshot;
    return !(
      isSameRespondent(stored, resumed) &&
      isSameIfBothKnown(stored.displayId, resumed.displayId) &&
      isSameIfBothKnown(stored.responseId, resumed.responseId)
    );
  }

  // One state per stored session (respondent and display), so a session whose first row creates the response sends its later rows as updates of it.
  private detachedStateFor(snapshot: SerializedSurveyState, states: Map<string, SurveyState>): SurveyState {
    const key = JSON.stringify([
      normalizeId(snapshot.singleUseId),
      normalizeId(snapshot.contactId),
      normalizeId(snapshot.userId),
      normalizeId(snapshot.displayId),
    ]);
    let state = states.get(key);
    if (!state) {
      state = new SurveyState(
        this.surveyState.surveyId,
        snapshot.singleUseId,
        null,
        snapshot.userId,
        snapshot.contactId,
        // The PIN token proves the PIN of the survey, not of a respondent.
        this.surveyState.pinAuthToken
      );
      state.updateDisplayId(snapshot.displayId);
      states.set(key, state);
    }
    return state;
  }

  // Sends one stored row with `state`, which it updates as a send through processQueue would.
  private async sendStoredEntry(
    entry: PendingResponseEntry,
    state: SurveyState
  ): Promise<Result<boolean | TQuotaFullResponse, ApiErrorResponse>> {
    // Only restore responseId from snapshot when it was set at capture time.
    // Otherwise, let the responseId from the previous sendResponse carry forward
    // (i.e., a create response in the previous iteration sets the responseId for updates).
    if (entry.surveyStateSnapshot.responseId) {
      state.updateResponseId(entry.surveyStateSnapshot.responseId);
    }

    const result = await this.sendResponse(entry.responseUpdate, state);

    // If updateResponse returned 404, the original createResponse likely never reached
    // the server. Reset responseId and retry as a fresh createResponse.
    if (!result.ok && result.error?.status === 404 && state.responseId !== null) {
      state.responseId = null;
      if (entry.surveyStateSnapshot.displayId) {
        state.updateDisplayId(entry.surveyStateSnapshot.displayId);
      }
      return this.sendResponse(entry.responseUpdate, state);
    }

    return result;
  }

  /**
   * Gives a sync the same follow-up as a send through processQueue. processQueue hands its work to syncPersistedResponses once two answers are stored, so without this a final submission delivered by a sync never reaches the ending card, and the delayed notice of an earlier failed card never goes away. Runs after the sync lock is released, so the processQueue it starts for answers added meanwhile is not turned away by that lock.
   */
  private settleSync(outcome: SyncOutcome, syncedCount: number) {
    if (outcome.sentFinished) {
      this.config.onResponseSendingFinished?.();
    }

    if (outcome.drained) {
      if (syncedCount > 0) {
        this.setDelayed(false);
        // The connection works again: the next failure starts its backoff from the beginning.
        this.stopRecovery();
      }
    } else if (this.queue.length > 0) {
      // Answers given on this page are still waiting: keep trying, as processQueue does after a failed burst. Rows left by an earlier visit are retried by the survey when the browser comes back online.
      if (outcome.sendFailed) {
        this.setDelayed(true);
      }
      this.startRecovery();
    }

    if (outcome.quotaFullResponse) {
      this.config.onQuotaFull?.(outcome.quotaFullResponse);
    }

    // Answers added while the sync ran were not in the rows it read.
    if (outcome.drained && this.queue.length > 0) {
      this.processQueueInBackground("Failed to process queue after persisted response sync");
    }
  }

  /**
   * Sends one queued update: a first attempt, then up to `retryAttempts` retries, each after a backoff of 1s, 2s, 4s, 8s... The last backoff is followed by a last attempt rather than by giving up, so a connection that came back during the wait is not reported as down.
   */
  private async sendResponseWithRetry(
    responseUpdate: TResponseUpdate,
    retryAttempts: number
  ): Promise<{
    success: boolean;
    quotaFullResponse?: TQuotaFullResponse;
    isRecaptchaError?: boolean;
    isTerminalClientError?: boolean;
    errorCode?: TResponseErrorCodesEnum;
  }> {
    let attempts = 0;
    let quotaFullResponse: TQuotaFullResponse | null = null;

    for (;;) {
      const res = await this.sendResponse(responseUpdate);

      if (res.ok) {
        this.queue.shift(); // remove the successfully sent response from the queue
        this.setDelayed(false);

        if (this.isQuotaFullResponse(res.data)) {
          quotaFullResponse = res.data;
        }

        if (attempts > 0) {
          console.log(`Forma: Response sent successfully after ${attempts + 1} attempts`);
        }

        return { success: true, quotaFullResponse: quotaFullResponse ?? undefined };
      }

      if (this.isRecaptchaError(res.error)) {
        console.error("Forma: Recaptcha verification failed", {
          error: res.error,
          responseId: this.surveyState.responseId,
        });
        return { success: false, isRecaptchaError: true };
      }

      // Permanent 4xx rejection — don't retry (backoff or manual Retry would just loop on the
      // same rejection). Signal the caller to drop the item from the queue.
      if (this.isTerminalClientError(res.error)) {
        console.error("Forma: Response rejected permanently, dropping from queue", {
          error: res.error,
          responseId: this.surveyState.responseId,
          queueLength: this.queue.length,
        });
        return {
          success: false,
          isTerminalClientError: true,
          errorCode: this.getTerminalClientErrorCode(res.error),
        };
      }

      console.error(`Forma: Response send failed`, {
        attempt: attempts + 1,
        maxAttempts: retryAttempts + 1,
        error: res.error,
        responseId: this.surveyState.responseId,
        queueLength: this.queue.length,
      });
      this.setDelayed(true);
      this.listenForOnline();

      if (attempts >= retryAttempts) break;

      // Exponential backoff: 1s, 2s, 4s, 8s
      const backoffMs = 1000 * Math.pow(2, attempts);
      await this.waitBeforeRetry(backoffMs);
      attempts++;
    }

    console.error(`Forma: Failed to send response after ${attempts + 1} attempts`, {
      queueLength: this.queue.length,
      responseId: this.surveyState.responseId,
      surveyId: this.surveyState.surveyId,
    });

    return { success: false, isRecaptchaError: false };
  }

  private isQuotaFullResponse(data: unknown): data is TQuotaFullResponse {
    return typeof data === "object" && data !== null && "quotaFull" in data;
  }

  private isRecaptchaError(error: any): boolean {
    return error.details?.code === RECAPTCHA_VERIFICATION_ERROR_CODE;
  }

  private getCreatePayload(
    responseUpdate: TResponseUpdate,
    state: SurveyState
  ): Omit<
    Parameters<ApiClient["createResponse"]>[0],
    "contactId" | "userId" | "singleUseId" | "surveyId" | "displayId" | "recaptchaToken"
  > {
    if (!state.shouldCreateResponseFromState) {
      return {
        finished: responseUpdate.finished,
        data: { ...responseUpdate.data, ...responseUpdate.hiddenFields },
        ttc: responseUpdate.ttc,
        variables: responseUpdate.variables,
        language: responseUpdate.language,
        meta: responseUpdate.meta,
        endingId: responseUpdate.endingId,
      };
    }

    const accumulatedResponse = state.responseAcc;

    return {
      finished: accumulatedResponse.finished,
      data: { ...accumulatedResponse.data, ...responseUpdate.hiddenFields },
      ttc: accumulatedResponse.ttc,
      variables: accumulatedResponse.variables,
      language: accumulatedResponse.language ?? responseUpdate.language,
      meta: accumulatedResponse.meta ?? responseUpdate.meta,
      endingId: accumulatedResponse.endingId ?? responseUpdate.endingId,
    };
  }

  private handleSuccessfulResponse(responseUpdate: TResponseUpdate, quotaFullResponse?: TQuotaFullResponse) {
    if (responseUpdate.finished) {
      this.config.onResponseSendingFinished?.();
    }

    this.isRequestInProgress = false;
    // The connection works again: the next failure starts its backoff from the beginning.
    this.stopRecovery();

    if (quotaFullResponse) {
      this.config.onQuotaFull?.(quotaFullResponse);
    }

    this.processQueueInBackground("Failed to process next queued response");
  }

  private handleFailedResponse(
    responseUpdate: TResponseUpdate,
    isRecaptchaError?: boolean,
    errorCode?: TResponseErrorCodesEnum
  ) {
    this.isRequestInProgress = false;

    if (isRecaptchaError) {
      this.config.onResponseSendingFailed?.(responseUpdate, TResponseErrorCodesEnum.RecaptchaError);
      return;
    }

    this.config.onResponseSendingFailed?.(
      responseUpdate,
      errorCode ?? TResponseErrorCodesEnum.ResponseSendingError
    );
  }

  // `state` is this page's response unless a stored row of another session is being sent (syncPersistedResponses); only this page's creation is reported.
  async sendResponse(
    responseUpdate: TResponseUpdate,
    state: SurveyState = this.surveyState
  ): Promise<Result<boolean | TQuotaFullResponse, ApiErrorResponse>> {
    try {
      let response;
      if (state.responseId !== null) {
        response = await this.api.updateResponse({
          ...responseUpdate,
          responseId: state.responseId,
          pinAuthToken: state.pinAuthToken || null,
        });

        if (!response.ok) {
          return err(response.error);
        }
      } else {
        const createPayload = this.getCreatePayload(responseUpdate, state);
        response = await this.api.createResponse({
          ...createPayload,
          surveyId: state.surveyId,
          contactId: state.contactId || null,
          userId: state.userId || null,
          singleUseId: state.singleUseId || null,
          displayId: state.displayId,
          pinAuthToken: state.pinAuthToken || null,
          recaptchaToken: this.responseRecaptchaToken ?? undefined,
        });

        if (!response.ok) {
          return err(response.error);
        }

        state.updateResponseId(response.data.id);
        state.disableBootstrapResponseCreate();
        if (state === this.surveyState) {
          this.config.onResponseCreated?.(response.data.id);
          if (this.config.setSurveyState) {
            this.config.setSurveyState(this.surveyState);
          }
        }
      }

      // Check for quota-full response
      if (response.ok && response.data.quotaFull) {
        return ok({
          quotaFull: true,
          quotaId: response.data.quota.id,
          action: response.data.quota.action,
          endingCardId: response.data.quota.endingCardId || "",
        });
      }

      return ok(true);
    } catch (error) {
      console.error("Forma: Error sending response", error);
      return err({
        code: "internal_server_error",
        message: "An error occurred while sending the response.",
        status: 500,
      });
    }
  }

  // update surveyState
  updateSurveyState(surveyState: SurveyState) {
    this.surveyState = surveyState;
  }

  // get unsent response data from queue
  getUnsentData(): TResponseUpdate["data"] {
    return this.queue.reduce((acc, item) => ({ ...acc, ...item.data }), {});
  }
}
