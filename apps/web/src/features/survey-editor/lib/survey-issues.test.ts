import { describe, expect, test } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import {
  type IssueMessages,
  describeFirstIssue,
  getInvalidCards,
  issuesFromDetails,
  logicRuleDomId,
} from "./survey-issues";

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
      fallbackScrollId: "blockone0000000000000000",
    });
  });

  test("flags a block whose name is missing", () => {
    expect(getInvalidCards(survey, [{ path: ["blocks", 1, "name"], message: "x" }])).toEqual({
      invalidIds: ["blocktwo0000000000000000"],
      firstInvalidScrollId: "blocktwo0000000000000000",
      fallbackScrollId: null,
    });
  });

  test("flags the logic rule of a rule issue, scrolling to the rule itself, or to its block while the block is collapsed", () => {
    expect(
      getInvalidCards(survey, [{ path: ["blocks", 0, "logic", 1, "conditions"], message: "x" }])
    ).toEqual({
      invalidIds: ["logic-b"],
      firstInvalidScrollId: logicRuleDomId("logic-b"),
      fallbackScrollId: "blockone0000000000000000",
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
    ).toEqual({ invalidIds: ["start", "ending1"], firstInvalidScrollId: "start", fallbackScrollId: null });
  });

  test("lists each card once and keeps the first issue's card as the scroll target", () => {
    expect(
      getInvalidCards(survey, [
        { path: ["blocks", 0, "elements", 0, "headline"], message: "x" },
        { path: ["blocks", 0, "elements", 0, "subheader"], message: "y" },
        { path: ["blocks", 0, "elements", 1, "headline"], message: "z" },
      ])
    ).toEqual({
      invalidIds: ["q1", "q2"],
      firstInvalidScrollId: "q1",
      fallbackScrollId: "blockone0000000000000000",
    });
  });

  test("ignores issues outside the cards and indexes that point nowhere", () => {
    expect(
      getInvalidCards(survey, [
        { path: ["name"], message: "x" },
        { path: ["blocks", 7, "elements", 0, "headline"], message: "y" },
        { path: ["endings", 3], message: "z" },
      ])
    ).toEqual({ invalidIds: [], firstInvalidScrollId: null, fallbackScrollId: null });
  });
});

