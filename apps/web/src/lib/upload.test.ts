import type { TFunction } from "i18next";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { CSRF_HEADER } from "./api";
import { toast } from "./toast";
import { FileUploadError, STORAGE_PATH, handleFileUpload, showFileUploadErrorToast } from "./upload";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const WORKSPACE = "0190a000-0000-7000-8000-000000000002";
const SIGNED = {
  data: {
    signedUrl: "https://bucket.example.com/uploads",
    // The order S3's presigned POST lists them in; the policy covers every one of them.
    presignedFields: { "Content-Type": "image/png", bucket: "uploads", key: "k/logo.png", Policy: "p" },
    fileUrl: `/storage/${WORKSPACE}/public/logo--fid--1.png`,
  },
};

const file = (name = "logo.png", size = 1000) => {
  const f = new File(["x"], name, { type: "image/png" });
  Object.defineProperty(f, "size", { value: size });
  return f;
};

/** The server and the bucket answering in turn. */
const stub = (...answers: (Response | Error)[]) => {
  const fetchMock = vi.fn(async () => {
    const answer = answers.shift();
    if (!answer) throw new Error("unexpected request");
    if (answer instanceof Error) throw answer;
    return answer;
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
};

afterEach(() => {
  vi.unstubAllGlobals();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

describe("handleFileUpload", () => {
  test("signs the upload with the session, posts the signed fields then the file to the bucket, and keeps the relative URL", async () => {
    const fetchMock = stub(json(200, SIGNED), new Response(null, { status: 201 }));
    const logo = file();

    const result = await handleFileUpload(logo, WORKSPACE, ["png", "jpg"]);

    expect(result).toEqual({ url: SIGNED.data.fileUrl });
    const [signPath, signInit] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(signPath).toBe(STORAGE_PATH);
    expect(signInit.method).toBe("POST");
    expect(new Headers(signInit.headers).get(CSRF_HEADER)).toBe("XMLHttpRequest");
    expect(JSON.parse(signInit.body as string)).toEqual({
      fileName: "logo.png",
      fileType: "image/png",
      allowedFileExtensions: ["png", "jpg"],
      workspaceId: WORKSPACE,
    });
    const [bucketUrl, bucketInit] = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(bucketUrl).toBe(SIGNED.data.signedUrl);
    expect(bucketInit.method).toBe("POST");
    const form = bucketInit.body as FormData;
    expect([...form.keys()]).toEqual(["Content-Type", "bucket", "key", "Policy", "file"]);
    expect(form.get("key")).toBe("k/logo.png");
    expect((form.get("file") as File).name).toBe("logo.png");
  });

  test("without allowed extensions the field is left out, as forma2 sent it", async () => {
    const fetchMock = stub(json(200, SIGNED), new Response(null, { status: 204 }));
    await handleFileUpload(file(), WORKSPACE);
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(init.body as string)).not.toHaveProperty("allowedFileExtensions");
  });

  test("refuses a file over 5 MB without a request, and anything that is not a file", async () => {
    const fetchMock = stub();
    expect(await handleFileUpload(file("big.png", 5 * 1024 * 1024 + 1), WORKSPACE)).toEqual({
      error: FileUploadError.FILE_SIZE_EXCEEDED,
      url: "",
    });
    expect(await handleFileUpload("logo.png" as unknown as File, WORKSPACE)).toEqual({
      error: FileUploadError.NO_FILE,
      url: "",
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test.each([
    [
      "a rejected file name",
      json(400, { code: "bad_request", message: "Invalid input", details: { fileName: "----.png" } }),
      FileUploadError.INVALID_FILE_NAME,
    ],
    [
      "a bucket the server cannot use",
      json(500, {
        code: "internal_server_error",
        message: "File storage is not configured correctly. Please check your file upload settings.",
        details: { storage_error_code: "s3_credentials_error" },
      }),
      FileUploadError.STORAGE_NOT_CONFIGURED,
    ],
    [
      "any other server failure",
      json(500, { code: "internal_server_error", message: "Internal server error", details: {} }),
      FileUploadError.UPLOAD_FAILED,
    ],
    [
      "a refusal without a file name",
      json(401, { code: "unauthorized", message: "You are not authorized", details: {} }),
      FileUploadError.UPLOAD_FAILED,
    ],
    ["an unreachable server", new TypeError("Failed to fetch"), FileUploadError.UPLOAD_FAILED],
  ])("signing fails with %s", async (_name, answer, error) => {
    const fetchMock = stub(answer);
    expect(await handleFileUpload(file(), WORKSPACE)).toEqual({ error, url: "" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  test.each([
    ["refuses the file", new Response("<Error/>", { status: 403 })],
    ["cannot be reached", new TypeError("Failed to fetch")],
  ])("the upload fails when the bucket %s", async (_name, answer) => {
    stub(json(200, SIGNED), answer);
    expect(await handleFileUpload(file(), WORKSPACE)).toEqual({
      error: FileUploadError.STORAGE_UPLOAD_FAILED,
      url: "",
    });
  });
});

describe("showFileUploadErrorToast", () => {
  const t = ((key: string) => key) as unknown as TFunction;

  test.each([
    [
      FileUploadError.STORAGE_NOT_CONFIGURED,
      "common.file_storage_not_set_up",
      "storage-not-configured-toast",
    ],
    [
      FileUploadError.STORAGE_UPLOAD_FAILED,
      "common.file_upload_service_unavailable",
      "storage-not-configured-toast",
    ],
    [FileUploadError.INVALID_FILE_NAME, "common.invalid_file_name", undefined],
    [FileUploadError.FILE_SIZE_EXCEEDED, "common.file_size_must_be_less_than_5_mb", undefined],
    [FileUploadError.UPLOAD_FAILED, "common.upload_failed", undefined],
  ])("%s shows %s", (error, message, id) => {
    showFileUploadErrorToast(error, t);
    const [shown] = toast.getSnapshot();
    expect(shown).toMatchObject({ kind: "error", message });
    if (id) expect(shown?.id).toBe(id);
  });
});
