import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import type { WorkspaceDetail } from "../../lib/queries/workspaces";
import { QueryClient } from "@tanstack/react-query";
import {
  type SurveyWire,
  forgetEditorSurvey,
  parseSurvey,
  responseCountsQuery,
  saveSurvey,
  surveyEditorKeys,
  toEditorWorkspace,
} from "./api";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

afterEach(() => {
  vi.unstubAllGlobals();
});

const wireSurvey = (): SurveyWire =>
  ({
    id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e",
    createdAt: "2026-10-01T08:00:00Z",
    updatedAt: "2026-10-02T09:30:00.123Z",
    publishOn: "2026-11-01T00:00:00Z",
    closeOn: null,
    archivedAt: null,
    name: "Feedback",
    triggers: [
      {
        actionClass: {
          id: "ac",
          createdAt: "2026-09-01T00:00:00Z",
          updatedAt: "2026-09-02T00:00:00Z",
          name: "New Session",
        },
      },
    ],
    segment: { id: "seg", createdAt: "2026-09-03T00:00:00Z", updatedAt: "2026-09-04T00:00:00Z", filters: [] },
    languages: [
      {
        default: true,
        enabled: true,
        language: {
          id: "lang",
          createdAt: "2026-09-05T00:00:00Z",
          updatedAt: "2026-09-06T00:00:00Z",
          code: "en",
        },
      },
    ],
    followUps: [
      { id: "fu", createdAt: "2026-09-07T00:00:00Z", updatedAt: "2026-09-08T00:00:00Z", name: "Thanks" },
    ],
    blocks: [],
  }) as unknown as SurveyWire;

describe("parseSurvey", () => {
  test("turns every timestamp the editor reads into a Date and adds the empty legacy questions", () => {
    const survey = parseSurvey(wireSurvey());
    expect(survey.createdAt).toEqual(new Date("2026-10-01T08:00:00Z"));
    expect(survey.updatedAt).toEqual(new Date("2026-10-02T09:30:00.123Z"));
    expect(survey.publishOn).toEqual(new Date("2026-11-01T00:00:00Z"));
    expect(survey.closeOn).toBeNull();
    expect(survey.archivedAt).toBeNull();
    expect(survey.questions).toEqual([]);
    expect(survey.triggers[0]?.actionClass.createdAt).toEqual(new Date("2026-09-01T00:00:00Z"));
    expect(survey.segment?.updatedAt).toEqual(new Date("2026-09-04T00:00:00Z"));
    expect(survey.languages[0]?.language.createdAt).toEqual(new Date("2026-09-05T00:00:00Z"));
    expect(survey.followUps[0]?.updatedAt).toEqual(new Date("2026-09-08T00:00:00Z"));
  });

  test("keeps an absent segment absent", () => {
    expect(parseSurvey({ ...wireSurvey(), segment: null }).segment).toBeNull();
  });
});

describe("toEditorWorkspace", () => {
  test("drops the caller's access and revives the timestamps", () => {
    const workspace = toEditorWorkspace({
      id: "ws",
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-02T00:00:00Z",
      name: "Product",
      organizationId: "org",
      languages: [
        { id: "l", createdAt: "2026-01-03T00:00:00Z", updatedAt: "2026-01-04T00:00:00Z", code: "de" },
      ],
      access: { role: "owner", canWrite: true, canManage: true, isReadOnly: false },
    } as unknown as WorkspaceDetail);
    expect(workspace).not.toHaveProperty("access");
    expect(workspace.createdAt).toEqual(new Date("2026-01-01T00:00:00Z"));
    expect(workspace.languages[0]?.updatedAt).toEqual(new Date("2026-01-04T00:00:00Z"));
  });
});

