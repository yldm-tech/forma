import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { type TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type {
  TSurvey,
  TSurveyElementSummaryCes,
  TSurveyElementSummaryConsent,
  TSurveyElementSummaryCsat,
  TSurveyElementSummaryMultipleChoice,
  TSurveyElementSummaryNps,
  TSurveyElementSummaryPictureSelection,
  TSurveyElementSummaryRating,
} from "@forma/types/surveys/types";
import type { i18n as I18n } from "i18next";
import type { ReactElement } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { OptionsType, type SelectedFilterValue } from "../../lib/filter-types";
import { getFormattedFilters } from "../../lib/filters";
import { MultipleChoiceSummary, PictureChoiceSummary } from "./choice-summaries";
import { CESSummary, CSATSummary, ConsentSummary, NPSSummary, RatingSummary } from "./score-summaries";
import { RatingScaleLegend, type SetSummaryFilter } from "./summary-parts";

// A summary click puts its operator and value into the filter; under a non-English locale they must still be the wire values getFormattedFilters reads, or the click filters by nothing (insights-08).

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("ja-JP");
});

afterEach(cleanup);

const label = (text: string) => ({ default: text });

const elements = {
  nps: {
    id: "nps",
    type: TSurveyElementTypeEnum.NPS,
    headline: label("NPS"),
    required: false,
    lowerLabel: label("Low"),
    upperLabel: label("High"),
    isColorCodingEnabled: false,
  },
  rating: {
    id: "rating",
    type: TSurveyElementTypeEnum.Rating,
    headline: label("Rating"),
    required: false,
    scale: "number",
    range: 10,
    isColorCodingEnabled: false,
  },
  csat: {
    id: "csat",
    type: TSurveyElementTypeEnum.CSAT,
    headline: label("CSAT"),
    required: false,
    scale: "smiley",
    range: 5,
    isColorCodingEnabled: false,
  },
  ces: {
    id: "ces",
    type: TSurveyElementTypeEnum.CES,
    headline: label("CES"),
    required: false,
    scale: "number",
    range: 7,
    isColorCodingEnabled: false,
  },
  consent: {
    id: "consent",
    type: TSurveyElementTypeEnum.Consent,
    headline: label("Consent"),
    required: false,
    label: label("I agree"),
  },
  single: {
    id: "single",
    type: TSurveyElementTypeEnum.MultipleChoiceSingle,
    headline: label("Single"),
    required: false,
    choices: [
      { id: "a", label: label("Alpha") },
      { id: "b", label: label("Beta") },
    ],
    shuffleOption: "none",
  },
  multi: {
    id: "multi",
    type: TSurveyElementTypeEnum.MultipleChoiceMulti,
    headline: label("Multi"),
    required: false,
    choices: [
      { id: "a", label: label("Alpha") },
      { id: "b", label: label("Beta") },
    ],
    shuffleOption: "none",
  },
  picture: {
    id: "picture",
    type: TSurveyElementTypeEnum.PictureSelection,
    headline: label("Picture"),
    required: false,
    allowMulti: true,
    choices: [
      { id: "p1", imageUrl: "https://example.com/1.png" },
      { id: "p2", imageUrl: "https://example.com/2.png" },
    ],
  },
} as const;

const survey = {
  id: "survey1",
  workspaceId: "workspace1",
  name: "Clicks",
  type: "link",
  status: "inProgress",
  blocks: [
    { id: "block1", name: "Block 1", elements: Object.values(elements) as unknown as TSurveyElement[] },
  ],
  hiddenFields: { enabled: false, fieldIds: [] },
  variables: [],
  languages: [],
  welcomeCard: { enabled: false },
  endings: [],
} as unknown as TSurvey;

const ratingChoices = (range: number) =>
  Array.from({ length: range }, (_, i) => ({ rating: i + 1, count: 1, percentage: 100 / range }));

const scoreSummary = (element: (typeof elements)["rating" | "csat" | "ces"], range: number) => ({
  type: element.type,
  element,
  responseCount: range,
  average: 3,
  choices: ratingChoices(range),
  dismissed: { count: 1 },
  csat: { satisfiedCount: 2, satisfiedPercentage: 40 },
});

/** Renders a card in Japanese, clicks every control but the view tabs and the id badges' copy buttons (the only labelled ones), and returns what each click filtered by. */
const clicksOf = (renderCard: (setFilter: SetSummaryFilter) => ReactElement) => {
  const setFilter = vi.fn<SetSummaryFilter>();
  const { container } = render(<I18nextProvider i18n={i18n}>{renderCard(setFilter)}</I18nextProvider>);
  const controls = container.querySelectorAll<HTMLElement>(
    'button:not([role="tab"]):not([aria-label]), [role="button"]'
  );
  for (const control of controls) fireEvent.click(control);
  return setFilter.mock.calls;
};

/** The criteria one summary click produces, as the responses query would receive them. */
const criteriaOf = ([
  elementId,
  ,
  elementType,
  filterValue,
  filterComboBoxValue,
]: Parameters<SetSummaryFilter>) => {
  const selected: SelectedFilterValue = {
    filter: [
      {
        elementType: { id: elementId, label: elementId, elementType, type: OptionsType.ELEMENTS },
        filterType: { filterValue, filterComboBoxValue },
      },
    ],
    responseStatus: "all",
  };
  return getFormattedFilters(survey, selected, { from: undefined }).data?.[elementId];
};

