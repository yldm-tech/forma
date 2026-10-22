import { describe, expect, test } from "vite-plus/test";
import {
  hasActiveFilters,
  initialFilters,
  normalizeSurveyFilters,
  parseStoredSurveyFilters,
} from "./filters";

// forma2's modules/survey/list/lib/utils.test.ts.
describe("normalizeSurveyFilters", () => {
  test("returns the default filters when there is no input", () => {
    expect(normalizeSurveyFilters(undefined)).toEqual({
      name: "",
      status: [],
      type: [],
      sortBy: "relevance",
    });
    expect(normalizeSurveyFilters(null)).toEqual(initialFilters);
  });

  test("trims the name, drops unknown fields and values, and de-duplicates and sorts the lists", () => {
    expect(
      normalizeSurveyFilters({
        name: "  Customer feedback  ",
        createdBy: ["you"],
        status: ["paused", "draft", "paused", "scheduled"],
        type: ["link", "app", "link", "website"],
        sortBy: "name",
      } as never)
    ).toEqual({
      name: "Customer feedback",
      status: ["draft", "paused"],
      type: ["app", "link"],
      sortBy: "name",
    });
  });

  test("keeps the archived pseudo-status", () => {
    expect(normalizeSurveyFilters({ status: ["archived", "inProgress"] }).status).toEqual([
      "archived",
      "inProgress",
    ]);
  });

  test("drops the type filter in a link-only workspace, whose toolbar has no type control", () => {
    expect(
      normalizeSurveyFilters({ name: "", status: [], type: ["app", "link"], sortBy: "updatedAt" }, "link")
    ).toEqual({ name: "", status: [], type: [], sortBy: "updatedAt" });
    expect(normalizeSurveyFilters({ type: ["app"] }, "website").type).toEqual(["app"]);
  });

  test("falls back to relevance for an unknown sort order", () => {
    expect(normalizeSurveyFilters({ sortBy: "popularity" } as never).sortBy).toBe("relevance");
  });

  test("ignores values of the wrong shape", () => {
    expect(normalizeSurveyFilters({ name: 3, status: "draft", type: null } as never)).toEqual(initialFilters);
  });
});

describe("parseStoredSurveyFilters", () => {
  test("is null when nothing is stored or the value is not JSON", () => {
    expect(parseStoredSurveyFilters(null)).toBeNull();
    expect(parseStoredSurveyFilters("")).toBeNull();
    expect(parseStoredSurveyFilters("{")).toBeNull();
  });

  test("sanitizes legacy stored filters", () => {
    expect(
      parseStoredSurveyFilters(
        JSON.stringify({
          name: "  NPS  ",
          createdBy: ["you"],
          status: ["completed", "draft"],
          type: ["link"],
          sortBy: "createdAt",
        })
      )
    ).toEqual({ name: "NPS", status: ["completed", "draft"], type: ["link"], sortBy: "createdAt" });
  });

  test("applies the workspace channel", () => {
    expect(parseStoredSurveyFilters(JSON.stringify({ type: ["app"] }), "link")?.type).toEqual([]);
  });
});

describe("hasActiveFilters", () => {
  test("the defaults and a sort order alone are not active filters", () => {
    expect(hasActiveFilters(initialFilters)).toBe(false);
    expect(hasActiveFilters({ ...initialFilters, sortBy: "name" })).toBe(false);
  });

  test("a name, a status or a type is an active filter", () => {
    expect(hasActiveFilters({ ...initialFilters, name: "x" })).toBe(true);
    expect(hasActiveFilters({ ...initialFilters, status: ["archived"] })).toBe(true);
    expect(hasActiveFilters({ ...initialFilters, type: ["app"] })).toBe(true);
  });
});
