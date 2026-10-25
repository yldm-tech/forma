import { Result, err, ok } from "@forma/types/error-handlers";
import { ApiErrorResponse, RESPONSE_ALREADY_FINISHED_ERROR_CODE } from "@forma/types/errors";
import { TQuotaFullResponse } from "@forma/types/quota";
import { TResponseUpdate } from "@forma/types/responses";
import { RECAPTCHA_VERIFICATION_ERROR_CODE } from "@/lib/constants";
import { TResponseErrorCodesEnum } from "@/types/response-error-codes";
import { ApiClient } from "./api-client";
import {
  type SerializedSurveyState,
  addPendingResponse,
  countPendingResponsesStrict,
  getPendingResponsesStrict,
  removePendingResponseStrict,
} from "./offline-storage";
import { SurveyState } from "./survey-state";

interface QueueConfig {
  appUrl: string;
  workspaceId: string;
  retryAttempts: number;
  persistOffline?: boolean;
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

    // If offline and persistence is enabled, don't attempt to send — data is safe in IndexedDB.
    if (this.config.persistOffline && typeof navigator !== "undefined" && !navigator.onLine) {
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
        return { success: false };
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
      // If offline persistence is enabled and we're now offline, don't treat it as a failure
      if (this.config.persistOffline && typeof navigator !== "undefined" && !navigator.onLine) {
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

    try {
      await this.retryProcessedDbIdCleanup();
      const entries = await getPendingResponsesStrict(this.config.surveyId);
      const entriesToSync = entries.filter((entry) => {
        return entry.id === undefined || !this.processedDbIdsPendingRemoval.has(entry.id);
      });
      if (entriesToSync.length === 0) return { success: true, syncedCount: 0 };

      const processedDbIds = new Set<number>();

      for (const entry of entriesToSync) {
        // Only restore responseId from snapshot when it was set at capture time.
        // Otherwise, let the responseId from the previous sendResponse carry forward
        // (i.e., a create response in the previous iteration sets the responseId for updates).
        if (entry.surveyStateSnapshot.responseId) {
          this.surveyState.updateResponseId(entry.surveyStateSnapshot.responseId);
        }

        let result = await this.sendResponse(entry.responseUpdate);

        // If updateResponse returned 404, the original createResponse likely never reached
        // the server. Reset responseId and retry as a fresh createResponse.
        if (!result.ok && result.error?.status === 404 && this.surveyState.responseId !== null) {
          this.surveyState.responseId = null;
          if (entry.surveyStateSnapshot.displayId) {
            this.surveyState.updateDisplayId(entry.surveyStateSnapshot.displayId);
          }
          result = await this.sendResponse(entry.responseUpdate);
        }

        if (result.ok) {
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
          this.removeProcessedQueueItemsByDbId(processedDbIds);
          return { success: false, syncedCount };
        }

        syncedCount++;
        onProgress?.(syncedCount, entriesToSync.length);
      }

      this.removeProcessedQueueItemsByDbId(processedDbIds);

      // Kick off processQueue for any entries added during sync
      if (this.queue.length > 0) {
        this.processQueueInBackground("Failed to process queue after persisted response sync");
      }

      return { success: true, syncedCount };
    } catch (error) {
      this.logOfflinePersistenceError("Failed to sync persisted responses from IndexedDB", error);
      return { success: false, syncedCount };
    } finally {
      this.isSyncing = false;
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
    responseUpdate: TResponseUpdate
  ): Omit<
    Parameters<ApiClient["createResponse"]>[0],
    "contactId" | "userId" | "singleUseId" | "surveyId" | "displayId" | "recaptchaToken"
  > {
    if (!this.surveyState.shouldCreateResponseFromState) {
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

    const accumulatedResponse = this.surveyState.responseAcc;

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

  async sendResponse(
    responseUpdate: TResponseUpdate
  ): Promise<Result<boolean | TQuotaFullResponse, ApiErrorResponse>> {
    try {
      let response;
      if (this.surveyState.responseId !== null) {
        response = await this.api.updateResponse({
          ...responseUpdate,
          responseId: this.surveyState.responseId,
          pinAuthToken: this.surveyState.pinAuthToken || null,
        });

        if (!response.ok) {
          return err(response.error);
        }
      } else {
        const createPayload = this.getCreatePayload(responseUpdate);
        response = await this.api.createResponse({
          ...createPayload,
          surveyId: this.surveyState.surveyId,
          contactId: this.surveyState.contactId || null,
          userId: this.surveyState.userId || null,
          singleUseId: this.surveyState.singleUseId || null,
          displayId: this.surveyState.displayId,
          pinAuthToken: this.surveyState.pinAuthToken || null,
          recaptchaToken: this.responseRecaptchaToken ?? undefined,
        });

        if (!response.ok) {
          return err(response.error);
        }

        this.surveyState.updateResponseId(response.data.id);
        this.surveyState.disableBootstrapResponseCreate();
        this.config.onResponseCreated?.(response.data.id);
        if (this.config.setSurveyState) {
          this.config.setSurveyState(this.surveyState);
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
