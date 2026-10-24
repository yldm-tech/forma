import { describe, expect, test } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { describeFirstIssue, getInvalidCards, issuesFromDetails } from "./survey-issues";

const element = (id: string) => ({
  id,
  type: TSurveyElementTypeEnum.OpenText,
  headline: { default: id },
  required: false,
  inputType: "text" as const,
  charLimit: { enabled: false },
});

const survey = {
  blocks: [
    {
      id: "blockone0000000000000000",
      name: "Block 1",
      elements: [element("q1"), element("q2")],
      logic: [
        { id: "logic-a", conditions: { id: "g", connector: "and", conditions: [] }, actions: [] },
        { id: "logic-b", conditions: { id: "g", connector: "and", conditions: [] }, actions: [] },
      ],
    },
    { id: "blocktwo0000000000000000", name: "", elements: [element("q3")] },
  ],
  endings: [{ id: "ending1", type: "endScreen" }],
} as unknown as TSurvey;

describe("getInvalidCards", () => {
  test("flags the element an element issue points at and scrolls to it", () => {
    expect(
      getInvalidCards(survey, [{ path: ["blocks", 0, "elements", 1, "headline"], message: "x" }])
    ).toEqual({
      invalidIds: ["q2"],
      firstInvalidScrollId: "q2",
    });
  });

  test("flags a block whose name is missing", () => {
    expect(getInvalidCards(survey, [{ path: ["blocks", 1, "name"], message: "x" }])).toEqual({
      invalidIds: ["blocktwo0000000000000000"],
      firstInvalidScrollId: "blocktwo0000000000000000",
    });
  });

  test("flags the logic rule of a rule issue, scrolling to its block", () => {
    expect(
      getInvalidCards(survey, [{ path: ["blocks", 0, "logic", 1, "conditions"], message: "x" }])
    ).toEqual({
      invalidIds: ["logic-b"],
      firstInvalidScrollId: "blockone0000000000000000",
    });
  });

  test("flags every rule of the block for a block-wide logic issue such as a cycle", () => {
    expect(getInvalidCards(survey, [{ path: ["blocks", 0, "logic"], message: "x" }]).invalidIds).toEqual([
      "logic-a",
      "logic-b",
    ]);
  });

  test("maps the welcome card to start and an ending to its id", () => {
    expect(
      getInvalidCards(survey, [
        { path: ["welcomeCard", "headline"], message: "x" },
        { path: ["endings", 0, "buttonLink"], message: "y" },
      ])
    ).toEqual({ invalidIds: ["start", "ending1"], firstInvalidScrollId: "start" });
  });

  test("lists each card once and keeps the first issue's card as the scroll target", () => {
    expect(
      getInvalidCards(survey, [
        { path: ["blocks", 0, "elements", 0, "headline"], message: "x" },
        { path: ["blocks", 0, "elements", 0, "subheader"], message: "y" },
        { path: ["blocks", 0, "elements", 1, "headline"], message: "z" },
      ])
    ).toEqual({ invalidIds: ["q1", "q2"], firstInvalidScrollId: "q1" });
  });

  test("ignores issues outside the cards and indexes that point nowhere", () => {
    expect(
      getInvalidCards(survey, [
        { path: ["name"], message: "x" },
        { path: ["blocks", 7, "elements", 0, "headline"], message: "y" },
        { path: ["endings", 3], message: "z" },
      ])
    ).toEqual({ invalidIds: [], firstInvalidScrollId: null });
  });
});

describe("describeFirstIssue", () => {
  const blockNameRequired = (n: number) => `Block name is required for Block ${n}`;
  const languageLabel = (code: string) => ({ de: "German", fr: "French" })[code] ?? code;

  test("names the block a missing block name belongs to", () => {
    expect(
      describeFirstIssue(
        { path: ["blocks", 2, "name"], message: "Block name is required" },
        blockNameRequired,
        languageLabel
      )
    ).toEqual({ message: "Block name is required for Block 3", showLanguageTab: false });
  });

  test("lists the languages of a translation issue by name and sends the author to the Language tab", () => {
    expect(
      describeFirstIssue(
        {
          path: ["blocks", 0, "elements", 0, "headline"],
          message:
            "The question in question 1 of block 1 is missing for the following languages:  -fLang- de,fr",
          params: { invalidLanguageCodes: ["de", "fr"] },
        },
        blockNameRequired,
        languageLabel
      )
    ).toEqual({
      message:
        "The question in question 1 of block 1 is missing for the following languages:   German, French",
      showLanguageTab: true,
    });
  });

  test("passes any other issue through", () => {
    expect(
      describeFirstIssue(
        { path: ["autoComplete"], message: "Response limit must be greater than 0" },
        blockNameRequired,
        languageLabel
      )
    ).toEqual({ message: "Response limit must be greater than 0", showLanguageTab: false });
  });
});

describe("issuesFromDetails", () => {
  test("reads the issues of a server validation error", () => {
    const issues = [{ path: ["blocks", 0, "name"], message: "Block name is required" }];
    expect(issuesFromDetails({ issues })).toEqual(issues);
  });

  test("returns none for other error details or malformed entries", () => {
    expect(issuesFromDetails(undefined)).toEqual([]);
    expect(issuesFromDetails({ fields: [] })).toEqual([]);
    expect(issuesFromDetails({ issues: [{ message: "no path" }, null, "text"] })).toEqual([]);
  });
});
