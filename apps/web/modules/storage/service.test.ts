import { randomUUID } from "crypto";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { StorageErrorCode } from "@forma/storage";
import { TAccessType } from "@forma/types/storage";
import {
  deleteFile,
  deleteFilesByWorkspaceId,
  getFileStreamForDownload,
  getSignedUrlForUpload,
} from "./service";

// Mock external dependencies
vi.mock("crypto", () => ({
  randomUUID: vi.fn(),
}));

vi.mock("@forma/logger", () => ({
  logger: {
    error: vi.fn(),
  },
}));

vi.mock("@forma/storage", () => ({
  StorageErrorCode: {
    Unknown: "unknown",
    S3ClientError: "s3_client_error",
    S3CredentialsError: "s3_credentials_error",
    FileNotFoundError: "file_not_found_error",
    InvalidInput: "invalid_input",
  },
  deleteFile: vi.fn(),
  deleteFilesByPrefix: vi.fn(),
  getFileStream: vi.fn(),
  getSignedDownloadUrl: vi.fn(),
  getSignedUploadUrl: vi.fn(),
}));

// Import mocked dependencies
const { logger } = await import("@forma/logger");
const storageModule = await import("@forma/storage");
const {
  deleteFile: deleteFileFromS3,
  deleteFilesByPrefix,
  getSignedUploadUrl,
  getFileStream,
} = storageModule;
type MockedSignedUploadReturn = Awaited<ReturnType<typeof getSignedUploadUrl>>;
type MockedFileStreamReturn = Awaited<ReturnType<typeof getFileStream>>;
type MockedDeleteFileReturn = Awaited<ReturnType<typeof deleteFile>>;
type MockedDeleteFilesByPrefixReturn = Awaited<ReturnType<typeof deleteFilesByPrefix>>;

const mockUUID = "test-uuid-123-456-789-10";

