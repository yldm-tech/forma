import type { TFunction } from "i18next";
import { ApiError } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";

/** server/internal/integration's CodeProviderError: a provider refused or failed the request, and the message is the provider's. */
export const PROVIDER_ERROR = "integration_provider_error";

/** The message of a failed integrations request: the provider's own words when a provider failed it, as forma2's pages showed them, otherwise as any request's. */
export const integrationErrorMessage = (t: TFunction, error: unknown): string => {
  if (error instanceof ApiError && error.code === PROVIDER_ERROR) return error.message;
  return errorMessage(t, error);
};

/** Whether a Slack failure means the connection must be made again: forma2 offered reconnecting when Slack answered missing_scope (the private-channel scopes it added later) or invalid_auth. */
export const slackNeedsReconnect = (error: unknown): boolean =>
  error instanceof ApiError &&
  error.code === PROVIDER_ERROR &&
  (error.message.includes("missing_scope") || error.message.includes("invalid_auth"));