describe("summary clicks under ja-JP", () => {
  test("every NPS group filters", () => {
    const summary = {
      type: TSurveyElementTypeEnum.NPS,
      element: elements.nps,
      responseCount: 3,
      total: 4,
      answered: 3,
      score: 33.33,
      promoters: { count: 2, percentage: 66.67 },
      passives: { count: 0, percentage: 0 },
      detractors: { count: 1, percentage: 33.33 },
      dismissed: { count: 1, percentage: 25 },
      choices: Array.from({ length: 11 }, (_, rating) => ({ rating, count: 0, percentage: 0 })),
    } as unknown as TSurveyElementSummaryNps;

    const calls = clicksOf((setFilter) => (
      <NPSSummary elementSummary={summary} survey={survey} setFilter={setFilter} />
    ));
    expect(calls.map((call) => criteriaOf(call))).toEqual([
      { op: "includesOne", value: [9, 10] },
      { op: "includesOne", value: [7, 8] },
      { op: "lessThan", value: 7 },
      { op: "skipped" },
    ]);
  });

  test("every bar of the NPS 0 to 10 view filters by its rating", () => {
    const summary = {
      type: TSurveyElementTypeEnum.NPS,
      element: elements.nps,
      responseCount: 11,
      total: 11,
      answered: 11,
      score: 0,
      promoters: { count: 2, percentage: 18 },
      passives: { count: 2, percentage: 18 },
      detractors: { count: 7, percentage: 64 },
      dismissed: { count: 0, percentage: 0 },
      choices: Array.from({ length: 11 }, (_, rating) => ({ rating, count: 1, percentage: 9 })),
    } as unknown as TSurveyElementSummaryNps;
    const setFilter = vi.fn<SetSummaryFilter>();
    render(
      <I18nextProvider i18n={i18n}>
        <NPSSummary elementSummary={summary} survey={survey} setFilter={setFilter} />
      </I18nextProvider>
    );
    fireEvent.click(screen.getByRole("tab", { name: i18n.t("workspace.surveys.summary.individual") }));
    fireEvent.click(screen.getByText("10", { selector: "div" }));
    expect(setFilter.mock.calls.map((call) => criteriaOf(call))).toEqual([{ op: "equals", value: 10 }]);
  });

  test.each([
    ["rating", RatingSummary, 10],
    ["csat", CSATSummary, 5],
    ["ces", CESSummary, 7],
  ] as const)("every %s bar filters by its rating", (key, Card, range) => {
    const summary = scoreSummary(elements[key], range);
    const calls = clicksOf((setFilter) => (
      <Card
        elementSummary={
          summary as unknown as TSurveyElementSummaryRating &
            TSurveyElementSummaryCsat &
            TSurveyElementSummaryCes
        }
        survey={survey}
        setFilter={setFilter}
      />
    ));
    expect(calls.map((call) => criteriaOf(call))).toEqual(
      Array.from({ length: range }, (_, i) => ({ op: "equals", value: i + 1 }))
    );
  });

  test("consent's accepted and dismissed rows filter", () => {
    const summary = {
      type: TSurveyElementTypeEnum.Consent,
      element: elements.consent,
      responseCount: 2,
      accepted: { count: 1, percentage: 50 },
      dismissed: { count: 1, percentage: 50 },
    } as unknown as TSurveyElementSummaryConsent;
    const calls = clicksOf((setFilter) => (
      <ConsentSummary elementSummary={summary} survey={survey} setFilter={setFilter} />
    ));
    expect(calls.map((call) => criteriaOf(call))).toEqual([{ op: "accepted" }, { op: "skipped" }]);
  });

  test.each([
    ["single", "includesOne"],
    ["multi", "includesAll"],
  ] as const)("a %s choice row filters by the choice", (key, op) => {
    const summary = {
      type: elements[key].type,
      element: elements[key],
      responseCount: 2,
      selectionCount: 2,
      choices: [
        { value: "Alpha", count: 1, percentage: 50 },
        { value: "Beta", count: 1, percentage: 50 },
      ],
    } as unknown as TSurveyElementSummaryMultipleChoice;
    const calls = clicksOf((setFilter) => (
      <MultipleChoiceSummary elementSummary={summary} survey={survey} setFilter={setFilter} />
    ));
    expect(calls.map((call) => criteriaOf(call))).toEqual([
      { op, value: ["Alpha"] },
      { op, value: ["Beta"] },
    ]);
  });

  test("a picture row filters by the picture", () => {
    const summary = {
      type: TSurveyElementTypeEnum.PictureSelection,
      element: elements.picture,
      responseCount: 2,
      selectionCount: 2,
      choices: elements.picture.choices.map((choice) => ({
        id: choice.id,
        imageUrl: choice.imageUrl,
        count: 1,
        percentage: 50,
      })),
    } as unknown as TSurveyElementSummaryPictureSelection;
    const calls = clicksOf((setFilter) => (
      <PictureChoiceSummary elementSummary={summary} survey={survey} setFilter={setFilter} />
    ));
    expect(calls.map((call) => criteriaOf(call))).toEqual([
      { op: "includesAll", value: ["p1"] },
      { op: "includesAll", value: ["p2"] },
    ]);
  });
});

describe("RatingScaleLegend", () => {
  test("a number scale shows each end's number once", () => {
    render(<RatingScaleLegend scale="number" range={10} />);
    expect(screen.getAllByText("1")).toHaveLength(1);
    expect(screen.getAllByText("10")).toHaveLength(1);
  });

  test("a star scale keeps the number beside each end's icon", () => {
    render(<RatingScaleLegend scale="star" range={5} />);
    expect(screen.getByText("1")).toBeTruthy();
    expect(screen.getByText("5")).toBeTruthy();
  });
});
