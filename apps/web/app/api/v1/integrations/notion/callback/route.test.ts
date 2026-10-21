import { NextRequest } from "next/server";
import { beforeEach, describe, expect, test, vi } from "vitest";

/**
 * `withV1ApiWrapper` is reduced to its handler: auth, rate limiting and audit logging are orthogonal to
 * the token-exchange contract proved here.
 */
const mocks = vi.hoisted(() => ({
  canUserWriteWorkspaceIntegrations: vi.fn(),
  consumeIntegrationOAuthState: vi.fn(),
  createOrUpdateIntegration: vi.fn(),
  getIntegrationByType: vi.fn(),
}));

vi.mock("@/lib/api/with-api-logging", () => ({
  withV1ApiWrapper: ({ handler }: { handler: unknown }) => handler,
}));

vi.mock("@/lib/constants", () => ({
  NOTION_OAUTH_CLIENT_ID: "client-id",
  NOTION_OAUTH_CLIENT_SECRET: "client-secret",
  NOTION_REDIRECT_URI: "http://localhost:3000/api/v1/integrations/notion/callback",
  WEBAPP_URL: "http://localhost:3000",
}));

vi.mock("@/lib/oauth/integration-state", () => ({
  IntegrationOAuthStateError: class IntegrationOAuthStateError extends Error {},
  consumeIntegrationOAuthState: mocks.consumeIntegrationOAuthState,
  getSafeOAuthCallbackError: (error: string | null) => error,
}));

vi.mock("@/lib/integration/service", () => ({
  createOrUpdateIntegration: mocks.createOrUpdateIntegration,
  getIntegrationByType: mocks.getIntegrationByType,
}));

vi.mock("@/lib/workspace/auth", () => ({
  canUserWriteWorkspaceIntegrations: mocks.canUserWriteWorkspaceIntegrations,
}));

vi.mock("@/lib/posthog", () => ({ capturePostHogEvent: vi.fn() }));
vi.mock("@/lib/utils/helper", () => ({ getOrganizationIdFromWorkspaceId: vi.fn() }));

const { GET } = await import("./route");

const fetchMock = vi.fn();

const jsonResponse = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const validToken = {
  access_token: "secret_token",
  bot_id: "bot_1",
  workspace_id: "notion_ws",
};

const callGet = async (): Promise<Response> => {
  const result = await (GET as unknown as (ctx: unknown) => Promise<{ response: Response }>)({
    req: new NextRequest("http://localhost:3000/api/v1/integrations/notion/callback?code=abc&state=xyz"),
    authentication: { user: { id: "user-1" } },
  });

  return result.response;
};

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  mocks.consumeIntegrationOAuthState.mockResolvedValue({ workspaceId: "ws_1" });
  mocks.canUserWriteWorkspaceIntegrations.mockResolvedValue(true);
  mocks.getIntegrationByType.mockResolvedValue(null);
  mocks.createOrUpdateIntegration.mockResolvedValue({ id: "integration-1" });
});

// `createOrUpdateIntegration` upserts, so an unchecked token response used to replace a live Notion
// connection with Notion's error body: the settings page then read no `bot_id` and reported "not
// connected", while the survey mappings still pointed at credentials that no longer existed.
describe("GET /api/v1/integrations/notion/callback", () => {
  test("stores the credential when Notion returns a token", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, validToken));

    const response = await callGet();

    expect(response.status).toBe(302);
    expect(new URL(response.headers.get("location") ?? "").searchParams.get("error")).toBeNull();
    expect(mocks.createOrUpdateIntegration).toHaveBeenCalledWith("ws_1", {
      type: "notion",
      config: { key: validToken, data: [] },
    });
  });

  test("keeps the existing credential when Notion rejects the exchange", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(400, { object: "error", status: 400, code: "invalid_grant", message: "nope" })
    );

    const response = await callGet();

    expect(mocks.createOrUpdateIntegration).not.toHaveBeenCalled();
    expect(response.status).toBe(302);
    expect(new URL(response.headers.get("location") ?? "").searchParams.get("error")).toBe("oauth_error");
  });

  test("keeps the existing credential when a 2xx body carries no usable token", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { object: "error", message: "unexpected" }));

    const response = await callGet();

    expect(mocks.createOrUpdateIntegration).not.toHaveBeenCalled();
    expect(new URL(response.headers.get("location") ?? "").searchParams.get("error")).toBe("oauth_error");
  });

  test("keeps the existing credential when the body is not JSON at all", async () => {
    fetchMock.mockResolvedValue(new Response("<html>gateway</html>", { status: 200 }));

    const response = await callGet();

    expect(mocks.createOrUpdateIntegration).not.toHaveBeenCalled();
    expect(new URL(response.headers.get("location") ?? "").searchParams.get("error")).toBe("oauth_error");
  });
});
