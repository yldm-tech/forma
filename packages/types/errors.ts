export const FILE_UPLOAD_ERROR_NAMES = {
  INVALID_FILE_NAME: "InvalidFileNameError",
  STORAGE_NOT_CONFIGURED: "StorageNotConfiguredError",
  STORAGE_UPLOAD_FAILED: "StorageUploadFailedError",
  FILE_TOO_LARGE: "FileTooLargeError",
} as const;

/**
 * Stable, locale-independent marker placed in an ApiErrorResponse's `details.code` when a
 * response update is rejected because the response was already finalized. Clients key off this
 * instead of the human-readable (and potentially localized/reworded) error message.
 */
export const RESPONSE_ALREADY_FINISHED_ERROR_CODE = "response_already_finished";

export interface ApiErrorResponse {
  code:
    | "not_found"
    | "gone"
    | "bad_request"
    | "internal_server_error"
    | "unauthorized"
    | "method_not_allowed"
    | "not_authenticated"
    | "forbidden"
    | "network_error"
    | "too_many_requests";
  message: string;
  status: number;
  url?: URL;
  details?: Record<string, string | string[] | number | number[] | boolean | boolean[]>;
  responseMessage?: string;
}