describe("describeFirstIssue", () => {
  const messages: IssueMessages = {
    surveyNameRequired: () => "Please enter a survey name.",
    blockNameRequired: (n) => `Block name is required for Block ${n}`,
    questionTextMissing: (block, question) => `Question ${question} in block ${block} needs a question text`,
    optionLabelMissing: (block, question, option) =>
      `Option ${option} of question ${question} in block ${block} is empty`,
    logicRuleIncomplete: (block, rule) => `Rule ${rule} in block ${block} is incomplete`,
    languageLabel: (code) => ({ de: "German", fr: "French" })[code] ?? code,
  };
  const choiceSurvey = {
    blocks: [
      {
        id: "blockone0000000000000000",
        name: "Block 1",
        elements: [
          { ...element("q1"), headline: { default: "<p></p>" } },
          {
            id: "q2",
            type: TSurveyElementTypeEnum.MultipleChoiceSingle,
            headline: { default: "Pick one" },
            required: false,
            choices: [
              { id: "a", label: { default: "Yes" } },
              { id: "b", label: { default: " " } },
            ],
          },
        ],
      },
    ],
    endings: [],
  } as unknown as TSurvey;

  test("names the block a missing block name belongs to", () => {
    expect(
      describeFirstIssue(survey, { path: ["blocks", 2, "name"], message: "Block name is required" }, messages)
    ).toEqual({ message: "Block name is required for Block 3", showLanguageTab: false });
  });

  test("words a missing survey name in the editor's language", () => {
    expect(
      describeFirstIssue(survey, { path: ["name"], message: "Survey name is required" }, messages)
    ).toEqual({ message: "Please enter a survey name.", showLanguageTab: false });
  });

  test("words an empty question text and an empty option by their position", () => {
    expect(
      describeFirstIssue(
        choiceSurvey,
        {
          path: ["blocks", 0, "elements", 0, "headline"],
          message: "The question in question 1 of block 1 is missing",
        },
        messages
      ).message
    ).toBe("Question 1 in block 1 needs a question text");
    expect(
      describeFirstIssue(
        choiceSurvey,
        {
          path: ["blocks", 0, "elements", 1, "choices", 1],
          message: "Choice 2 in question 2 of block 1 is missing",
        },
        messages
      ).message
    ).toBe("Option 2 of question 2 in block 1 is empty");
  });

  test("keeps the raw message when the text the path names is not empty", () => {
    const message = "The headline in question 1 of block 1 is not present for the following languages: de";
    expect(
      describeFirstIssue(survey, { path: ["blocks", 0, "elements", 0, "headline"], message }, messages)
        .message
    ).toBe(message);
  });

  describe("a logic rule issue", () => {
    const condition = (rightOperand?: unknown) => ({
      id: "c1",
      leftOperand: { type: "element", value: "q2" },
      operator: "equals",
      ...(rightOperand === undefined ? {} : { rightOperand }),
    });
    const ruleSurvey = (conditions: unknown[], actions: unknown[]) =>
      ({
        blocks: [
          {
            ...choiceSurvey.blocks[0],
            logic: [{ id: "rule1", conditions: { id: "g", connector: "and", conditions }, actions }],
          },
        ],
        endings: [],
      }) as unknown as TSurvey;
    const jump = (target: string) => ({ id: "a1", objective: "jumpToBlock", target });
    const describeRule = (survey: TSurvey, message: string) =>
      describeFirstIssue(survey, { path: ["blocks", 0, "logic", 0, "conditions"], message }, messages)
        .message;

    test("calls a rule with a blank left incomplete, by rule and block", () => {
      const missingValue =
        'Conditional Logic: Right operand should be a static value for "equals" in logic no: 1 of block 1';
      expect(describeRule(ruleSurvey([condition()], [jump("blocktwo0000000000000000")]), missingValue)).toBe(
        "Rule 1 in block 1 is incomplete"
      );
      expect(
        describeRule(
          ruleSurvey([condition({ type: "static", value: "" })], [jump("blocktwo0000000000000000")]),
          missingValue
        )
      ).toBe("Rule 1 in block 1 is incomplete");
      expect(
        describeRule(
          ruleSurvey(
            [{ id: "g2", connector: "or", conditions: [condition({ type: "static", value: [] })] }],
            []
          ),
          missingValue
        )
      ).toBe("Rule 1 in block 1 is incomplete");
      expect(
        describeRule(
          ruleSurvey([condition({ type: "static", value: "a" })], [jump("")]),
          "Conditional Logic: Target block id cannot be empty"
        )
      ).toBe("Rule 1 in block 1 is incomplete");
    });

    test("keeps the reason of a complete rule that is refused", () => {
      const complete = ruleSurvey(
        [condition({ type: "static", value: "gone" }), { ...condition(), id: "c2", operator: "isSubmitted" }],
        [
          jump("blocktwo0000000000000000"),
          {
            id: "a2",
            objective: "calculate",
            variableId: "v",
            operator: "add",
            value: { type: "static", value: 0 },
          },
        ]
      );
      const reason = 'Conditional Logic: Choice "gone" does not exist in element 2 of block 1';
      expect(describeRule(complete, reason)).toBe(reason);
    });
  });

  test("lists the languages of a translation issue by name and sends the author to the Language tab", () => {
    expect(
      describeFirstIssue(
        survey,
        {
          path: ["blocks", 0, "elements", 0, "headline"],
          message:
            "The question in question 1 of block 1 is missing for the following languages:  -fLang- de,fr",
          params: { invalidLanguageCodes: ["de", "fr"] },
        },
        messages
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
        survey,
        { path: ["autoComplete"], message: "Response limit must be greater than 0" },
        messages
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
