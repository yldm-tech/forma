import { beforeEach, describe, expect, test, vi } from "vitest";
import type { authenticatedApiClient } from "@/modules/api/v2/auth/authenticated-api-client";
import { handleApiError } from "@/modules/api/v2/lib/utils";
import { getWorkspaceIdFromSurveyIds } from "@/modules/api/v2/management/lib/helper";
import { createWebhook } from "./lib/webhook";
import { POST } from "./route";

const { mockAuthenticatedApiClient, mockCreatedResponse } = vi.hoisted(() => ({
  mockAuthenticatedApiClient: vi.fn(),
  mockCreatedResponse: vi.fn(),
}));

vi.mock("@/modules/api/v2/auth/authenticated-api-client", () => ({
  authenticatedApiClient: mockAuthenticatedApiClient,
}));
vi.mock("@/modules/api/v2/lib/response", () => ({
  responses: { createdResponse: mockCreatedResponse, successResponse: vi.fn() },
}));
vi.mock("@/modules/api/v2/lib/utils", () => ({ handleApiError: vi.fn() }));
vi.mock("@/modules/api/v2/management/lib/helper", () => ({
  getWorkspaceId: vi.fn(),
  getWorkspaceIdFromSurveyIds: vi.fn(),
}));
vi.mock("@/modules/api/v2/management/lib/authorized-workspace-ids", () => ({
  getAuthorizedApiKeyWorkspaceIds: vi.fn(),
}));
vi.mock("@/modules/api/v2/management/lib/workspace-resolver", () => ({ resolveBodyIdsV2: vi.fn() }));
vi.mock("./lib/webhook", () => ({ createWebhook: vi.fn(), getWebhooks: vi.fn() }));

const request = new Request("http://localhost/api/v2/management/webhooks", { method: "POST" });

const postWebhook = async (body: Record<string, unknown>) => {
  mockAuthenticatedApiClient.mockImplementation(
    async ({ handler }: Parameters<typeof authenticatedApiClient>[0]) =>
      handler({ parsedInput: { body }, auditLog: undefined } as never)
  );

  return POST(request as never);
};

const webhookBody = {
  name: "Webhook",
  url: "https://example.com/hook",
  source: "user",
  triggers: ["responseCreated"],
  workspaceId: "workspace-1",
  surveyIds: ["survey-1"],
};

describe("POST /api/v2/management/webhooks", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(createWebhook).mockResolvedValue({ ok: true, data: { id: "webhook-1" } } as never);
    mockCreatedResponse.mockImplementation((body: unknown) => Response.json(body, { status: 201 }));
  });

  test("refuses surveys that belong to another workspace", async () => {
    // A webhook stored across a workspace boundary can never match the fan-out query, so it looks configured and silently delivers nothing. PUT has always rejected this body.
    vi.mocked(getWorkspaceIdFromSurveyIds).mockResolvedValue({ ok: true, data: "workspace-2" });

    await postWebhook(webhookBody);

    expect(createWebhook).not.toHaveBeenCalled();
    expect(handleApiError).toHaveBeenCalledWith(
      request,
      {
        type: "bad_request",
        details: [{ field: "surveyIds", issue: "webhook workspace does not match the surveys workspace" }],
      },
      undefined
    );
  });

  test("creates the webhook when the surveys are in the webhook's workspace", async () => {
    vi.mocked(getWorkspaceIdFromSurveyIds).mockResolvedValue({ ok: true, data: "workspace-1" });

    const response = await postWebhook(webhookBody);

    expect(handleApiError).not.toHaveBeenCalled();
    expect(createWebhook).toHaveBeenCalledWith(webhookBody);
    expect(response.status).toBe(201);
  });

  test("creates a workspace-wide webhook when no surveys are named", async () => {
    const { surveyIds: _surveyIds, ...bodyWithoutSurveys } = webhookBody;

    const response = await postWebhook(bodyWithoutSurveys);

    expect(getWorkspaceIdFromSurveyIds).not.toHaveBeenCalled();
    expect(createWebhook).toHaveBeenCalledWith(bodyWithoutSurveys);
    expect(response.status).toBe(201);
  });
});
