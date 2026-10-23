import { ApiError } from "../../../lib/api";
import { getAIUnavailableMessageForErrorCode } from "../../ai/availability";

type Translate = (key: string) => string;

/**
 * forma2's getAiErrorMessage: the sentence for an AI error code, shared by the two ways a generation fails — a refusal before the stream opened and an error event after it did.
 */
export function getAiErrorMessage(code: string | undefined, t: Translate): string {
  const unavailable = getAIUnavailableMessageForErrorCode(code, t);
  if (unavailable) return unavailable;
  switch (code) {
    case "ai_generated_payload_invalid":
      return t("workspace.surveys.ai_create.generated_payload_invalid");
    case "ai_output_too_long":
      return t("workspace.surveys.ai_create.ai_output_too_long");
    case "ai_quota_exceeded":
      return t("workspace.surveys.ai_create.ai_rate_limited");
    case "ai_generation_failed":
      return t("workspace.surveys.ai_create.generation_failed");
    case "ai_nothing_generated":
      return t("workspace.surveys.ai_create.nothing_generated");
    // Raised by the route's guards rather than by generation: the shared rate limit, and a body the route refused.
    case "too_many_requests":
      return t("workspace.surveys.ai_create.too_many_requests");
    case "bad_request":
      return t("workspace.surveys.ai_create.request_rejected");
    default:
      return t("common.something_went_wrong_please_try_again");
  }
}

/** The code a thrown error carries, never empty, so an unrecognised failure such as a dropped connection still renders the generic message. */
export function getAiErrorCode(error: unknown): string {
  return error instanceof ApiError ? error.code : "ai_unknown";
}
