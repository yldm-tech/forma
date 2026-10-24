import { describe, expect, test } from "vite-plus/test";
import { OptionsType, type SelectedFilterValue } from "./filter-types";
import { countAppliedFilters, hasActiveFilters } from "./filter-state";

const row: SelectedFilterValue["filter"][number] = {
  elementType: { id: "q1", label: "Plan", type: OptionsType.ELEMENTS },
  filterType: { filterValue: "Includes either", filterComboBoxValue: ["Pro"] },
};

describe("countAppliedFilters", () => {
  test("counts the rows and a response status other than all", () => {
    expect(countAppliedFilters({ filter: [], responseStatus: "all" })).toBe(0);
    expect(countAppliedFilters({ filter: [row], responseStatus: "all" })).toBe(1);
    expect(countAppliedFilters({ filter: [row], responseStatus: "complete" })).toBe(2);
  });
});

describe("hasActiveFilters", () => {
  const to = new Date("2026-10-24T00:00:00Z");

  test("is false for the default filter and an open date range", () => {
    expect(hasActiveFilters({ filter: [], responseStatus: "all" }, { from: undefined, to })).toBe(false);
  });

  test("is true for a condition, a status or a date range start", () => {
    expect(hasActiveFilters({ filter: [row], responseStatus: "all" }, { from: undefined, to })).toBe(true);
    expect(hasActiveFilters({ filter: [], responseStatus: "partial" }, { from: undefined, to })).toBe(true);
    expect(hasActiveFilters({ filter: [], responseStatus: "all" }, { from: new Date("2026-10-01"), to })).toBe(
      true
    );
  });
});
