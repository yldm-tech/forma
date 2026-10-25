import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { type TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { type DateRange, OptionsType, type SelectedFilterValue } from "../../lib/filter-types";
import { getFormattedFilters } from "../../lib/filters";
import { ResponseFilter } from "./response-filter";

// The filter the popover starts from, and what Apply hands back.
const context = vi.hoisted(() => ({
  selectedFilter: { filter: [], responseStatus: "all" } as unknown,
  setSelectedFilter: (() => {}) as (value: unknown) => void,
}));

vi.mock("./response-filter-context", () => ({
  useResponseFilter: () => ({
    selectedFilter: context.selectedFilter,
    setSelectedFilter: context.setSelectedFilter,
  }),
}));

vi.mock("../../api", () => ({
  surveyFilterDataQuery: (surveyId: string) => ({
    queryKey: ["filter-data", surveyId],
    queryFn: async () => ({
      environmentTags: [],
      attributes: {},
      meta: {},
      hiddenFields: {},
      reservedValues: {},
      variableValues: {},
      quotas: [],
    }),
  }),
}));

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  Element.prototype.scrollIntoView ??= () => {};
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const label = (text: string) => ({ default: text });

const scoreElements = [
  { id: "nps", type: TSurveyElementTypeEnum.NPS, headline: label("NPS"), required: false },
  {
    id: "rating",
    type: TSurveyElementTypeEnum.Rating,
    headline: label("Rating"),
    required: false,
    scale: "number",
    range: 5,
  },
  {
    id: "csat",
    type: TSurveyElementTypeEnum.CSAT,
    headline: label("CSAT"),
    required: false,
    scale: "smiley",
    range: 5,
  },
  {
    id: "ces",
    type: TSurveyElementTypeEnum.CES,
    headline: label("CES"),
    required: false,
    scale: "number",
    range: 7,
  },
  {
    id: "rating2",
    type: TSurveyElementTypeEnum.Rating,
    headline: label("Rating 2"),
    required: false,
    scale: "number",
    range: 5,
  },
] as unknown as TSurveyElement[];

const survey = {
  id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e",
  workspaceId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f",
  name: "Scores",
  status: "inProgress",
  blocks: [{ id: "block1", name: "Block 1", elements: scoreElements }],
  hiddenFields: { enabled: false, fieldIds: [] },
  variables: [],
  languages: [],
} as unknown as TSurvey;

const scoreRow = (id: string, elementType: TSurveyElementTypeEnum, filterValue: string) => ({
  elementType: { type: OptionsType.ELEMENTS, label: id, id, elementType },
  filterType: { filterValue, filterComboBoxValue: undefined },
});

/** Opens the popover on the given filter, clicks Apply, and returns what was applied. */
const applyFilter = (filter: SelectedFilterValue): SelectedFilterValue => {
  const setSelectedFilter = vi.fn();
  context.selectedFilter = filter;
  context.setSelectedFilter = setSelectedFilter;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nextProvider i18n={i18n}>
        <ResponseFilter survey={survey} />
      </I18nextProvider>
    </QueryClientProvider>
  );
  fireEvent.click(screen.getByRole("button", { name: new RegExp(i18n.t("common.filter")) }));
  fireEvent.click(screen.getByRole("button", { name: i18n.t("common.apply_filters") }));
  expect(setSelectedFilter).toHaveBeenCalledTimes(1);
  return setSelectedFilter.mock.calls[0]?.[0] as SelectedFilterValue;
};

// Submitted and Skipped on a score element take no value (the value box is disabled), so Apply must keep the row and send the operator alone.
describe("ResponseFilter Apply", () => {
  test("keeps a score element's Submitted and Skipped rows, which have no value", () => {
    const applied = applyFilter({
      responseStatus: "all",
      filter: [
        scoreRow("nps", TSurveyElementTypeEnum.NPS, "Submitted"),
        scoreRow("rating", TSurveyElementTypeEnum.Rating, "Skipped"),
        scoreRow("csat", TSurveyElementTypeEnum.CSAT, "Submitted"),
        scoreRow("ces", TSurveyElementTypeEnum.CES, "Skipped"),
      ],
    });
    expect(applied.filter.map((row) => row.elementType.id)).toEqual(["nps", "rating", "csat", "ces"]);
    expect(getFormattedFilters(survey, applied, {} as DateRange).data).toEqual({
      nps: { op: "submitted" },
      rating: { op: "skipped" },
      csat: { op: "submitted" },
      ces: { op: "skipped" },
    });
  });

  test("still drops a score row whose operator needs a value it does not have", () => {
    const applied = applyFilter({
      responseStatus: "all",
      filter: [
        scoreRow("csat", TSurveyElementTypeEnum.CSAT, "Submitted"),
        scoreRow("rating2", TSurveyElementTypeEnum.Rating, "Is equal to"),
      ],
    });
    expect(applied.filter.map((row) => row.elementType.id)).toEqual(["csat"]);
  });
});
