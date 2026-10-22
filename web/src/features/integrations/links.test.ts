import type { TFunction } from "i18next";
import { describe, expect, test } from "vite-plus/test";
import type { LinkableSurvey, SheetLink } from "./api";
import {
  DEFAULT_LINK_SETTINGS,
  NOTION_MAPPING_ERRORS,
  constructGoogleSheetsUrl,
  elementSelection,
  emptyMappingRow,
  extractSpreadsheetIdFromUrl,
  headlineLabel,
  isValidGoogleSheetsUrl,
  linkBase,
  linkSettingsOf,
  mappingErrorOf,
  mappingRowsOf,
  notionElementItems,
  notionLink,
  notionLinkProblem,
  toggled,
  unusedBy,
  withLink,
  withoutLink,
} from "./links";

// A t that answers the key, so the tests read which message was chosen.
const t = ((key: string) => key) as unknown as TFunction;

const survey: LinkableSurvey = {
  id: "s1",
  name: "Feedback",
  elements: [
    { id: "q1", type: "openText", headline: { default: "<p>What is your name?</p>" } },
    { id: "q2", type: "rating", headline: { default: "How did #recall:q1/fallback:you# like it?" } },
    { id: "q3", type: "multipleChoiceSingle", headline: { default: "Plan", de: "Tarif" } },
  ],
  hiddenFields: ["utm_source"],
  variables: [{ storageKey: "score", name: "Score" }],
};

const NOW = new Date("2026-10-22T09:00:00.000Z");

describe("link builders", () => {
  test("a new link writes the creation time and nothing else extra", () => {
    expect(DEFAULT_LINK_SETTINGS).toEqual({
      includeCreatedAt: true,
      includeVariables: false,
      includeHiddenFields: false,
      includeMetadata: false,
      includeContactAttributes: false,
    });
  });

  test("a saved link's absent flags are off", () => {
    expect(linkSettingsOf({ createdAt: "", surveyId: "s1", surveyName: "", includeMetadata: true })).toEqual({
      includeCreatedAt: false,
      includeVariables: false,
      includeHiddenFields: false,
      includeMetadata: true,
      includeContactAttributes: false,
    });
  });

  test("a link names its survey and is stamped with the save time", () => {
    expect(linkBase(survey, DEFAULT_LINK_SETTINGS, NOW)).toEqual({
      createdAt: "2026-10-22T09:00:00.000Z",
      surveyId: "s1",
      surveyName: "Feedback",
      ...DEFAULT_LINK_SETTINGS,
    });
  });

  test("the selection reads All questions only when every element is chosen", () => {
    expect(elementSelection(t, survey, ["q1", "q2", "q3"])).toEqual({
      elementIds: ["q1", "q2", "q3"],
      elements: "common.all_questions",
    });
    expect(elementSelection(t, survey, ["q2"])).toEqual({
      elementIds: ["q2"],
      elements: "common.selected_questions",
    });
  });

  test("saving appends a new link and replaces an edited one, never touching the original list", () => {
    const first = { surveyId: "a" } as SheetLink;
    const second = { surveyId: "b" } as SheetLink;
    const edited = { surveyId: "c" } as SheetLink;
    const links = Object.freeze([first, second]);
    expect(withLink(links, edited)).toEqual([first, second, edited]);
    expect(withLink(links, edited, 0)).toEqual([edited, second]);
    expect(withoutLink(links, 0)).toEqual([second]);
    expect(links).toEqual([first, second]);
  });

  test("toggling keeps the order ids were picked in", () => {
    expect(toggled(["q2"], "q1")).toEqual(["q2", "q1"]);
    expect(toggled(["q2", "q1"], "q2")).toEqual(["q1"]);
  });
});

describe("headline labels", () => {
  test("show the default language as plain text", () => {
    expect(headlineLabel(survey, { default: "<p>What is your name?</p>" })).toBe("What is your name?");
    expect(headlineLabel(survey, { de: "Nur Deutsch" })).toBe("");
  });

  test("name a recalled element, hidden field and variable", () => {
    expect(headlineLabel(survey, survey.elements[1]!.headline)).toBe("How did @What is your name? like it?");
    expect(headlineLabel(survey, { default: "From #recall:utm_source/fallback:x#" })).toBe(
      "From @utm_source"
    );
    expect(headlineLabel(survey, { default: "Got #recall:score/fallback:0#" })).toBe("Got @Score");
    expect(headlineLabel(survey, { default: "Hi #recall:unknown/fallback:x#" })).toBe("Hi @unknown");
  });

  test("leave a partial recall token as text", () => {
    expect(headlineLabel(survey, { default: "Literal #recall: here" })).toBe("Literal #recall: here");
  });
});

