import type { InfiniteData } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import {
  type TWorkflowListItem,
  type TWorkflowListPage,
  buildWorkflowListSearchParams,
  buildWorkflowRunListSearchParams,
  classifyWorkflowSaveError,
  flattenWorkflowPages,
  getWorkflowApiErrorMessage,
  removeWorkflowFromInfiniteData,
  workflowKeys,
  workflowsApi,
} from "./api";

describe("buildWorkflowListSearchParams", () => {
  test("encodes workspace, limit, cursor, sort and the filter[...] family", () => {
    const params = buildWorkflowListSearchParams({
      workspaceId: "ws_1",
      limit: 12,
      cursor: "cursor_1",
      filters: { nameContains: "  onboarding  ", statusIn: ["draft", "enabled"], sortBy: "name" },
    });
    expect(params.get("workspaceId")).toBe("ws_1");
    expect(params.get("limit")).toBe("12");
    expect(params.get("cursor")).toBe("cursor_1");
    expect(params.get("sortBy")).toBe("name");
    expect(params.get("filter[name][contains]")).toBe("onboarding");
    expect(params.getAll("filter[status][in]")).toEqual(["draft", "enabled"]);
  });

  test("omits cursor, sort and filters when not provided, and a blank name", () => {
    const params = buildWorkflowListSearchParams({
      workspaceId: "ws_1",
      limit: 12,
      filters: { nameContains: "   " },
    });
    expect(params.has("cursor")).toBe(false);
    expect(params.has("sortBy")).toBe(false);
    expect(params.has("filter[name][contains]")).toBe(false);
    expect(params.has("filter[status][in]")).toBe(false);
  });
});

describe("buildWorkflowRunListSearchParams", () => {
  test("encodes workspace, limit, cursor and the run filter family", () => {
    const params = buildWorkflowRunListSearchParams({
      workspaceId: "ws_1",
      limit: 25,
      cursor: "cursor_1",
      filters: {
        workflowId: "wf_1",
        responseId: "rsp_1",
        statusIn: ["failed", "completed"],
        isDryRun: false,
      },
    });
    expect(params.get("workflowId")).toBe("wf_1");
    expect(params.get("responseId")).toBe("rsp_1");
    expect(params.get("filter[isDryRun][eq]")).toBe("false");
    expect(params.getAll("filter[status][in]")).toEqual(["failed", "completed"]);
    expect(params.get("cursor")).toBe("cursor_1");
  });

  test("omits cursor and filters when not provided", () => {
    const params = buildWorkflowRunListSearchParams({ workspaceId: "ws_1", limit: 20 });
    expect([...params.keys()]).toEqual(["workspaceId", "limit"]);
  });
});

describe("list cache helpers", () => {
  const item = (id: string) => ({ id }) as TWorkflowListItem;
  const data: InfiniteData<TWorkflowListPage> = {
    pages: [
      { data: [item("a"), item("b")], meta: { limit: 2, nextCursor: "c1" } },
      { data: [item("c")], meta: { limit: 2, nextCursor: null } },
    ],
    pageParams: [null, "c1"],
  };

  test("a different search term produces a distinct list key under the lists key", () => {
    const key = workflowKeys.list({ workspaceId: "ws", limit: 12, nameContains: "a" });
    expect(key.slice(0, 2)).toEqual(workflowKeys.lists());
    expect(key).not.toEqual(workflowKeys.list({ workspaceId: "ws", limit: 12, nameContains: "b" }));
  });

  test("flattens pages in order and drops a removed workflow from its page", () => {
    expect(flattenWorkflowPages(data).map((w) => w.id)).toEqual(["a", "b", "c"]);
    expect(flattenWorkflowPages(removeWorkflowFromInfiniteData(data, "b")).map((w) => w.id)).toEqual([
      "a",
      "c",
    ]);
    expect(flattenWorkflowPages(undefined)).toEqual([]);
  });

  test("returns the same reference when nothing matched", () => {
    expect(removeWorkflowFromInfiniteData(data, "zzz")).toBe(data);
    expect(removeWorkflowFromInfiniteData(undefined, "a")).toBeUndefined();
  });
});

describe("getWorkflowApiErrorMessage and classifyWorkflowSaveError", () => {
  const refused = new ApiError(
    422,
    "invalid_workflow_state",
    "Archived workflows are read-only; unarchive before editing."
  );

  test("surfaces the API's own detail for a refusal", () => {
    expect(getWorkflowApiErrorMessage(refused, "fallback")).toBe(
      "Archived workflows are read-only; unarchive before editing."
    );
    expect(classifyWorkflowSaveError(refused)).toBe("rejected");
  });

  test.each([
    ["an empty detail", new ApiError(400, "problem", "   ")],
    ["a 502 from a gateway", new ApiError(502, "http_error", "Bad Gateway")],
    ["an offline fetch rejection", new TypeError("Failed to fetch")],
    ["the mutation timeout", new DOMException("The operation timed out.", "TimeoutError")],
    ["a non-Error throw", "kaboom"],
  ])("falls back to the caller's copy for %s", (_label, error) => {
    expect(getWorkflowApiErrorMessage(error, "fallback")).toBe("fallback");
  });

  test("anything that is not a refusal is worth retrying once back online", () => {
    expect(classifyWorkflowSaveError(new TypeError("Failed to fetch"))).toBe("unreachable");
    expect(classifyWorkflowSaveError(new ApiError(503, "http_error", "Unavailable"))).toBe("unreachable");
  });
});

describe("workflowsApi requests", () => {
  const fetchMock = vi.fn();
  const json = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  test("unwraps the resource and sends lifecycle operations to their endpoints", async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(json(200, { data: { id: "wf_1", status: "enabled" } }))
    );
    await expect(workflowsApi.enable("wf_1")).resolves.toMatchObject({ id: "wf_1", status: "enabled" });
    await workflowsApi.archive("wf_1");
    await workflowsApi.unarchive("wf_1");
    await workflowsApi.disable("wf_1");
    expect(
      fetchMock.mock.calls.map(([path, init]) => `${(init as RequestInit).method} ${path as string}`)
    ).toEqual([
      "POST /api/v3/workflows/wf_1/enable",
      "POST /api/v3/workflows/wf_1/archive",
      "POST /api/v3/workflows/wf_1/unarchive",
      "POST /api/v3/workflows/wf_1/disable",
    ]);
  });

  test("duplicate posts an empty object without a name, and delete resolves on a 204", async () => {
    fetchMock.mockResolvedValueOnce(json(201, { data: { id: "wf_2" } }));
    await workflowsApi.duplicate("wf_1");
    expect((fetchMock.mock.calls[0]![1] as RequestInit).body).toBe("{}");
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await expect(workflowsApi.remove("wf_1")).resolves.toBeUndefined();
    expect((fetchMock.mock.calls[1]![1] as RequestInit).method).toBe("DELETE");
  });

  test("a problem response becomes an ApiError carrying its detail, code and invalid params", async () => {
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          title: "Unprocessable Entity",
          status: 422,
          detail: "A send_email recipient cannot access this workspace.",
          code: "workflow_not_executable",
          invalid_params: [
            { name: "definition.nodes.config.to", reason: "Recipient x cannot access this workspace." },
          ],
        }),
        { status: 422, headers: { "content-type": "application/problem+json" } }
      )
    );
    const error = await workflowsApi.enable("wf_1").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      status: 422,
      code: "workflow_not_executable",
      message: "A send_email recipient cannot access this workspace.",
      invalidParams: [
        { name: "definition.nodes.config.to", reason: "Recipient x cannot access this workspace." },
      ],
    });
  });
});
