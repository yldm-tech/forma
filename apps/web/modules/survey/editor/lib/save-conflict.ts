import { OperationNotAllowedError } from "@forma/types/errors";

/**
 * Optimistic-concurrency precondition for the editor's survey saves (r238).
 *
 * The editor sends a whole `TSurvey` on every save, so a client working from a stale copy replaces every block, logic rule, ending and translation written by whoever saved in the meantime. Nothing detected that: the write was unconditional, the loser was told nothing, and there was no log line or recovery path.
 *
 * The precondition is the `updatedAt` the client loaded. It already travels both ways — `ZSurvey` requires it and `updateSurveyInternal` overwrites the stored column with `new Date()` — so the payload field is free to reuse as the expected version, and nothing the client sends in it is ever persisted.
 *
 * The check is skipped when no version is supplied, so a caller that legitimately writes without having loaded the survey first keeps working unconditionally.
 *
 * Applied on the draft path only for now (`updateSurveyDraftAction`, whose one caller is the editor's menu bar). `updateSurveyAction` has three callers outside the editor that re-submit the survey they rendered with rather than the one the action returned, so a precondition there would reject their second save; see the note at that action.
 */

/**
 * Stable, locale-independent marker returned as the update actions' `serverError` when a save is refused because the survey moved on. Clients key off this rather than the human-readable text, exactly like the `*_ERROR_CODE` sentinels in `@forma/types/errors`. The suffix carries the version the client has to adopt before it can deliberately overwrite, so recovering from a conflict needs no extra round trip.
 */
export const SURVEY_MODIFIED_ELSEWHERE_ERROR_CODE = "survey_modified_elsewhere";

/**
 * Normalize an expected-version value off the wire. `ZSurvey` parses `updatedAt` as a `Date`, but `ZSurveyDraft` is a `looseObject` that does not declare the field at all, so on the draft path it arrives as whatever the client sent and is typed `unknown` here.
 */
export function toSurveyVersion(value: unknown): Date | null {
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? null : value;
  }

  if (typeof value === "string" || typeof value === "number") {
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? null : parsed;
  }

  return null;
}

/**
 * Refuse a write built on a version the survey has already moved past.
 *
 * Only a stored version strictly newer than the expected one is a conflict. A client that somehow reports a version at or ahead of the stored row is not working from stale data, so it is let through rather than locked out by a clock or serialization quirk.
 */
export function assertSurveyNotModifiedElsewhere(expectedVersion: unknown, storedVersion: Date): void {
  const expected = toSurveyVersion(expectedVersion);

  if (!expected || storedVersion.getTime() <= expected.getTime()) {
    return;
  }

  throw new OperationNotAllowedError(
    `${SURVEY_MODIFIED_ELSEWHERE_ERROR_CODE}:${storedVersion.toISOString()}`
  );
}

/**
 * The stored version carried by a conflict `serverError`, or `null` when the error is anything else. Adopting it is what lets the next explicit save overwrite; an autosave must not use it, or the conflict resolves silently in the loser's favour.
 */
export function parseSurveyModifiedElsewhereError(serverError: string | null | undefined): Date | null {
  if (!serverError?.startsWith(`${SURVEY_MODIFIED_ELSEWHERE_ERROR_CODE}:`)) {
    return null;
  }

  return toSurveyVersion(serverError.slice(SURVEY_MODIFIED_ELSEWHERE_ERROR_CODE.length + 1));
}