describe("storage service", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(randomUUID).mockReturnValue(mockUUID);
  });

  describe("getSignedUrlForUpload", () => {
    test("should generate signed URL for upload with unique filename", async () => {
      const mockSignedUrlResponse = {
        ok: true,
        data: {
          signedUrl: "https://s3.example.com/upload",
          presignedFields: { key: "value" },
        },
      } as MockedSignedUploadReturn;

      vi.mocked(getSignedUploadUrl).mockResolvedValue(mockSignedUrlResponse);

      const result = await getSignedUrlForUpload(
        "test-image.jpg",
        "env-123",
        "image/jpeg",
        "public" as TAccessType
      );

      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.data).toEqual({
          signedUrl: "https://s3.example.com/upload",
          presignedFields: { key: "value" },
          fileUrl: `/storage/env-123/public/test-image--fid--${mockUUID}.jpg`,
        });
      }

      expect(getSignedUploadUrl).toHaveBeenCalledWith(
        `test-image--fid--${mockUUID}.jpg`,
        "image/jpeg",
        "env-123/public",
        1024 * 1024 * 10 // 10MB default
      );
    });

    test("should return relative URL for private files", async () => {
      const mockSignedUrlResponse = {
        ok: true,
        data: {
          signedUrl: "https://s3.example.com/upload",
          presignedFields: { key: "value" },
        },
      } as MockedSignedUploadReturn;

      vi.mocked(getSignedUploadUrl).mockResolvedValue(mockSignedUrlResponse);

      const result = await getSignedUrlForUpload(
        "test-doc.pdf",
        "env-123",
        "application/pdf",
        "private" as TAccessType
      );

      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.data.fileUrl).toBe(`/storage/env-123/private/test-doc--fid--${mockUUID}.pdf`);
      }
    });

    test("should generate scoped private upload URL when path segments are provided", async () => {
      const mockSignedUrlResponse = {
        ok: true,
        data: {
          signedUrl: "https://s3.example.com/upload",
          presignedFields: { key: "value" },
        },
      } as MockedSignedUploadReturn;

      vi.mocked(getSignedUploadUrl).mockResolvedValue(mockSignedUrlResponse);

      const result = await getSignedUrlForUpload(
        "test-doc.pdf",
        "ws-123",
        "application/pdf",
        "private" as TAccessType,
        1024 * 1024 * 10,
        ["surveys", "survey-123", "elements", "element-123"]
      );

      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.data.fileUrl).toBe(
          `/storage/ws-123/private/surveys/survey-123/elements/element-123/test-doc--fid--${mockUUID}.pdf`
        );
      }

      expect(getSignedUploadUrl).toHaveBeenCalledWith(
        `test-doc--fid--${mockUUID}.pdf`,
        "application/pdf",
        "ws-123/private/surveys/survey-123/elements/element-123",
        1024 * 1024 * 10
      );
    });

    test.each(["", ".", "..", "bad segment", "bad/segment", "bad\\segment", "bad?segment", "bad#segment"])(
      "should reject unsafe scoped private upload path segment %s",
      async (unsafeSegment) => {
        const result = await getSignedUrlForUpload(
          "test-doc.pdf",
          "ws-123",
          "application/pdf",
          "private" as TAccessType,
          1024 * 1024 * 10,
          ["surveys", unsafeSegment]
        );

        expect(result.ok).toBe(false);
        if (!result.ok) {
          expect(result.error.code).toBe(StorageErrorCode.InvalidInput);
        }
        expect(getSignedUploadUrl).not.toHaveBeenCalled();
      }
    );

    test("should properly sanitize filenames with special characters like # in URL", async () => {
      const mockSignedUrlResponse = {
        ok: true,
        data: {
          signedUrl: "https://s3.example.com/upload",
          presignedFields: { key: "value" },
        },
      } as MockedSignedUploadReturn;

      vi.mocked(getSignedUploadUrl).mockResolvedValue(mockSignedUrlResponse);

      const result = await getSignedUrlForUpload(
        "test#file.txt",
        "env-123",
        "text/plain",
        "public" as TAccessType
      );

      expect(result.ok).toBe(true);
      if (result.ok) {
        // The filename should be URL-encoded to prevent # from being treated as a URL fragment
        expect(result.data.fileUrl).toBe(`/storage/env-123/public/testfile--fid--${mockUUID}.txt`);
      }

      expect(getSignedUploadUrl).toHaveBeenCalledWith(
        `testfile--fid--${mockUUID}.txt`,
        "text/plain",
        "env-123/public",
        1024 * 1024 * 10 // 10MB default
      );
    });

    test("should handle files with multiple dots in filename", async () => {
      const mockSignedUrlResponse = {
        ok: true,
        data: {
          signedUrl: "https://s3.example.com/upload",
          presignedFields: { key: "value" },
        },
      } as MockedSignedUploadReturn;

      vi.mocked(getSignedUploadUrl).mockResolvedValue(mockSignedUrlResponse);

      const result = await getSignedUrlForUpload(
        "my.backup.file.pdf",
        "env-123",
        "application/pdf",
        "public" as TAccessType
      );

      expect(result.ok).toBe(true);
      expect(getSignedUploadUrl).toHaveBeenCalledWith(
        `my.backup.file--fid--${mockUUID}.pdf`,
        "application/pdf",
        "env-123/public",
        1024 * 1024 * 10
      );
    });

    test("should use custom maxFileUploadSize when provided", async () => {
      const mockSignedUrlResponse = {
        ok: true,
        data: {
          signedUrl: "https://s3.example.com/upload",
          presignedFields: { key: "value" },
        },
      } as MockedSignedUploadReturn;

      vi.mocked(getSignedUploadUrl).mockResolvedValue(mockSignedUrlResponse);

      await getSignedUrlForUpload(
        "large-file.pdf",
        "env-123",
        "application/pdf",
        "public" as TAccessType,
        1024 * 1024 * 50 // 50MB
      );

      expect(getSignedUploadUrl).toHaveBeenCalledWith(
        `large-file--fid--${mockUUID}.pdf`,
        "application/pdf",
        "env-123/public",
        1024 * 1024 * 50
      );
    });

    test("should return error when getSignedUploadUrl fails", async () => {
      const mockErrorResponse = {
        ok: false,
        error: {
          code: StorageErrorCode.S3ClientError,
        },
      } as MockedSignedUploadReturn;

      vi.mocked(getSignedUploadUrl).mockResolvedValue(mockErrorResponse);

      const result = await getSignedUrlForUpload(
        "test-file.pdf",
        "env-123",
        "application/pdf",
        "public" as TAccessType
      );

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error.code).toBe(StorageErrorCode.S3ClientError);
      }
    });

    test("should handle unexpected errors and return unknown error", async () => {
      vi.mocked(getSignedUploadUrl).mockRejectedValue(new Error("Unexpected error"));

      const result = await getSignedUrlForUpload(
        "test-file.pdf",
        "env-123",
        "application/pdf",
        "public" as TAccessType
      );

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error.code).toBe(StorageErrorCode.Unknown);
      }
      expect(logger.error).toHaveBeenCalledWith(
        { error: expect.any(Error) },
        "Error getting signed url for upload"
      );
    });

    test("should return InvalidInput when sanitized filename is empty or invalid", async () => {
      const mockErrorResponse = {
        ok: false,
        error: { code: StorageErrorCode.InvalidInput },
      } as MockedSignedUploadReturn;

      vi.mocked(getSignedUploadUrl).mockResolvedValue(mockErrorResponse);

      const result = await getSignedUrlForUpload("----.png", "env-123", "image/png", "public" as TAccessType);

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error.code).toBe(StorageErrorCode.InvalidInput);
      }
    });
  });

  describe("deleteFile", () => {
    test("should call deleteFileFromS3 with correct file key", async () => {
      const mockSuccessResult = {
        ok: true,
        data: undefined,
      } as MockedDeleteFileReturn;

      vi.mocked(deleteFileFromS3).mockResolvedValue(mockSuccessResult);

      const result = await deleteFile("env-123", "public" as TAccessType, "test-file.jpg");

      expect(result).toEqual(mockSuccessResult);
      expect(deleteFileFromS3).toHaveBeenCalledWith("env-123/public/test-file.jpg");
    });

    test("should handle private access type", async () => {
      const mockSuccessResult = {
        ok: true,
        data: undefined,
      } as MockedDeleteFileReturn;

      vi.mocked(deleteFileFromS3).mockResolvedValue(mockSuccessResult);

      const result = await deleteFile("env-456", "private" as TAccessType, "private-doc.pdf");

      expect(result).toEqual(mockSuccessResult);
      expect(deleteFileFromS3).toHaveBeenCalledWith("env-456/private/private-doc.pdf");
    });

    test("should handle when deleteFileFromS3 returns error", async () => {
      const mockErrorResult = {
        ok: false,
        error: {
          code: StorageErrorCode.Unknown,
        },
      } as MockedDeleteFileReturn;

      vi.mocked(deleteFileFromS3).mockResolvedValue(mockErrorResult);

      const result = await deleteFile("env-123", "public" as TAccessType, "test-file.jpg");

      expect(result).toEqual(mockErrorResult);
      expect(deleteFileFromS3).toHaveBeenCalledWith("env-123/public/test-file.jpg");
    });

    test("should fall back to fallbackId path when primary returns FileNotFoundError", async () => {
      const mockNotFound = {
        ok: false,
        error: { code: StorageErrorCode.FileNotFoundError },
      } as MockedDeleteFileReturn;
      const mockSuccess = { ok: true, data: undefined } as MockedDeleteFileReturn;

      vi.mocked(deleteFileFromS3).mockResolvedValueOnce(mockNotFound).mockResolvedValueOnce(mockSuccess);

      const result = await deleteFile("ws-456", "public" as TAccessType, "file.jpg", "env-123");

      expect(result).toEqual(mockSuccess);
      expect(deleteFileFromS3).toHaveBeenCalledTimes(2);
      expect(deleteFileFromS3).toHaveBeenNthCalledWith(1, "ws-456/public/file.jpg");
      expect(deleteFileFromS3).toHaveBeenNthCalledWith(2, "env-123/public/file.jpg");
    });

    test("should not fall back when primary delete succeeds", async () => {
      const mockSuccess = { ok: true, data: undefined } as MockedDeleteFileReturn;

      vi.mocked(deleteFileFromS3).mockResolvedValue(mockSuccess);

      const result = await deleteFile("ws-456", "public" as TAccessType, "file.jpg", "env-123");

      expect(result).toEqual(mockSuccess);
      expect(deleteFileFromS3).toHaveBeenCalledTimes(1);
      expect(deleteFileFromS3).toHaveBeenCalledWith("ws-456/public/file.jpg");
    });

    test("should not fall back on non-FileNotFound errors", async () => {
      const mockError = {
        ok: false,
        error: { code: StorageErrorCode.S3ClientError },
      } as MockedDeleteFileReturn;

      vi.mocked(deleteFileFromS3).mockResolvedValue(mockError);

      const result = await deleteFile("ws-456", "public" as TAccessType, "file.jpg", "env-123");

      expect(result).toEqual(mockError);
      expect(deleteFileFromS3).toHaveBeenCalledTimes(1);
    });

    // Regression: the key is `${id}/${accessType}/${fileName}`, so a `..` segment would delete objects outside the workspace prefix the caller was authorized against. The route no longer decodes before calling in, so the encoded case has to be caught here.
    test.each([
      "../../ws-victim/private/secret.pdf",
      "sub/../../../ws-victim/private/secret.pdf",
      "%2e%2e/%2e%2e/ws-victim/private/secret.pdf",
      "./file.jpg",
    ])("should reject traversal in the file name: %s", async (fileName) => {
      const result = await deleteFile("ws-456", "public" as TAccessType, fileName);

      expect(result.ok).toBe(false);
      expect(deleteFileFromS3).not.toHaveBeenCalled();
    });

    // The guard decodes each segment to catch a double-encoded traversal, so it has to survive a segment that is not valid percent-encoding at all.
    test("should delete a file whose name holds a literal percent sign", async () => {
      const mockSuccess = { ok: true, data: undefined } as MockedDeleteFileReturn;
      vi.mocked(deleteFileFromS3).mockResolvedValue(mockSuccess);

      const result = await deleteFile("ws-456", "private" as TAccessType, "invoice 100%.pdf");

      expect(result).toEqual(mockSuccess);
      expect(deleteFileFromS3).toHaveBeenCalledWith("ws-456/private/invoice 100%.pdf");
    });

    test("should still allow nested file paths without dot segments", async () => {
      const mockSuccess = { ok: true, data: undefined } as MockedDeleteFileReturn;
      vi.mocked(deleteFileFromS3).mockResolvedValue(mockSuccess);

      const result = await deleteFile("ws-456", "public" as TAccessType, "survey-1/q-2/file.jpg");

      expect(result).toEqual(mockSuccess);
      expect(deleteFileFromS3).toHaveBeenCalledWith("ws-456/public/survey-1/q-2/file.jpg");
    });
  });

  describe("deleteFilesByWorkspaceId", () => {
    test("should delete files under workspaceId and all environmentId prefixes", async () => {
      const mockSuccessResult = {
        ok: true,
        data: undefined,
      } as MockedDeleteFilesByPrefixReturn;

      vi.mocked(deleteFilesByPrefix).mockResolvedValue(mockSuccessResult);

      const result = await deleteFilesByWorkspaceId("ws-456", ["env-123", "env-789"]);

      expect(result).toEqual(mockSuccessResult);
      expect(deleteFilesByPrefix).toHaveBeenCalledTimes(3);
      expect(deleteFilesByPrefix).toHaveBeenCalledWith("ws-456");
      expect(deleteFilesByPrefix).toHaveBeenCalledWith("env-123");
      expect(deleteFilesByPrefix).toHaveBeenCalledWith("env-789");
    });

    test("should return error if any prefix deletion fails", async () => {
      const mockSuccessResult = {
        ok: true,
        data: undefined,
      } as MockedDeleteFilesByPrefixReturn;

      const mockErrorResult = {
        ok: false,
        error: {
          code: StorageErrorCode.Unknown,
        },
      } as MockedDeleteFilesByPrefixReturn;

      vi.mocked(deleteFilesByPrefix)
        .mockResolvedValueOnce(mockSuccessResult)
        .mockResolvedValueOnce(mockErrorResult);

      const result = await deleteFilesByWorkspaceId("ws-456", ["env-123"]);

      expect(result!.ok).toBe(false);
    });
  });

  describe("getFileStreamForDownload", () => {
    // Regression: `fileName` comes from the `[...filePath]` route param and the key is `${id}/${accessType}/${fileName}`, so a `..` segment let a caller name an object outside their own workspace prefix — reachable with no auth at all through the `public/` access type, which skips authorizePrivateDownload. The `%2e%2e` case covers a double-encoded traversal: Next decodes `%252e%252e` once, so the guard has to decode each segment itself to see it.
    test.each([
      "../../ws-victim/private/secret.pdf",
      "sub/../../../ws-victim/private/secret.pdf",
      "%2e%2e/%2e%2e/ws-victim/private/secret.pdf",
      "./secret.pdf",
    ])("should reject traversal in the file name: %s", async (fileName) => {
      const result = await getFileStreamForDownload(fileName, "ws-attacker", "public" as TAccessType);

      expect(result.ok).toBe(false);
      expect(getFileStream).not.toHaveBeenCalled();
    });

    test("should still allow nested file paths without dot segments", async () => {
      const mockStreamResult = {
        ok: true,
        data: { body: new ReadableStream(), contentType: "image/jpeg", contentLength: 1 },
      } as MockedFileStreamReturn;
      vi.mocked(getFileStream).mockResolvedValue(mockStreamResult);

      const result = await getFileStreamForDownload(
        "survey-1/q-2/file.jpg",
        "ws-456",
        "public" as TAccessType
      );

      expect(result.ok).toBe(true);
      expect(getFileStream).toHaveBeenCalledWith("ws-456/public/survey-1/q-2/file.jpg");
    });

    test("should return file stream for public file", async () => {
      const mockStream = new ReadableStream();
      const mockStreamResult = {
        ok: true,
        data: {
          body: mockStream,
          contentType: "image/jpeg",
          contentLength: 12345,
        },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValue(mockStreamResult);

      const result = await getFileStreamForDownload("test-image.jpg", "env-123", "public" as TAccessType);

      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.data.body).toBe(mockStream);
        expect(result.data.contentType).toBe("image/jpeg");
        expect(result.data.contentLength).toBe(12345);
      }
      expect(getFileStream).toHaveBeenCalledWith("env-123/public/test-image.jpg");
    });

    test("should return file stream for private file", async () => {
      const mockStream = new ReadableStream();
      const mockStreamResult = {
        ok: true,
        data: {
          body: mockStream,
          contentType: "application/pdf",
          contentLength: 54321,
        },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValue(mockStreamResult);

      const result = await getFileStreamForDownload("document.pdf", "env-456", "private" as TAccessType);

      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.data.contentType).toBe("application/pdf");
      }
      expect(getFileStream).toHaveBeenCalledWith("env-456/private/document.pdf");
    });

    // Contract change: this used to decode `fileName` again and assert `my%20file.png` reached S3 as `my file.png`. The caller hands over the stored name — Next decodes the route param, and the export path decodes the stored URL — so a second decode corrupted every name a user could put a `%` in. The name is now used verbatim.
    test("should use the file name verbatim instead of decoding it again", async () => {
      const mockStreamResult = {
        ok: true,
        data: { body: new ReadableStream(), contentType: "image/png", contentLength: 1000 },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValue(mockStreamResult);

      const result = await getFileStreamForDownload("my file.png", "env-123", "public" as TAccessType);

      expect(result.ok).toBe(true);
      expect(getFileStream).toHaveBeenCalledWith("env-123/public/my file.png");
    });

    // Regression: `decodeURIComponent` throws `URIError` on a bare `%`, which the catch turned into an Unknown error — HTTP 500 — for a file that uploaded successfully and was then unreachable forever.
    test("should stream a file whose name holds a literal percent sign", async () => {
      const mockStreamResult = {
        ok: true,
        data: { body: new ReadableStream(), contentType: "application/pdf", contentLength: 10 },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValue(mockStreamResult);

      const result = await getFileStreamForDownload("invoice 100%.pdf", "ws-123", "private" as TAccessType);

      expect(result.ok).toBe(true);
      expect(getFileStream).toHaveBeenCalledWith("ws-123/private/invoice 100%.pdf");
    });

    // The two halves of the round trip have to agree on one key. Upload percent-encodes the stored name into `fileUrl`; Next decodes the `[...filePath]` param once before handing it back to us.
    test("should resolve the same S3 key that upload stored for a name holding a percent sign", async () => {
      vi.mocked(getSignedUploadUrl).mockResolvedValue({
        ok: true,
        data: { signedUrl: "https://s3.example.com/upload", presignedFields: {} },
      } as MockedSignedUploadReturn);
      vi.mocked(getFileStream).mockResolvedValue({
        ok: true,
        data: { body: new ReadableStream(), contentType: "application/pdf", contentLength: 10 },
      } as MockedFileStreamReturn);

      const uploadResult = await getSignedUrlForUpload(
        "invoice 100%.pdf",
        "ws-123",
        "application/pdf",
        "private" as TAccessType
      );
      expect(uploadResult.ok).toBe(true);
      if (!uploadResult.ok) return;

      const [storedName, , storedPrefix] = vi.mocked(getSignedUploadUrl).mock.calls[0];
      const uploadedKey = `${storedPrefix}/${storedName}`;

      // What Next hands the route as `params.filePath` for the URL upload returned.
      const routeFileName = uploadResult.data.fileUrl
        .replace("/storage/ws-123/private/", "")
        .split("/")
        .map(decodeURIComponent)
        .join("/");

      const downloadResult = await getFileStreamForDownload(
        routeFileName,
        "ws-123",
        "private" as TAccessType
      );

      expect(downloadResult.ok).toBe(true);
      expect(getFileStream).toHaveBeenCalledWith(uploadedKey);
    });

    test("should return error when getFileStream fails with FileNotFoundError and no fallback", async () => {
      const mockErrorResult = {
        ok: false,
        error: {
          code: StorageErrorCode.FileNotFoundError,
        },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValue(mockErrorResult);

      const result = await getFileStreamForDownload("missing-file.jpg", "env-123", "public" as TAccessType);

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error.code).toBe(StorageErrorCode.FileNotFoundError);
      }
      expect(getFileStream).toHaveBeenCalledTimes(1);
    });

    test("should fall back to fallbackId path when primary path returns FileNotFoundError", async () => {
      const mockErrorResult = {
        ok: false,
        error: { code: StorageErrorCode.FileNotFoundError },
      } as MockedFileStreamReturn;

      const mockStream = new ReadableStream();
      const mockStreamResult = {
        ok: true,
        data: { body: mockStream, contentType: "image/jpeg", contentLength: 5000 },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValueOnce(mockErrorResult).mockResolvedValueOnce(mockStreamResult);

      const result = await getFileStreamForDownload(
        "legacy-file.jpg",
        "ws-456",
        "public" as TAccessType,
        "env-123"
      );

      expect(result.ok).toBe(true);
      expect(getFileStream).toHaveBeenCalledTimes(2);
      expect(getFileStream).toHaveBeenNthCalledWith(1, "ws-456/public/legacy-file.jpg");
      expect(getFileStream).toHaveBeenNthCalledWith(2, "env-123/public/legacy-file.jpg");
    });

    test("should not fall back when primary path succeeds", async () => {
      const mockStream = new ReadableStream();
      const mockStreamResult = {
        ok: true,
        data: { body: mockStream, contentType: "image/jpeg", contentLength: 5000 },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValue(mockStreamResult);

      const result = await getFileStreamForDownload("file.jpg", "ws-456", "public" as TAccessType, "env-123");

      expect(result.ok).toBe(true);
      expect(getFileStream).toHaveBeenCalledTimes(1);
      expect(getFileStream).toHaveBeenCalledWith("ws-456/public/file.jpg");
    });

    test("should not fall back on non-FileNotFound errors", async () => {
      const mockErrorResult = {
        ok: false,
        error: { code: StorageErrorCode.S3ClientError },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValue(mockErrorResult);

      const result = await getFileStreamForDownload("file.jpg", "ws-456", "public" as TAccessType, "env-123");

      expect(result.ok).toBe(false);
      expect(getFileStream).toHaveBeenCalledTimes(1);
    });

    test("should return error when getFileStream fails with S3ClientError", async () => {
      const mockErrorResult = {
        ok: false,
        error: {
          code: StorageErrorCode.S3ClientError,
        },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValue(mockErrorResult);

      const result = await getFileStreamForDownload("some-file.jpg", "env-123", "public" as TAccessType);

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error.code).toBe(StorageErrorCode.S3ClientError);
      }
    });

    test("should handle unexpected errors and return unknown error", async () => {
      vi.mocked(getFileStream).mockRejectedValue(new Error("Unexpected S3 error"));

      const result = await getFileStreamForDownload("test-file.jpg", "env-123", "public" as TAccessType);

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error.code).toBe(StorageErrorCode.Unknown);
      }
      expect(logger.error).toHaveBeenCalledWith(
        { error: expect.any(Error) },
        "Error getting file stream for download"
      );
    });

    test("should handle filename with fid pattern", async () => {
      const mockStream = new ReadableStream();
      const mockStreamResult = {
        ok: true,
        data: {
          body: mockStream,
          contentType: "image/jpeg",
          contentLength: 5000,
        },
      } as MockedFileStreamReturn;

      vi.mocked(getFileStream).mockResolvedValue(mockStreamResult);

      const result = await getFileStreamForDownload(
        "photo--fid--abc123-def456.jpg",
        "env-123",
        "public" as TAccessType
      );

      expect(result.ok).toBe(true);
      expect(getFileStream).toHaveBeenCalledWith("env-123/public/photo--fid--abc123-def456.jpg");
    });
  });
});
