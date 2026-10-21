import { describe, expect, test } from "vitest";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type {
  DateRange,
  SelectedFilterValue,
} from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/components/response-filter-context";
import {
  decodeResponseFilterParam,
  encodeResponseFilterParam,
  isDefaultResponseFilterState,
} from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/lib/response-filter-url";
import type { OptionsType } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/components/ElementsComboBox";

// The enum lives in a client component; a node-environment spec takes the values type-only.
const OPTION_TYPE_ELEMENTS = "Elements" as OptionsType;
const OPTION_TYPE_TAGS = "Tags" as OptionsType;

const defaultFilter: SelectedFilterValue = { filter: [], responseStatus: "all" };
const defaultDateRange: DateRange = { from: undefined, to: new Date("2026-09-22T23:59:59.999Z") };

const choiceFilter: SelectedFilterValue = {
  filter: [
    {
      elementType: {
        id: "kx7m2p4q8r1s5t9v3w6y0z2a",
        label: "Which plan are you on?",
        type: OPTION_TYPE_ELEMENTS,
        elementType: TSurveyElementTypeEnum.MultipleChoiceMulti,
      },
      filterType: { filterValue: "Includes either", filterComboBoxValue: ["Free", "Pro"] },
    },
  ],
  responseStatus: "complete",
};

const januaryRange: DateRange = {
  from: new Date("2026-01-01T00:00:00.000Z"),
  to: new Date("2026-01-31T23:59:59.999Z"),
  preset: "last 30 days",
};

describe("isDefaultResponseFilterState", () => {
  test("a date range that only bounds the end is still the default, not an active filter", () => {
    expect(isDefaultResponseFilterState(defaultFilter, defaultDateRange)).toBe(true);
  });

  test("any narrowing makes it non-default", () => {
    expect(
      isDefaultResponseFilterState({ ...defaultFilter, responseStatus: "partial" }, defaultDateRange)
    ).toBe(false);
    expect(isDefaultResponseFilterState(defaultFilter, januaryRange)).toBe(false);
    expect(isDefaultResponseFilterState(choiceFilter, defaultDateRange)).toBe(false);
  });
});

describe("encodeResponseFilterParam", () => {
  test("the default state writes no param, so a bare URL stays bare", () => {
    expect(encodeResponseFilterParam(defaultFilter, defaultDateRange)).toBeNull();
  });

  test("a date range with no start is not serialised", () => {
    expect(encodeResponseFilterParam(defaultFilter, { from: undefined, to: new Date() })).toBeNull();
  });
});

describe("round trip", () => {
  test("filter entries, response status, bounds and preset all survive", () => {
    const encoded = encodeResponseFilterParam(choiceFilter, januaryRange);
    expect(encoded).not.toBeNull();

    expect(decodeResponseFilterParam(encoded as string)).toEqual({
      selectedFilter: choiceFilter,
      dateRange: januaryRange,
    });
  });

  test("a single-value combo box value keeps its scalar shape", () => {
    const tagFilter: SelectedFilterValue = {
      filter: [
        {
          elementType: { label: "churn-risk", type: OPTION_TYPE_TAGS },
          filterType: { filterValue: "Applied", filterComboBoxValue: "Applied" },
        },
      ],
      responseStatus: "all",
    };

    const encoded = encodeResponseFilterParam(tagFilter, defaultDateRange);
    expect(decodeResponseFilterParam(encoded as string)).toEqual({ selectedFilter: tagFilter });
  });
});

describe("decodeResponseFilterParam", () => {
  test("unreadable values leave the view unfiltered rather than half-filtered", () => {
    expect(decodeResponseFilterParam("not json")).toBeNull();
    expect(decodeResponseFilterParam("[]")).toBeNull();
    expect(decodeResponseFilterParam(JSON.stringify({ f: "not an array" }))).toBeNull();
    expect(decodeResponseFilterParam(JSON.stringify({ s: "finished" }))).toBeNull();
  });

  test("a date range with an unparseable bound is dropped, the filters are kept", () => {
    const decoded = decodeResponseFilterParam(
      JSON.stringify({ s: "partial", d: { f: "yesterday-ish", t: "2026-01-31T23:59:59.999Z" } })
    );

    expect(decoded).toEqual({ selectedFilter: { filter: [], responseStatus: "partial" } });
  });

  test("an unknown preset is dropped but the explicit bounds still stand", () => {
    const decoded = decodeResponseFilterParam(
      JSON.stringify({
        d: { f: "2026-01-01T00:00:00.000Z", t: "2026-01-31T23:59:59.999Z", p: "since the dawn of time" },
      })
    );

    expect(decoded?.dateRange).toEqual({
      from: new Date("2026-01-01T00:00:00.000Z"),
      to: new Date("2026-01-31T23:59:59.999Z"),
    });
  });

  test("a preset is normalised to its canonical key, so a hand-edited link still labels the range", () => {
    const decoded = decodeResponseFilterParam(
      JSON.stringify({
        d: { f: "2026-01-01T00:00:00.000Z", t: "2026-01-31T23:59:59.999Z", p: " Last 7 Days " },
      })
    );

    expect(decoded?.dateRange?.preset).toBe("last 7 days");
  });

  test("a link whose question was deleted decodes to a filter on an id the survey no longer has", () => {
    // The hydrator cannot see the survey, so an id that no longer exists is carried through rather
    // than dropped: `getFormattedFilters` looks the element up and the query simply matches nothing.
    const decoded = decodeResponseFilterParam(
      JSON.stringify({ f: [{ i: "deleted-element-id", t: "Elements", v: "Skipped" }] })
    );

    expect(decoded?.selectedFilter.filter).toHaveLength(1);
    expect(decoded?.selectedFilter.filter[0].elementType.id).toBe("deleted-element-id");
  });
});
