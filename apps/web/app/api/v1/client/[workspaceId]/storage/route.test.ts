import { beforeEach, describe, expect, test, vi } from "vitest";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import { MAX_FILE_UPLOAD_SIZE_BYTES } from "@/lib/constants";
import { POST } from "./route";

/**
 * The upload cap handed to the presigned POST is the one the survey advertises.
 *
 * `element.maxSizeInMB` is enforced nowhere else on the server: the respondent's browser checks it, and this endpoint needs no authentication, so whatever number reaches `getSignedUrlForUpload` is the only thing standing between a crafted POST and the operator's bucket. It used to come from `getBiggerUploadFileSizePermission()`, which returns `true` for every install, so every upload got the 1 GB branch no matter what the survey said.
 *
 * `withV1ApiWrapper` is reduced to its handler — auth, rate limiting and audit logging are orthogonal to the cap. `getSurveyFileUploadConfigs` is deliberately NOT mocked: it is how the route reads the advertised size, so mocking it would prove nothing.
 */
const mocks = vi.hoisted(() => ({
  applyRateLimit: vi.fn(),
  getErrorResponseFromStorageError: vi.fn(),
  getOrganization: vi.fn(),
  getOrganizationIdFromWorkspaceId: vi.fn(),
  getSignedUrlForUpload: vi.fn(),
  getSurvey: vi.fn(),
  resolveClientApiIds: vi.fn(),
  validateSurveyAllowsFileUpload: vi.fn(),
}));

vi.mock("@/lib/api/with-api-logging", () => ({
  withV1ApiWrapper: ({ handler }: { handler: unknown }) => handler,
}));

vi.mock("@/lib/organization/service", () => ({
  getOrganization: mocks.getOrganization,
}));

vi.mock("@/lib/survey/service", () => ({
  getSurvey: mocks.getSurvey,
}));

vi.mock("@/lib/utils/helper", () => ({
  getOrganizationIdFromWorkspaceId: mocks.getOrganizationIdFromWorkspaceId,
}));

vi.mock("@/lib/utils/resolve-client-id", () => ({
  resolveClientApiIds: mocks.resolveClientApiIds,
}));

vi.mock("@/modules/core/rate-limit/helpers", () => ({
  applyRateLimit: mocks.applyRateLimit,
}));

vi.mock("@/modules/storage/service", () => ({
  getSignedUrlForUpload: mocks.getSignedUrlForUpload,
}));

vi.mock("@/modules/storage/utils", () => ({
  getErrorResponseFromStorageError: mocks.getErrorResponseFromStorageError,
  validateSurveyAllowsFileUpload: mocks.validateSurveyAllowsFileUpload,
}));

const workspaceId = "lygo31gfsexlr4lh6rq8dxyl";
const surveyId = "cgt5e6dw1vsf1bv2ki5gj845";
const elementId = "upload_1";

const surveyAdvertising = (maxSizeInMB: number | undefined) => ({
  id: surveyId,
  workspaceId,
  blocks: [
    {
      id: "block_1",
      elements: [{ id: elementId, type: TSurveyElementTypeEnum.FileUpload, maxSizeInMB }],
    },
  ],
  questions: [],
});

const postUpload = async () => {
  const req = new Request(`https://api.test/api/v1/client/${workspaceId}/storage`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ fileName: "receipt.png", fileType: "image/png", surveyId, elementId }),
  });

  return (POST as unknown as (params: unknown) => Promise<{ response: Response }>)({
    req,
    props: { params: Promise.resolve({ workspaceId }) },
  });
};

const presignedMaxSize = (): number => mocks.getSignedUrlForUpload.mock.calls[0][4];

describe("POST /api/v1/client/[workspaceId]/storage — respondent upload cap", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    mocks.resolveClientApiIds.mockResolvedValue({ workspaceId });
    mocks.getOrganizationIdFromWorkspaceId.mockResolvedValue("org_1");
    mocks.getOrganization.mockResolvedValue({ id: "org_1" });
    mocks.applyRateLimit.mockResolvedValue(undefined);
    mocks.validateSurveyAllowsFileUpload.mockReturnValue({ ok: true });
    mocks.getSignedUrlForUpload.mockResolvedValue({
      ok: true,
      data: { signedUrl: "https://s3.test/upload", presignedFields: {}, fileUrl: "https://app.test/f.png" },
    });
  });

  test.each([5, 25])(
    "presigns exactly the %i MB the element advertises, not the ceiling",
    async (maxSizeInMB) => {
      mocks.getSurvey.mockResolvedValue(surveyAdvertising(maxSizeInMB));

      const result = await postUpload();

      expect(result.response.status).toBe(200);
      expect(presignedMaxSize()).toBe(maxSizeInMB * 1024 * 1024);
      expect(presignedMaxSize()).toBeLessThan(MAX_FILE_UPLOAD_SIZE_BYTES);
    }
  );

  test("clamps an element advertising more than the ceiling", async () => {
    mocks.getSurvey.mockResolvedValue(surveyAdvertising(4096));

    await postUpload();

    expect(presignedMaxSize()).toBe(MAX_FILE_UPLOAD_SIZE_BYTES);
  });

  test("falls back to the ceiling when the element advertises no size", async () => {
    mocks.getSurvey.mockResolvedValue(surveyAdvertising(undefined));

    await postUpload();

    expect(presignedMaxSize()).toBe(MAX_FILE_UPLOAD_SIZE_BYTES);
  });
});
