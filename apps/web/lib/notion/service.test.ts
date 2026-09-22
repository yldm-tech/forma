import { beforeEach, describe, expect, test, vi } from "vitest";
import { TIntegrationNotionConfig } from "@forma/types/integration/notion";

const integrationServiceMock = vi.hoisted(() => ({ getIntegrationByType: vi.fn() }));
vi.mock("@/lib/integration/service", () => integrationServiceMock);

const { getNotionDatabases, writeData } = await import("@/lib/notion/service");

const config = {
  key: {
    access_token: "ntn_notion-token",
    bot_id: "bot_1",
    token_type: "bearer",
    duplicated_template_id: null,
    owner: { type: "user", user: null },
    workspace_icon: null,
    workspace_id: "notion_ws_1",
    workspace_name: "Acme",
  },
  data: [],
} as TIntegrationNotionConfig;

const jsonResponse = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
});

// ENG: Notion writes discarded the response entirely, so a rejected page creation — a revoked grant, a
// renamed database column — resolved exactly like a successful delivery and the response pipeline logged
// nothing at all. These assert the failure is now observable to the caller.
describe("writeData", () => {
  test("resolves when Notion accepts the page", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { object: "page", id: "page_1" }));

    await expect(writeData("db_1", { Name: { title: [] } }, config)).resolves.toBeUndefined();
  });

  test("rejects with Notion's code and message on a 400", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(400, { object: "error", code: "validation_error", message: "Name is not a property" })
    );

    await expect(writeData("db_1", { Name: { title: [] } }, config)).rejects.toThrow(
      /Notion API error creating page: 400 .*validation_error Name is not a property/
    );
  });

  test("rejects on a 401 whose body is not JSON", async () => {
    fetchMock.mockResolvedValue(new Response("<html>unauthorized</html>", { status: 401 }));

    await expect(writeData("db_1", {}, config)).rejects.toThrow(/Notion API error creating page: 401/);
  });
});

// ENG: the access token used to be encrypted by the Notion OAuth callback and decrypted here, so Notion
// was the one provider whose credentials were protected at rest. That moved into the integration store,
// which decrypts on read for every provider — decrypting a second time here would fail on the plaintext.
describe("getHeaders", () => {
  test("authorizes with the token as the store handed it over", async () => {
    integrationServiceMock.getIntegrationByType.mockResolvedValue({ config });
    fetchMock.mockResolvedValue(jsonResponse(200, { results: [] }));

    await getNotionDatabases("ws_1");

    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe("Bearer ntn_notion-token");
  });
});

describe("getNotionDatabases", () => {
  test("rejects instead of reporting an empty database list when the search is unauthorised", async () => {
    integrationServiceMock.getIntegrationByType.mockResolvedValue({ config });
    fetchMock.mockResolvedValue(
      jsonResponse(401, { object: "error", code: "unauthorized", message: "API token is invalid." })
    );

    await expect(getNotionDatabases("ws_1")).rejects.toThrow(
      /Notion API error fetching databases: 401 .*unauthorized API token is invalid./
    );
  });

  test("returns the databases Notion reports", async () => {
    integrationServiceMock.getIntegrationByType.mockResolvedValue({ config });
    fetchMock.mockResolvedValue(jsonResponse(200, { results: [{ id: "db_1" }] }));

    await expect(getNotionDatabases("ws_1")).resolves.toEqual([{ id: "db_1" }]);
  });
});
