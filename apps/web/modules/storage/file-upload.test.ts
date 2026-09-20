import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { STORAGE_ERROR_CODES } from "@forma/types/storage";
import * as fileUploadModule from "./file-upload";

// Mock global fetch
const mockFetch = vi.fn();
global.fetch = mockFetch;

const mockAtoB = vi.fn();
global.atob = mockAtoB;

// Mock FileReader
const mockFileReader = {
  readAsDataURL: vi.fn(),
  result: "data:image/jpeg;base64,test",
  onload: null as any,
  onerror: null as any,
};

// Mock File object
const createMockFile = (name: string, type: string, size: number) => {
  const file = new File([], name, { type });
  Object.defineProperty(file, "size", {
    value: size,
    writable: false,
  });
  return file;
};

describe("fileUpload", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Mock FileReader
    global.FileReader = vi.fn(function FileReader() {
      return mockFileReader;
    }) as any;
    global.atob = (base64) => Buffer.from(base64, "base64").toString("binary");
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  test("should return error when no file is provided", async () => {
    const result = await fileUploadModule.handleFileUpload(null as any, "test-env");
    expect(result.error).toBe(fileUploadModule.FileUploadError.NO_FILE);
    expect(result.url).toBe("");
  });

  // Contract change: the size check used to read `await file.arrayBuffer()` and measure its `byteLength`, so this test asserted an 11 MB buffer behind a 1 KB `file.size` was rejected. `file.size` is that same byte count as metadata, so the check now reads no bytes at all — and the whole file is never pulled into memory on the main thread.
  test("should return FILE_SIZE_EXCEEDED from file.size without reading the file", async () => {
    const file = createMockFile("test.jpg", "image/jpeg", 11 * 1024 * 1024);
    const arrayBufferSpy = vi.spyOn(file, "arrayBuffer");

    const result = await fileUploadModule.handleFileUpload(file, "env-oversize-buffer");

    expect(result.error).toBe(fileUploadModule.FileUploadError.FILE_SIZE_EXCEEDED);
    expect(result.url).toBe("");
    expect(arrayBufferSpy).not.toHaveBeenCalled();
    expect(mockFetch).not.toHaveBeenCalled();
  });

  test("should handle API error when getting signed URL", async () => {
    const file = createMockFile("test.jpg", "image/jpeg", 1000);

    // Mock failed API response
    mockFetch.mockResolvedValueOnce({
      ok: false,
    });

    const result = await fileUploadModule.handleFileUpload(file, "test-env");
    expect(result.error).toBe(fileUploadModule.FileUploadError.UPLOAD_FAILED);
    expect(result.url).toBe("");
  });

  test("should return STORAGE_NOT_CONFIGURED when signing API returns a storage configuration error", async () => {
    const file = createMockFile("test.jpg", "image/jpeg", 1000);

    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 500,
      json: async () => ({
        code: "internal_server_error",
        message: "File storage is not configured correctly. Please check your file upload settings.",
        details: { storage_error_code: STORAGE_ERROR_CODES.S3_CREDENTIALS_ERROR },
      }),
    });

    const result = await fileUploadModule.handleFileUpload(file, "test-env");

    expect(result.error).toBe(fileUploadModule.FileUploadError.STORAGE_NOT_CONFIGURED);
    expect(result.url).toBe("");
  });

  test("should return INVALID_FILE_NAME when signing API rejects the file name", async () => {
    const file = createMockFile("----.jpg", "image/jpeg", 1000);

    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 400,
      json: async () => ({ details: { fileName: "Invalid file name" } }),
    });

    const result = await fileUploadModule.handleFileUpload(file, "test-env");

    expect(result.error).toBe(fileUploadModule.FileUploadError.INVALID_FILE_NAME);
    expect(result.url).toBe("");
  });

  test("should handle successful file upload with presigned fields", async () => {
    const file = createMockFile("test.jpg", "image/jpeg", 1000);

    // Mock successful API response - now returns relative path
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        data: {
          signedUrl: "https://s3.example.com/upload",
          fileUrl: "/storage/test-env/public/file.jpg",
          presignedFields: {
            key: "value",
          },
        },
      }),
    });

    // Mock successful upload response
    mockFetch.mockResolvedValueOnce({
      ok: true,
    });

    const result = await fileUploadModule.handleFileUpload(file, "test-env");
    expect(result.error).toBeUndefined();
    expect(result.url).toBe("/storage/test-env/public/file.jpg");

    // Regression: the file used to be base64-encoded and rebuilt as a Blob before this point. It now goes on the form as-is, so `fetch` streams it and nothing copies it.
    const uploadBody = mockFetch.mock.calls[1][1].body as FormData;
    expect(uploadBody.get("key")).toBe("value");
    expect(uploadBody.get("file")).toBe(file);
  });

  test("should handle upload error", async () => {
    const file = createMockFile("test.jpg", "image/jpeg", 1000);

    // Mock successful API response - now returns relative path
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        data: {
          signedUrl: "https://s3.example.com/upload",
          fileUrl: "/storage/test-env/public/file.jpg",
          presignedFields: {
            key: "value",
          },
        },
      }),
    });

    // Mock failed upload response
    mockFetch.mockResolvedValueOnce({
      ok: false,
    });

    const result = await fileUploadModule.handleFileUpload(file, "test-env");
    expect(result.error).toBe(fileUploadModule.FileUploadError.STORAGE_UPLOAD_FAILED);
    expect(result.url).toBe("");
  });

  test("should return STORAGE_UPLOAD_FAILED when storage upload request throws", async () => {
    const file = createMockFile("test.jpg", "image/jpeg", 1000);

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        data: {
          signedUrl: "https://s3.example.com/upload",
          fileUrl: "/storage/test-env/public/file.jpg",
          presignedFields: {
            key: "value",
          },
        },
      }),
    });

    mockFetch.mockRejectedValueOnce(new Error("Network error"));

    const result = await fileUploadModule.handleFileUpload(file, "test-env");
    expect(result.error).toBe(fileUploadModule.FileUploadError.STORAGE_UPLOAD_FAILED);
    expect(result.url).toBe("");
  });

  test("should catch unexpected errors and return UPLOAD_FAILED", async () => {
    const file = createMockFile("test.jpg", "image/jpeg", 1000);

    // The signing request is the first thing that can throw now that the file itself is never read.
    mockFetch.mockRejectedValueOnce(new Error("Unexpected crash while signing"));

    const result = await fileUploadModule.handleFileUpload(file, "env-crash");

    expect(result.error).toBe(fileUploadModule.FileUploadError.UPLOAD_FAILED);
    expect(result.url).toBe("");
  });
});

