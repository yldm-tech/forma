import type { InfiniteData } from "@tanstack/react-query";
import { describe, expect, test } from "vite-plus/test";
import type { SurveyListItem, SurveyListPage } from "../api";
import {
  flattenSurveyPages,
  removeSurveyFromInfiniteData,
  removedSurveyId,
  surveyKeys,
  updateSurveyInInfiniteData,
} from "./query";

// forma2's modules/survey/list/lib/query.test.ts.
const surveyA: SurveyListItem = {
  id: "survey_a",
  name: "Survey A",
  workspaceId: "ws_1",
  type: "link",
  status: "draft",
  publishOn: null,
  archivedAt: null,
  createdAt: "2026-04-15T10:00:00.000Z",
  updatedAt: "2026-04-15T10:00:00.000Z",
  responseCount: 0,
  completedResponseCount: 0,
  creator: { name: "Alice" },
};
const surveyB: SurveyListItem = { ...surveyA, id: "survey_b", name: "Survey B" };

const baseData: InfiniteData<SurveyListPage> = {
  pages: [
    { data: [surveyA], meta: { limit: 20, nextCursor: "cursor-1", totalCount: 2, workspaceSurveyCount: 4 } },
    // Cursor pages are asked for without counts, so both come back null.
    { data: [surveyB], meta: { limit: 20, nextCursor: null, totalCount: null, workspaceSurveyCount: null } },
  ],
  pageParams: [null, "cursor-1"],
};

describe("surveyKeys", () => {
  test("every list key starts with the lists key, so one invalidation reaches all of them", () => {
    const key = surveyKeys.list({
      workspaceId: "ws_1",
      limit: 12,
      filters: { name: "", status: [], type: [], sortBy: "relevance" },
    });
    expect(key.slice(0, 2)).toEqual([...surveyKeys.lists()]);
  });
});

describe("flattenSurveyPages", () => {
  test("flattens every fetched page", () => {
    expect(flattenSurveyPages(baseData)).toEqual([surveyA, surveyB]);
    expect(flattenSurveyPages(undefined)).toEqual([]);
  });
});

describe("removeSurveyFromInfiniteData", () => {
  test("drops the survey and lowers only the filtered total by default", () => {
    const next = removeSurveyFromInfiniteData(baseData, "survey_a");
    expect(next?.pages[0]?.data).toEqual([]);
    expect(next?.pages[1]?.data).toEqual([surveyB]);
    expect(next?.pages[0]?.meta.totalCount).toBe(1);
    // Archiving and restoring take a survey out of this view but leave it in the workspace.
    expect(next?.pages[0]?.meta.workspaceSurveyCount).toBe(4);
  });

  test("lowers the workspace count too when the survey leaves the workspace", () => {
    const next = removeSurveyFromInfiniteData(baseData, "survey_a", { removesFromWorkspace: true });
    expect(next?.pages[0]?.meta.totalCount).toBe(1);
    expect(next?.pages[0]?.meta.workspaceSurveyCount).toBe(3);
  });

  test("returns the original cache when the survey is not in it", () => {
    expect(removeSurveyFromInfiniteData(baseData, "missing")).toBe(baseData);
    expect(removeSurveyFromInfiniteData(undefined, "survey_a")).toBeUndefined();
  });

  test("leaves the null counts of later pages alone", () => {
    const next = removeSurveyFromInfiniteData(baseData, "survey_a", { removesFromWorkspace: true });
    expect(next?.pages[1]?.meta.totalCount).toBeNull();
    expect(next?.pages[1]?.meta.workspaceSurveyCount).toBeNull();
  });

  test("never counts below zero", () => {
    const empty: InfiniteData<SurveyListPage> = {
      pages: [
        { data: [surveyA], meta: { limit: 20, nextCursor: null, totalCount: 0, workspaceSurveyCount: 0 } },
      ],
      pageParams: [null],
    };
    const next = removeSurveyFromInfiniteData(empty, "survey_a", { removesFromWorkspace: true });
    expect(next?.pages[0]?.meta).toMatchObject({ totalCount: 0, workspaceSurveyCount: 0 });
  });
});

describe("updateSurveyInInfiniteData", () => {
  test("patches the matching survey and leaves the others untouched", () => {
    const next = updateSurveyInInfiniteData(baseData, "survey_b", { status: "inProgress", name: "B2" });
    expect(next?.pages[1]?.data[0]).toMatchObject({ status: "inProgress", name: "B2" });
    expect(next?.pages[0]?.data[0]).toEqual(surveyA);
  });

  test("returns the original reference when the survey is not in the cache", () => {
    expect(updateSurveyInInfiniteData(baseData, "missing", { status: "inProgress" })).toBe(baseData);
    expect(updateSurveyInInfiniteData(undefined, "survey_a", { status: "inProgress" })).toBeUndefined();
  });

  test("does not mutate the cached data", () => {
    updateSurveyInInfiniteData(baseData, "survey_a", { status: "completed" });
    expect(baseData.pages[0]?.data[0]?.status).toBe("draft");
  });
});

describe("removedSurveyId", () => {
  test("reads the survey id of a removal's variables", () => {
    expect(removedSurveyId({ surveyId: "s1" })).toBe("s1");
    expect(removedSurveyId({ surveyId: 1 })).toBeUndefined();
    expect(removedSurveyId(null)).toBeUndefined();
    expect(removedSurveyId("s1")).toBeUndefined();
  });
});
