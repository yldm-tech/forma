// forma2's handleFileUpload (modules/storage/file-upload.ts) and its error toasts: a public file is signed by POST /api/v1/management/storage with the session, then posted straight to the bucket with the presigned form fields, and kept by its relative /storage URL.
import { STORAGE_CONFIGURATION_ERROR_CODES } from "@forma/types/storage";
import type { TFunction } from "i18next";
import { showStorageNotConfiguredToast } from "../components/ui/storage-not-configured-toast";
import { ApiError, api } from "./api";
import { toast } from "./toast";

export const FileUploadError = {
  NO_FILE: "no_file",
  INVALID_FILE_TYPE: "invalid_file_type",
  FILE_SIZE_EXCEEDED: "file_size_exceeded",
  UPLOAD_FAILED: "upload_failed",
  INVALID_FILE_NAME: "invalid_file_name",
  STORAGE_NOT_CONFIGURED: "storage_not_configured",
  STORAGE_UPLOAD_FAILED: "storage_upload_failed",
} as const;

export type FileUploadError = (typeof FileUploadError)[keyof typeof FileUploadError];

/** forma2's MAX_FILE_SIZE_MB for an admin upload; the server signs public uploads for no more. */
export const MAX_FILE_SIZE_MB = 5;

export const STORAGE_PATH = "/api/v1/management/storage";

interface SignedUpload {
  signedUrl: string;
  /** The form fields of the presigned POST, in the order the bucket expects them before the file. */
  presignedFields: Record<string, string>;
  fileUrl: string;
}

export interface FileUploadResult {
  error?: FileUploadError;
  url: string;
}

/** forma2's getFileUploadErrorFromResponse: a rejected file name, a bucket the server cannot reach with its configuration, or any other failure. */
const signingError = (error: unknown): FileUploadError => {
  if (!(error instanceof ApiError)) return FileUploadError.UPLOAD_FAILED;
  if (error.status === 400 && error.details?.fileName) return FileUploadError.INVALID_FILE_NAME;
  const code = error.details?.storage_error_code;
  if (error.status >= 500 && typeof code === "string" && STORAGE_CONFIGURATION_ERROR_CODES.has(code)) {
    return FileUploadError.STORAGE_NOT_CONFIGURED;
  }
  return FileUploadError.UPLOAD_FAILED;
};

/** Uploads one file to the workspace's public storage and returns its relative URL, or the reason it failed. It never throws. */
export const handleFileUpload = async (
  file: File,
  workspaceId: string,
  allowedFileExtensions?: string[]
): Promise<FileUploadResult> => {
  if (!(file instanceof File)) return { error: FileUploadError.NO_FILE, url: "" };
  // `file.size` is metadata: the size check never reads the file.
  if (file.size / 1024 > MAX_FILE_SIZE_MB * 1024)
    return { error: FileUploadError.FILE_SIZE_EXCEEDED, url: "" };

  let signed: SignedUpload;
  try {
    const res = await api.post<{ data: SignedUpload }>(STORAGE_PATH, {
      fileName: file.name,
      fileType: file.type,
      allowedFileExtensions,
      workspaceId,
    });
    signed = res.data;
  } catch (error) {
    return { error: signingError(error), url: "" };
  }

  const form = new FormData();
  for (const [key, value] of Object.entries(signed.presignedFields)) form.append(key, value);
  // The file goes last and as-is, so fetch streams it; the bucket takes the key and content type from the signed fields.
  form.append("file", file);

  let uploaded: Response;
  try {
    uploaded = await fetch(signed.signedUrl, { method: "POST", body: form });
  } catch {
    return { error: FileUploadError.STORAGE_UPLOAD_FAILED, url: "" };
  }
  if (!uploaded.ok) return { error: FileUploadError.STORAGE_UPLOAD_FAILED, url: "" };
  return { url: signed.fileUrl };
};

export const getFileUploadErrorMessage = (error: FileUploadError, t: TFunction): string => {
  switch (error) {
    case FileUploadError.NO_FILE:
      return t("common.no_files_uploaded");
    case FileUploadError.INVALID_FILE_TYPE:
      return t("common.invalid_file_type");
    case FileUploadError.FILE_SIZE_EXCEEDED:
      return t("common.file_size_must_be_less_than_5_mb");
    case FileUploadError.INVALID_FILE_NAME:
      return t("common.invalid_file_name");
    case FileUploadError.STORAGE_NOT_CONFIGURED:
      return t("common.storage_not_configured");
    case FileUploadError.STORAGE_UPLOAD_FAILED:
      return t("common.file_upload_service_unavailable");
    default:
      return t("common.upload_failed");
  }
};

/** forma2's showFileUploadErrorToast: storage failures get the storage toast, everything else its message. */
export const showFileUploadErrorToast = (error: FileUploadError, t: TFunction): void => {
  if (error === FileUploadError.STORAGE_NOT_CONFIGURED) {
    showStorageNotConfiguredToast(t, "notConfigured");
    return;
  }
  if (error === FileUploadError.STORAGE_UPLOAD_FAILED) {
    showStorageNotConfiguredToast(t, "uploadUnavailable");
    return;
  }
  toast.error(getFileUploadErrorMessage(error, t));
};
