/**
 * The error codes the storage API answers with when the file storage itself is misconfigured. Kept free of zod so the survey runtime, which reports them to a respondent uploading a file, can import them without the schema library; `storage.ts` re-exports them.
 */

export const STORAGE_ERROR_CODES = {
  S3_CREDENTIALS_ERROR: "s3_credentials_error",
  S3_CLIENT_ERROR: "s3_client_error",
} as const;

export const STORAGE_CONFIGURATION_ERROR_CODES = new Set<string>([
  STORAGE_ERROR_CODES.S3_CREDENTIALS_ERROR,
  STORAGE_ERROR_CODES.S3_CLIENT_ERROR,
]);

export type TStorageConfigurationErrorCode = (typeof STORAGE_ERROR_CODES)[keyof typeof STORAGE_ERROR_CODES];

export interface TStorageApiErrorDetails {
  fileName?: string;
  storage_error_code?: string;
}
