import { Result } from "@forma/types/error-handlers";
import { truncateText } from "@/lib/utils/strings";

/**
 * Pure derivation of an integration's delivery health from the `Integration` row's
 * `lastErrorAt` / `lastErrorMessage` / `consecutiveFailures` columns. No database access lives here on
 * purpose: `record-delivery-result.ts` is the server-only half that performs the write, and a client
 * component rendering the status row only needs what is in this file.
 */

/** Provider error text is stored verbatim minus credentials, so cap it rather than let a provider dump a whole response body into the row. */
export const INTEGRATION_ERROR_MESSAGE_LIMIT = 500;

/** Failures in a row at which the manage page stops describing the failure and starts prompting for a reconnect. Below it the run is more likely a transient provider error than a dead grant. */
export const INTEGRATION_RECONNECT_FAILURE_THRESHOLD = 3;

const REDACTED = "[redacted]";

// Longest names first so an alternation match cannot stop at the `token` inside `access_token`.
const CREDENTIAL_FIELD_NAMES = [
  "refresh_token",
  "access_token",
  "client_secret",
  "refreshToken",
  "accessToken",
  "clientSecret",
  "authorization",
  "api_key",
  "apiKey",
  "password",
  "secret",
  "token",
];

const CREDENTIAL_ALTERNATION = CREDENTIAL_FIELD_NAMES.join("|");

/** `"access_token": "abc"` — how a provider's JSON error body echoes back what was sent. */
const CREDENTIAL_JSON_PATTERN = new RegExp(`("(?:${CREDENTIAL_ALTERNATION})"\\s*:\\s*")[^"]*"`, "gi");

/** `?access_token=abc` — how a request URL quoted in an error message carries one. */
const CREDENTIAL_QUERY_PATTERN = new RegExp(`\\b((?:${CREDENTIAL_ALTERNATION})=)[^&\\s"']+`, "gi");

/** An `Authorization` header value quoted into the message. */
const CREDENTIAL_SCHEME_PATTERN = /\b(Bearer|Basic)\s+[\w.~+/=-]+/gi;

const redactCredentials = (message: string): string =>
  message
    .replace(CREDENTIAL_JSON_PATTERN, `$1${REDACTED}"`)
    .replace(CREDENTIAL_QUERY_PATTERN, `$1${REDACTED}`)
    .replace(CREDENTIAL_SCHEME_PATTERN, `$1 ${REDACTED}`);

/**
 * Normalise whatever the destination threw into one line of storable text.
 *
 * The result is untrusted provider output rather than our own copy: it is redacted here and must be
 * rendered as text, never as markup, wherever it is surfaced.
 */
export const toIntegrationErrorMessage = (error: unknown): string => {
  let raw: string;
  if (error instanceof Error) {
    raw = error.message;
  } else if (typeof error === "string") {
    raw = error;
  } else {
    raw = "";
  }

  const collapsed = redactCredentials(raw).replace(/\p{C}/gu, " ").replace(/\s+/g, " ").trim();

  if (collapsed === "") return "Unknown integration delivery error";
  return truncateText(collapsed, INTEGRATION_ERROR_MESSAGE_LIMIT);
};

export type TIntegrationDeliveryOutcome = { ok: true } | { ok: false; error: unknown };

/** The shape `handleIntegrations` already holds after `Promise.allSettled` over the per-destination `Result`s: a rejection and an `ok: false` are the same failure as far as delivery health is concerned. */
export const toIntegrationDeliveryOutcome = (
  settled: PromiseSettledResult<Result<void, Error>>
): TIntegrationDeliveryOutcome => {
  if (settled.status === "rejected") return { ok: false, error: settled.reason };
  if (!settled.value.ok) return { ok: false, error: settled.value.error };
  return { ok: true };
};

/**
 * What a delivery outcome implies for the row.
 *
 * `none` is the steady state and the reason there is no `lastSyncAt` column: a healthy delivery must
 * leave the row alone, or every response in a workspace would contend on this one row.
 */
export type TIntegrationHealthTransition =
  { kind: "none" } | { kind: "failure"; lastErrorAt: Date; lastErrorMessage: string } | { kind: "recovery" };

export const resolveIntegrationHealthTransition = (
  consecutiveFailures: number | null | undefined,
  outcome: TIntegrationDeliveryOutcome,
  now: Date
): TIntegrationHealthTransition => {
  if (!outcome.ok) {
    return { kind: "failure", lastErrorAt: now, lastErrorMessage: toIntegrationErrorMessage(outcome.error) };
  }

  // `undefined` means the caller did not load the column, so we cannot prove the row is already clean and
  // have to let the write path ask the database. A known 0 is the only case that writes nothing at all.
  if (consecutiveFailures === 0) return { kind: "none" };

  return { kind: "recovery" };
};

/**
 * The states the manage page renders.
 *
 * `noKnownFailure` is deliberately not called "healthy" or "synced": there is no success timestamp on the
 * row, so the only thing it asserts is that no failure has been recorded since the last success — never
 * that a delivery has ever happened. Do not render it as "last synced".
 */
export type TIntegrationHealthState = "noKnownFailure" | "failing" | "reconnectSuggested";

export const resolveIntegrationHealthState = (health: {
  lastErrorAt: Date | null;
  consecutiveFailures: number | null;
}): TIntegrationHealthState => {
  const failures = health.consecutiveFailures ?? 0;
  if (failures >= INTEGRATION_RECONNECT_FAILURE_THRESHOLD) return "reconnectSuggested";
  // A row written before the counter existed can carry an error timestamp with a zero counter; that is
  // still a failure the page should show rather than hide behind a counter default.
  if (failures > 0 || health.lastErrorAt !== null) return "failing";
  return "noKnownFailure";
};
