import type { TFunction } from "i18next";
import { ApiError } from "./api";

/**
 * The message to show for a failed request, following forma2's getClientErrorData: a rate limit gets its own wording, an expected error the server's message, and anything else a generic retry prompt, so internals never reach the screen.
 */
export const errorMessage = (t: TFunction, error: unknown): string => {
  if (error instanceof ApiError) {
    if (error.isRateLimited) return t("common.error_rate_limit_description");
    if (error.status < 500 && error.code !== "http_error") return error.message;
  }
  return t("common.something_went_wrong_please_try_again");
};