describe("fileUploadModule.toBase64", () => {
  test("resolves with base64 string when FileReader succeeds", async () => {
    const dummyFile = new File(["hello"], "hello.txt", { type: "text/plain" });

    // Mock FileReader
    const mockReadAsDataURL = vi.fn();
    const mockFileReaderInstance = {
      readAsDataURL: mockReadAsDataURL,
      onload: null as ((this: FileReader, ev: ProgressEvent<FileReader>) => any) | null,
      onerror: null,
      result: "data:text/plain;base64,aGVsbG8=",
    };

    globalThis.FileReader = vi.fn(function FileReader() {
      return mockFileReaderInstance as unknown as FileReader;
    }) as any;

    const promise = fileUploadModule.toBase64(dummyFile);

    // Trigger the onload manually
    mockFileReaderInstance.onload?.call(
      mockFileReaderInstance as unknown as FileReader,
      new Event("load") as unknown as ProgressEvent<FileReader>
    );

    const result = await promise;
    expect(result).toBe("data:text/plain;base64,aGVsbG8=");
  });

  test("rejects when FileReader errors", async () => {
    const dummyFile = new File(["oops"], "oops.txt", { type: "text/plain" });

    const mockReadAsDataURL = vi.fn();
    const mockFileReaderInstance = {
      readAsDataURL: mockReadAsDataURL,
      onload: null,
      onerror: null as ((this: FileReader, ev: ProgressEvent<FileReader>) => any) | null,
      result: null,
    };

    globalThis.FileReader = vi.fn(function FileReader() {
      return mockFileReaderInstance as unknown as FileReader;
    }) as any;

    const promise = fileUploadModule.toBase64(dummyFile);

    // Simulate error
    mockFileReaderInstance.onerror?.call(
      mockFileReaderInstance as unknown as FileReader,
      new Event("error") as unknown as ProgressEvent<FileReader>
    );

    await expect(promise).rejects.toThrow();
  });
});
