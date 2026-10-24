import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import { createSurveyFromTemplate, surveyListSearchParams, surveysApi, v3ErrorMessage } from "./api";

const respond = (status: number, body: unknown, contentType = "application/json") =>
  vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: { "content-type": contentType },
    })
  );

const call = (fetchMock: ReturnType<typeof respond>) => {
  // The client sends a JSON string or no body at all.
  const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit & { body?: string }];
  return {
    path,
    method: init.method,
    body: init.body === undefined ? undefined : JSON.parse(init.body),
  };
};

afterEach(() => {
  vi.unstubAllGlobals();
});

// forma2's modules/survey/list/lib/v3-surveys-client.test.ts.
describe("surveyListSearchParams", () => {
  test("emits only the supported v3 parameters, with normalized filter values", () => {
    const params = surveyListSearchParams({
      workspaceId: "ws_1",
      limit: 20,
      cursor: "cursor_1",
      filters: {
        name: "  Product feedback  ",
        status: ["paused", "draft"],
        type: ["link", "app"],
        sortBy: "relevance",
      },
    });
    expect(params.toString()).toBe(
      "workspaceId=ws_1&limit=20&sortBy=relevance&cursor=cursor_1&filter%5Bname%5D%5Bcontains%5D=Product+feedback&filter%5Bstatus%5D%5Bin%5D=draft&filter%5Bstatus%5D%5Bin%5D=paused&filter%5Btype%5D%5Bin%5D=app&filter%5Btype%5D%5Bin%5D=link"
    );
  });

  test("asks without counts only when told to, and leaves out an empty cursor", () => {
    const filters = { name: "", status: [], type: [], sortBy: "relevance" as const };
    expect(
      surveyListSearchParams({
        workspaceId: "ws_1",
        limit: 12,
        cursor: "c",
        includeTotalCount: false,
        filters,
      }).toString()
    ).toBe("workspaceId=ws_1&limit=12&sortBy=relevance&cursor=c&includeTotalCount=false");
    expect(
      surveyListSearchParams({
        workspaceId: "ws_1",
        limit: 12,
        cursor: null,
        includeTotalCount: true,
        filters,
      }).toString()
    ).toBe("workspaceId=ws_1&limit=12&sortBy=relevance");
  });

  test("passes the archived pseudo-status through as a status", () => {
    const params = surveyListSearchParams({
      workspaceId: "ws_1",
      limit: 12,
      filters: { name: "", status: ["archived"], type: [], sortBy: "updatedAt" },
    });
    expect(params.getAll("filter[status][in]")).toEqual(["archived"]);
  });
});

describe("surveysApi", () => {
  test("lists a page from /api/v3/surveys", async () => {
    const page = { data: [], meta: { limit: 12, nextCursor: null, totalCount: 0, workspaceSurveyCount: 0 } };
    const fetchMock = respond(200, page);
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      surveysApi.list({
        workspaceId: "ws_1",
        limit: 12,
        filters: { name: "", status: [], type: [], sortBy: "relevance" },
      })
    ).resolves.toEqual(page);
    expect(call(fetchMock)).toMatchObject({
      path: "/api/v3/surveys?workspaceId=ws_1&limit=12&sortBy=relevance",
      method: "GET",
    });
  });

  test("changes a status and renames through PATCH with only that field", async () => {
    const fetchMock = respond(200, { data: { id: "s1", status: "paused" } });
    vi.stubGlobal("fetch", fetchMock);
    await surveysApi.updateStatus("s1", "paused");
    expect(call(fetchMock)).toEqual({
      path: "/api/v3/surveys/s1",
      method: "PATCH",
      body: { status: "paused" },
    });

    const renameMock = respond(200, { data: { id: "s1", name: "New" } });
    vi.stubGlobal("fetch", renameMock);
    await surveysApi.rename("s1", "New");
    expect(call(renameMock)).toEqual({ path: "/api/v3/surveys/s1", method: "PATCH", body: { name: "New" } });
  });

  test("archives and restores, answering the lifecycle acknowledgement", async () => {
    const ack = { id: "s1", status: "paused", archivedAt: "2026-10-22T10:00:00.000Z" };
    const fetchMock = respond(200, { data: ack });
    vi.stubGlobal("fetch", fetchMock);
    await expect(surveysApi.archive("s1")).resolves.toEqual(ack);
    expect(call(fetchMock)).toMatchObject({ path: "/api/v3/surveys/s1/archive", method: "POST" });

    const restoreMock = respond(200, { data: { ...ack, archivedAt: null } });
    vi.stubGlobal("fetch", restoreMock);
    await expect(surveysApi.restore("s1")).resolves.toEqual({ ...ack, archivedAt: null });
    expect(call(restoreMock)).toMatchObject({ path: "/api/v3/surveys/s1/restore", method: "POST" });
  });

  test("deletes with DELETE and copies to a target workspace", async () => {
    const deleteMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", deleteMock);
    await expect(surveysApi.delete("s1")).resolves.toBeUndefined();
    expect(call(deleteMock)).toMatchObject({ path: "/api/v3/surveys/s1", method: "DELETE" });

    const copyMock = respond(201, { id: "s2" });
    vi.stubGlobal("fetch", copyMock);
    await surveysApi.copy("s1", "ws_2");
    expect(call(copyMock)).toEqual({
      path: "/api/app/surveys/s1/copy",
      method: "POST",
      body: { targetWorkspaceId: "ws_2" },
    });
  });

  test("a v3 problem's detail becomes the error message", async () => {
    vi.stubGlobal(
      "fetch",
      respond(
        422,
        {
          title: "Unprocessable Content",
          status: 422,
          detail: "Survey is archived",
          code: "unprocessable_content",
        },
        "application/problem+json"
      )
    );
    const error = await surveysApi.rename("s1", "x").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(v3ErrorMessage(error, "fallback")).toBe("Survey is archived");
  });
});

describe("createSurveyFromTemplate", () => {
  test("posts the template request and answers the created survey", async () => {
    const fetchMock = respond(201, { data: { id: "s9", name: "New Survey" } });
    vi.stubGlobal("fetch", fetchMock);
    const input = {
      workspaceId: "ws_1",
      templateId: "custom",
      source: "custom" as const,
      surveyType: "link" as const,
      defaultLanguage: "de-DE",
    };
    await expect(createSurveyFromTemplate(input)).resolves.toMatchObject({ id: "s9" });
    expect(call(fetchMock)).toEqual({ path: "/api/v3/surveys/templates", method: "POST", body: input });
  });
});

describe("v3ErrorMessage", () => {
  test("uses the error's own message, else the fallback", () => {
    expect(
      v3ErrorMessage(new ApiError(403, "forbidden", "You are not authorized to access this resource"), "f")
    ).toBe("You are not authorized to access this resource");
    expect(v3ErrorMessage(new TypeError("Failed to fetch"), "f")).toBe("Failed to fetch");
    expect(v3ErrorMessage(new Error(""), "f")).toBe("f");
    expect(v3ErrorMessage("nope", "f")).toBe("f");
  });
});