describe("saveSurvey", () => {
  test("sends a draft save to the draft endpoint with the expected version", async () => {
    const fetchMock = vi.fn().mockResolvedValue(json(200, wireSurvey()));
    vi.stubGlobal("fetch", fetchMock);
    const input = { ...parseSurvey(wireSurvey()), updatedAt: new Date("2026-10-02T09:30:00.123Z") };

    await saveSurvey(input, true);

    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(path).toBe("/api/app/surveys/0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e/draft");
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body as string)).toMatchObject({ updatedAt: "2026-10-02T09:30:00.123Z" });
  });

  test("sends a full save to the survey and surfaces the server's conflict marker", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      json(403, {
        code: "operation_not_allowed",
        message: "survey_modified_elsewhere:2026-10-03T00:00:00Z",
      })
    );
    vi.stubGlobal("fetch", fetchMock);

    const error = await saveSurvey({ ...parseSurvey(wireSurvey()) }, false).catch((e: unknown) => e);

    expect((fetchMock.mock.calls[0] as [string])[0]).toBe(
      "/api/app/surveys/0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e"
    );
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).message).toBe("survey_modified_elsewhere:2026-10-03T00:00:00Z");
  });
});

describe("responseCountsQuery", () => {
  test("pages the workspace's survey list, archived surveys included, until it finds the survey", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        json(200, {
          data: [{ id: "other", responseCount: 1, completedResponseCount: 1 }],
          meta: { nextCursor: "next" },
        })
      )
      .mockResolvedValueOnce(
        json(200, {
          data: [{ id: "survey", responseCount: 7, completedResponseCount: 4 }],
          meta: { nextCursor: null },
        })
      );
    vi.stubGlobal("fetch", fetchMock);

    const queryFn = responseCountsQuery("ws", "survey", "Feedback").queryFn as (context: {
      signal: AbortSignal;
    }) => Promise<unknown>;

    await expect(queryFn({ signal: new AbortController().signal })).resolves.toEqual({
      responseCount: 7,
      finishedResponseCount: 4,
    });
    const firstUrl = new URL((fetchMock.mock.calls[0] as [string])[0], "http://localhost");
    expect(firstUrl.searchParams.get("filter[name][contains]")).toBe("Feedback");
    expect(firstUrl.searchParams.get("filter[status][in]")).toBe(
      "draft,inProgress,paused,completed,archived"
    );
    expect(
      new URL((fetchMock.mock.calls[1] as [string])[0], "http://localhost").searchParams.get("cursor")
    ).toBe("next");
  });

  test("counts nothing for a survey the list does not show", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json(200, { data: [], meta: { nextCursor: null } })));
    const queryFn = responseCountsQuery("ws", "survey", "Feedback").queryFn as (context: {
      signal: AbortSignal;
    }) => Promise<unknown>;
    await expect(queryFn({ signal: new AbortController().signal })).resolves.toEqual({
      responseCount: 0,
      finishedResponseCount: 0,
    });
  });
});

describe("responseCountsQuery with a long name", () => {
  // The v3 list refuses a name filter over 512 characters with a 400, which the editor's loader rethrew: a survey with a longer name could not be opened at all.
  test("filters by the name cut to the 512 characters the list accepts", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      json(200, {
        data: [{ id: "survey", responseCount: 2, completedResponseCount: 1 }],
        meta: { nextCursor: null },
      })
    );
    vi.stubGlobal("fetch", fetchMock);
    const name = "😀".repeat(300) + "x".repeat(300);
    const queryFn = responseCountsQuery("ws", "survey", name).queryFn as (context: {
      signal: AbortSignal;
    }) => Promise<unknown>;
    await expect(queryFn({ signal: new AbortController().signal })).resolves.toEqual({
      responseCount: 2,
      finishedResponseCount: 1,
    });
    const filter = new URL((fetchMock.mock.calls[0] as [string])[0], "http://localhost").searchParams.get(
      "filter[name][contains]"
    );
    expect(Array.from(filter ?? "")).toHaveLength(512);
    expect(name.startsWith(filter ?? "-")).toBe(true);
  });
});

describe("forgetEditorSurvey", () => {
  test("drops only the editor's cached copy of that survey", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(surveyEditorKeys.survey("s1"), { id: "s1" });
    queryClient.setQueryData(surveyEditorKeys.survey("s2"), { id: "s2" });
    queryClient.setQueryData(surveyEditorKeys.quotas("s1"), []);
    forgetEditorSurvey(queryClient, "s1");
    expect(queryClient.getQueryData(surveyEditorKeys.survey("s1"))).toBeUndefined();
    expect(queryClient.getQueryData(surveyEditorKeys.survey("s2"))).toEqual({ id: "s2" });
    expect(queryClient.getQueryData(surveyEditorKeys.quotas("s1"))).toEqual([]);
  });
});
