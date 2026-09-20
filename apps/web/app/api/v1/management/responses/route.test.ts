import { NextRequest } from "next/server";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { MAX_ITEMS_PER_PAGE } from "@/app/api/v1/management/lib/pagination";
import { RESPONSES_PER_PAGE } from "@/lib/constants";
import { GET } from "./route";

/**
 * `withV1ApiWrapper` is reduced to its handler: auth, rate limiting and audit logging are orthogonal to the pagination contract proved here.
 */
const mocks = vi.hoisted(() => ({
  can: vi.fn(),
  getResponses: vi.fn(),
  getResponsesByWorkspaceIds: vi.fn(),
  getSurvey: vi.fn(),
}));

vi.mock("@/lib/api/with-api-logging", () => ({
  withV1ApiWrapper: ({ handler }: { handler: unknown }) => handler,
}));

vi.mock("./lib/response", () => ({
  createResponseWithQuotaEvaluation: vi.fn(),
  getResponses: mocks.getResponses,
  getResponsesByWorkspaceIds: mocks.getResponsesByWorkspaceIds,
}));

vi.mock("@/lib/survey/service", () => ({ getSurvey: mocks.getSurvey }));
vi.mock("@/lib/authorization", () => ({ can: mocks.can }));
vi.mock("@/lib/authorization/permission-action", () => ({
  getWorkspaceAuthorizationActionForMethod: vi.fn(() => "read"),
}));
vi.mock("@/lib/pipelines", () => ({ sendToPipeline: vi.fn() }));
vi.mock("@/lib/response/anonymize", () => ({ applyAnonymizePolicy: vi.fn() }));
vi.mock("@/lib/workspace/service", () => ({ getWorkspaceLegacyStoragePrefixes: vi.fn() }));
vi.mock("@/app/api/v1/management/lib/workspace-resolver", () => ({ resolveBodyIds: vi.fn() }));
vi.mock("@/modules/api/lib/validation", () => ({
  formatValidationErrorsForV1Api: vi.fn(),
  validateResponseData: vi.fn(),
}));
vi.mock("@/modules/storage/utils", () => ({
  resolveStorageUrlsInObject: vi.fn((value) => value),
  validateClientFileUploads: vi.fn(),
}));

const authentication = {
  type: "apiKey",
  apiKeyId: "api-key-1",
  organizationId: "organization-1",
  workspacePermissions: [{ workspaceId: "workspace-1", workspaceName: "Workspace", permission: "read" }],
};

const callGet = async (query: string): Promise<Response> => {
  const result = await (GET as unknown as (ctx: unknown) => Promise<{ response: Response }>)({
    req: new NextRequest(`http://localhost/api/v1/management/responses${query}`),
    authentication,
  });

  return result.response;
};

describe("GET /api/v1/management/responses pagination", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getResponses.mockResolvedValue([]);
    mocks.getResponsesByWorkspaceIds.mockResolvedValue([]);
    mocks.getSurvey.mockResolvedValue({ id: "survey-1", workspaceId: "workspace-1" });
    mocks.can.mockResolvedValue(true);
  });

  test("bounds a workspace-wide read that sends no pagination at all", async () => {
    // This is the whole point: an unpaginated call used to reach Prisma with `take: undefined`, which is every response row of every workspace the key can read.
    const response = await callGet("");

    expect(response.status).toBe(200);
    expect(mocks.getResponsesByWorkspaceIds).toHaveBeenCalledWith(["workspace-1"], RESPONSES_PER_PAGE, 0);
  });

  test("rejects a limit above the ceiling instead of asking the database for it", async () => {
    const response = await callGet("?limit=100000000");

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toMatchObject({ code: "bad_request" });
    expect(mocks.getResponsesByWorkspaceIds).not.toHaveBeenCalled();
    expect(mocks.getResponses).not.toHaveBeenCalled();
  });

  test("rejects an oversized limit on the single-survey branch too", async () => {
    const response = await callGet(`?surveyId=survey-1&limit=${MAX_ITEMS_PER_PAGE + 1}`);

    expect(response.status).toBe(400);
    expect(mocks.getResponses).not.toHaveBeenCalled();
  });

  test("passes an accepted limit and skip through", async () => {
    await callGet("?surveyId=survey-1&limit=50&skip=100");

    expect(mocks.getResponses).toHaveBeenCalledWith("survey-1", 50, 100);
  });
});
