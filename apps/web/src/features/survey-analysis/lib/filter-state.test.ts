import { describe, expect, test } from "vite-plus/test";
import { OptionsType, type SelectedFilterValue } from "./filter-types";
import { countAppliedFilters, hasActiveFilters, withSummaryFilterRow } from "./filter-state";

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
    expect(
      hasActiveFilters({ filter: [], responseStatus: "all" }, { from: new Date("2026-10-01"), to })
    ).toBe(true);
  });
});

describe("the keyword search", () => {
  const to = new Date("2026-10-24T00:00:00Z");

  test("filters the views without counting on the Filter trigger", () => {
    const searched: SelectedFilterValue = { filter: [], responseStatus: "all", keyword: "onboarding" };
    expect(countAppliedFilters(searched)).toBe(0);
    expect(hasActiveFilters(searched, { from: undefined, to })).toBe(true);
  });

  test("a blank keyword is no filter", () => {
    expect(
      hasActiveFilters({ filter: [], responseStatus: "all", keyword: "  " }, { from: undefined, to })
    ).toBe(false);
  });
});

describe("withSummaryFilterRow", () => {
  const ratingRow = (filterValue: string, value: string): SelectedFilterValue["filter"][number] => ({
    elementType: { id: "rating", label: "Rating", type: OptionsType.ELEMENTS },
    filterType: { filterValue, filterComboBoxValue: value },
  });
  const attributeNamedRating: SelectedFilterValue["filter"][number] = {
    elementType: { id: "rating", label: "rating", type: OptionsType.ATTRIBUTES, dataType: "number" },
    filterType: { filterValue: "Is greater than", filterComboBoxValue: "1" },
  };

  test("a click on an element without a row adds one", () => {
    expect(withSummaryFilterRow([row], ratingRow("Is equal to", "9"))).toEqual({
      rows: [row, ratingRow("Is equal to", "9")],
      replaced: false,
    });
  });

  test("a click replaces every row on the element, so a range beside the clicked value cannot leave nothing to match", () => {
    const rows = [ratingRow("Is more than", "2"), row, ratingRow("Is less than", "8"), attributeNamedRating];
    expect(withSummaryFilterRow(rows, ratingRow("Is equal to", "9"))).toEqual({
      rows: [ratingRow("Is equal to", "9"), row, attributeNamedRating],
      replaced: true,
    });
  });
});
