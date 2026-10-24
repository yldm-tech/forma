import { ApiError } from "../../lib/api";

/** Why the last PIN was refused, so the respondent knows whether to retype it, to wait, or that retyping will not help. */
export type TPinError = "incorrect" | "rateLimited" | "failed";

/** The code the server answers a wrong PIN with (linksurvey's errInvalidPin, a 400). */
const INVALID_PIN_CODE = "INVALID_PIN";

/**
 * Only the server's wrong-PIN answer means the PIN was wrong. A server or proxy failure, or a survey paused or closed since the gate loaded (404), says nothing about the PIN, and calling it incorrect sent a respondent with the right PIN round again until the attempt limit.
 */
export const pinErrorKind = (error: unknown): TPinError => {
  if (!(error instanceof ApiError)) return "failed";
  if (error.isRateLimited) return "rateLimited";
  return error.status === 400 && error.code === INVALID_PIN_CODE ? "incorrect" : "failed";
};