describe("Google Sheets URLs", () => {
  const url =
    "https://docs.google.com/spreadsheets/d/1aBcDeFgHiJkLmNoPqRsTuVwXyZaBcDeFgHiJkLmNoPq/edit#gid=0";

  test("extract the spreadsheet id", () => {
    expect(extractSpreadsheetIdFromUrl(url)).toBe("1aBcDeFgHiJkLmNoPqRsTuVwXyZaBcDeFgHiJkLmNoPq");
    expect(() => extractSpreadsheetIdFromUrl("https://not-a-google-sheet-url.com")).toThrow(
      "Invalid Google Sheets URL"
    );
    expect(() => extractSpreadsheetIdFromUrl("https://docs.google.com/spreadsheets/d/")).toThrow(
      "Invalid Google Sheets URL"
    );
  });

  test("rebuild the URL an edited link shows", () => {
    expect(constructGoogleSheetsUrl("abc")).toBe("https://docs.google.com/spreadsheets/d/abc");
  });

  test("accept only spreadsheet URLs", () => {
    expect(isValidGoogleSheetsUrl(url)).toBe(true);
    expect(isValidGoogleSheetsUrl("https://docs.google.com/spreadsheets/d/")).toBe(true);
    expect(isValidGoogleSheetsUrl("https://not-a-google-sheet-url.com")).toBe(false);
  });
});

describe("Notion mappings", () => {
  const text = { id: "q1", name: "Name", type: "openText" };
  const rating = { id: "q2", name: "Rating", type: "rating" };
  const richText = { id: "p1", name: "Notes", type: "rich_text" };
  const number = { id: "p2", name: "Score", type: "number" };
  const rollup = { id: "p3", name: "Total", type: "rollup" };

  test("offer the elements, variables, hidden fields, metadata, creation time and contact attributes in that order", () => {
    expect(
      notionElementItems(t, survey, [
        { key: "email", name: "Email" },
        { key: "plan", name: null },
      ])
    ).toEqual([
      { id: "q1", name: "What is your name?", type: "openText" },
      { id: "q2", name: "How did @What is your name? like it?", type: "rating" },
      { id: "q3", name: "Plan", type: "multipleChoiceSingle" },
      { id: "score", name: "Score", type: "openText" },
      { id: "utm_source", name: "common.hidden_field : utm_source", type: "openText" },
      { id: "metadata", name: "common.metadata", type: "openText" },
      { id: "createdAt", name: "common.created_at", type: "date" },
      { id: "person.email", name: "common.person: Email", type: "openText" },
      { id: "person.plan", name: "common.person: plan", type: "openText" },
    ]);
  });

  test("flag a property Notion cannot write before a type mismatch", () => {
    expect(mappingErrorOf({ element: text, column: richText })).toBeNull();
    expect(mappingErrorOf({ element: rating, column: number })).toBeNull();
    expect(mappingErrorOf({ element: rating, column: richText })).toBe(NOTION_MAPPING_ERRORS.MAPPING);
    expect(mappingErrorOf({ element: rating, column: rollup })).toBe(NOTION_MAPPING_ERRORS.UNSUPPORTED_TYPE);
    expect(mappingErrorOf({ element: { id: "", name: "", type: "" }, column: rollup })).toBeNull();
  });

  test("let each element and each property be used by one row only", () => {
    const rows = [
      { ...emptyMappingRow(), element: text, column: richText },
      { ...emptyMappingRow(), element: rating, column: number },
    ];
    expect(unusedBy([text, rating], rows, 1, "element")).toEqual([rating]);
    expect(unusedBy([richText, number, rollup], rows, 0, "column")).toEqual([richText, rollup]);
  });

  test("check a link in forma2's order", () => {
    const empty = [emptyMappingRow()];
    const complete = [{ ...emptyMappingRow(), element: text, column: richText }];
    expect(notionLinkProblem(t, { hasDatabase: false, hasSurvey: false, rows: empty })).toBe(
      "workspace.integrations.notion.please_select_a_database"
    );
    expect(notionLinkProblem(t, { hasDatabase: true, hasSurvey: false, rows: empty })).toBe(
      "workspace.integrations.please_select_a_survey_error"
    );
    expect(notionLinkProblem(t, { hasDatabase: true, hasSurvey: true, rows: empty })).toBe(
      "workspace.integrations.notion.please_select_at_least_one_mapping"
    );
    expect(
      notionLinkProblem(t, {
        hasDatabase: true,
        hasSurvey: true,
        rows: [{ ...emptyMappingRow(), element: rating, column: richText }],
      })
    ).toBe("workspace.integrations.notion.please_resolve_mapping_errors");
    expect(
      notionLinkProblem(t, {
        hasDatabase: true,
        hasSurvey: true,
        rows: [...complete, { ...emptyMappingRow(), element: rating }],
      })
    ).toBe("workspace.integrations.notion.please_complete_mapping_fields_with_notion_property");
    expect(notionLinkProblem(t, { hasDatabase: true, hasSurvey: true, rows: complete })).toBeNull();
  });

  test("save the complete rows of the mapping, without the extra-column flags", () => {
    const rows = [...mappingRowsOf([{ element: text, column: richText }]), emptyMappingRow()];
    expect(notionLink(survey, { id: "db1", name: "Responses" }, rows, NOW)).toEqual({
      createdAt: "2026-10-22T09:00:00.000Z",
      surveyId: "s1",
      surveyName: "Feedback",
      mapping: [{ element: text, column: richText }],
      databaseId: "db1",
      databaseName: "Responses",
    });
  });
});
