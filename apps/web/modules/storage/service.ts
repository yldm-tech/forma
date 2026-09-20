import { randomUUID } from "crypto";
import { logger } from "@forma/logger";
import {
  type FileStreamResult,
  type StorageError,
  StorageErrorCode,
  deleteFile as deleteFileFromS3,
  deleteFilesByPrefix,
  getFileStream,
  getSignedUploadUrl,
} from "@forma/storage";
import { Result, err, ok } from "@forma/types/error-handlers";
import { type TAccessType } from "@forma/types/storage";
import { sanitizeFileName } from "./utils";

const SAFE_FILE_PATH_SEGMENT = /^[A-Za-z0-9_-]+$/;

/**
 * Rejects relative path segments in a caller-supplied storage file name.
 *
 * Download/delete keys are built as `${id}/${accessType}/${fileName}`, and `fileName` arrives from the
 * `[...filePath]` route param, so `..` segments let a caller step out of their own
 * `workspaceId/accessType/` prefix and name another tenant's object — including a `private/` one via
 * the unauthenticated `public/` route. Object stores treat keys opaquely and SigV4 signs the
 * unnormalized path, so this does not resolve on a stock S3/MinIO backend; but whether one tenant can
 * name another's file must not depend on how the storage backend or any proxy in front of it happens
 * to treat dot segments. `fileName` legitimately contains `/` (upload nests it under
 * `filePathSegments`), so only the segments themselves are constrained.
 *
 * Each segment is checked raw and decoded: the route param reaches us decoded once already, so a `%252e%252e` traversal is a literal `%2e%2e` here and only a second decode reveals it. The decode is for the guard alone — the key is built from the name as given — and a segment whose encoding is malformed (`100%`, which `decodeURIComponent` throws on) is simply not a double-encoded dot segment.
 */
const hasTraversalSegment = (fileName: string): boolean =>
  fileName.split("/").some((segment) => {
    if (segment === "." || segment === "..") return true;

    try {
      const decoded = decodeURIComponent(segment);
      return decoded === "." || decoded === "..";
    } catch {
      return false;
    }
  });

export const getSignedUrlForUpload = async (
  fileName: string,
  workspaceId: string,
  fileType: string,
  accessType: TAccessType,
  maxFileUploadSize: number = 1024 * 1024 * 10, // 10MB
  filePathSegments: string[] = []
): Promise<
  Result<
    {
      signedUrl: string;
      presignedFields: Record<string, string>;
      fileUrl: string;
    },
    StorageError
  >
> => {
  try {
    const safeFileName = sanitizeFileName(fileName);
    if (!safeFileName) {
      return err({ code: StorageErrorCode.InvalidInput });
    }

    if (filePathSegments.some((segment) => !SAFE_FILE_PATH_SEGMENT.test(segment))) {
      return err({ code: StorageErrorCode.InvalidInput });
    }

    const encodedFilePathSegments = filePathSegments.map((segment) => encodeURIComponent(segment));
    const fileNameWithoutExtension = safeFileName.split(".").slice(0, -1).join(".");
    const fileExtension = safeFileName.split(".").pop();

    const updatedFileName = `${fileNameWithoutExtension}--fid--${randomUUID()}.${fileExtension}`;
    const filePath = [workspaceId, accessType, ...filePathSegments].join("/");

    const signedUrlResult = await getSignedUploadUrl(updatedFileName, fileType, filePath, maxFileUploadSize);

    if (!signedUrlResult.ok) {
      return signedUrlResult;
    }

    // Return relative path - can be resolved to absolute URL at runtime when needed
    return ok({
      signedUrl: signedUrlResult.data.signedUrl,
      presignedFields: signedUrlResult.data.presignedFields,
      fileUrl: `/storage/${workspaceId}/${accessType}/${[
        ...encodedFilePathSegments,
        encodeURIComponent(updatedFileName),
      ].join("/")}`,
    });
  } catch (error) {
    logger.error({ error }, "Error getting signed url for upload");

    return err({
      code: StorageErrorCode.Unknown,
    });
  }
};

/**
 * Get a file stream for downloading/streaming files directly.
 * Use this instead of signed URL redirect for Next.js Image component compatibility.
 *
 * Tries the primary ID path first. If the file is not found and a fallbackId is provided,
 * retries with the fallback path. This supports backwards compatibility: new uploads use
 * workspaceId paths while old files may still live under environmentId paths.
 */
export const getFileStreamForDownload = async (
  fileName: string,
  primaryId: string,
  accessType: TAccessType,
  fallbackId?: string
): Promise<Result<FileStreamResult, StorageError>> => {
  try {
    if (hasTraversalSegment(fileName)) {
      return err({ code: StorageErrorCode.InvalidInput });
    }

    // `fileName` is the stored name, not an encoded one: the `[...filePath]` route param arrives decoded from Next, and the export path decodes the stored URL before calling in. Decoding again here threw `URIError` on any name holding a literal `%` (`invoice 100%.pdf`, which `sanitizeFileName` keeps), turning an uploaded file into a permanent 500.
    const primaryKey = `${primaryId}/${accessType}/${fileName}`;

    const streamResult = await getFileStream(primaryKey);

    if (!streamResult.ok && streamResult.error.code === StorageErrorCode.FileNotFoundError && fallbackId) {
      const fallbackKey = `${fallbackId}/${accessType}/${fileName}`;
      return await getFileStream(fallbackKey);
    }

    return streamResult;
  } catch (error) {
    logger.error({ error }, "Error getting file stream for download");

    return err({
      code: StorageErrorCode.Unknown,
    });
  }
};

// Deletes a file from S3. Tries the primary ID path first; if the file is not found and a
// fallbackId is provided, retries with the fallback path (backwards compat for old environmentId paths).
export const deleteFile = async (
  primaryId: string,
  accessType: TAccessType,
  fileName: string,
  fallbackId?: string
) => {
  // Same reasoning as the download path: a `..` segment would let an authorized caller delete objects
  // outside the workspace prefix they were authorized against.
  if (hasTraversalSegment(fileName)) {
    return err({ code: StorageErrorCode.InvalidInput });
  }

  const result = await deleteFileFromS3(`${primaryId}/${accessType}/${fileName}`);

  if (!result.ok && result.error.code === StorageErrorCode.FileNotFoundError && fallbackId) {
    return await deleteFileFromS3(`${fallbackId}/${accessType}/${fileName}`);
  }

  return result;
};

// Deletes all files for a workspace — cleans up both workspaceId-prefixed (new uploads) and
// environmentId-prefixed (legacy uploads) paths. Errors are not thrown; callers should check results.
export const deleteFilesByWorkspaceId = async (workspaceId: string, environmentIds: string[]) => {
  const results = await Promise.all([
    deleteFilesByPrefix(workspaceId),
    ...environmentIds.map((envId) => deleteFilesByPrefix(envId)),
  ]);

  // Return the first error if any, otherwise success
  for (const result of results) {
    if (!result.ok) {
      return result;
    }
  }

  return results[0];
};
