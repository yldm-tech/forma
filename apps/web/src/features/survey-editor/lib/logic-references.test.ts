import { describe, expect, test } from "vite-plus/test";
import type { TSurveyQuota } from "@forma/types/quota";
import type { TSurveyBlock, TSurveyBlockLogic } from "@forma/types/surveys/blocks";
import { type TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import {
  findBlockUsedInLogic,
  findElementUsedInLogic,
  findEndingCardUsedInLogic,
  findHiddenFieldUsedInLogic,
  findOptionUsedInLogic,
  findVariableUsedInLogic,
  isUsedInQuota,
  isUsedInRecall,
} from "./utils";

// The checks the editor runs before deleting an element, a choice, an ending, a hidden field or a variable: each returns the index of the first element whose block's logic still refers to it, or -1.

const openText = (id: string, headline = id): TSurveyElement => ({
  id,
  type: TSurveyElementTypeEnum.OpenText,
  headline: { default: headline },
  required: false,
  inputType: "text",
  charLimit: { enabled: false },
});

const choice = (id: string): TSurveyElement => ({
  id,
  type: TSurveyElementTypeEnum.MultipleChoiceSingle,
  headline: { default: id },
  required: false,
  shuffleOption: "none",
  choices: [
    { id: "opt-a", label: { default: "A" } },
    { id: "opt-b", label: { default: "B" } },
  ],
});

const rule = (logic: Pick<TSurveyBlockLogic, "conditions" | "actions">): TSurveyBlockLogic => ({
  id: "rule",
  ...logic,
});

const condition = (left: { type: string; value: string }, right?: { type: string; value: unknown }) =>
  ({
    id: "group",
    connector: "and",
    conditions: [{ id: "c1", leftOperand: left, operator: "equals", rightOperand: right }],
  }) as TSurveyBlockLogic["conditions"];

const surveyWith = (blocks: TSurveyBlock[], extra: Partial<TSurvey> = {}): TSurvey =>
  ({
    blocks,
    endings: [{ id: "ending-1", type: "endScreen", headline: { default: "Thanks" } }],
    welcomeCard: { enabled: false },
    variables: [],
    hiddenFields: { enabled: true, fieldIds: ["source"] },
    ...extra,
  }) as unknown as TSurvey;

describe("findElementUsedInLogic", () => {
  test("finds a later block's condition on the element", () => {
    const survey = surveyWith([
      { id: "b1", name: "Block 1", elements: [openText("q1")] },
      {
        id: "b2",
        name: "Block 2",
        elements: [openText("q2")],
        logic: [
          rule({
            conditions: condition({ type: "element", value: "q1" }, { type: "static", value: "yes" }),
            actions: [{ id: "a", objective: "jumpToBlock", target: "ending-1" }],
          }),
        ],
      },
    ]);
    expect(findElementUsedInLogic(survey, "q1")).toBe(1);
    expect(findElementUsedInLogic(survey, "q2")).toBe(-1);
  });

  test("finds a requireAnswer action and a calculate value that name the element", () => {
    const survey = surveyWith([
      {
        id: "b1",
        name: "Block 1",
        elements: [openText("q1")],
        logic: [
          rule({
            conditions: condition({ type: "element", value: "q1" }, { type: "static", value: "x" }),
            actions: [{ id: "a", objective: "requireAnswer", target: "q2" }],
          }),
        ],
      },
      { id: "b2", name: "Block 2", elements: [openText("q2")] },
    ]);
    expect(findElementUsedInLogic(survey, "q2")).toBe(0);
  });

  test("returns -1 for an element the survey does not have", () => {
    expect(findElementUsedInLogic(surveyWith([]), "missing")).toBe(-1);
  });
});

describe("findOptionUsedInLogic", () => {
  test("finds a condition comparing the element with the choice", () => {
    const survey = surveyWith([
      {
        id: "b1",
        name: "Block 1",
        elements: [choice("mc")],
        logic: [
          rule({
            conditions: condition({ type: "element", value: "mc" }, { type: "static", value: "opt-b" }),
            actions: [{ id: "a", objective: "jumpToBlock", target: "ending-1" }],
          }),
        ],
      },
    ]);
    expect(findOptionUsedInLogic(survey, "mc", "opt-b")).toBe(0);
    expect(findOptionUsedInLogic(survey, "mc", "opt-a")).toBe(-1);
  });

  test("finds the choice inside a multi-value operand", () => {
    const survey = surveyWith([
      {
        id: "b1",
        name: "Block 1",
        elements: [choice("mc")],
        logic: [
          rule({
            conditions: condition(
              { type: "element", value: "mc" },
              { type: "static", value: ["opt-a", "opt-b"] }
            ),
            actions: [{ id: "a", objective: "jumpToBlock", target: "ending-1" }],
          }),
        ],
      },
    ]);
    expect(findOptionUsedInLogic(survey, "mc", "opt-a")).toBe(0);
  });
});

describe("findEndingCardUsedInLogic and findBlockUsedInLogic", () => {
  const survey = surveyWith([
    {
      id: "b1",
      name: "Block 1",
      elements: [openText("q1")],
      logic: [
        rule({
          conditions: condition({ type: "element", value: "q1" }, { type: "static", value: "x" }),
          actions: [{ id: "a", objective: "jumpToBlock", target: "b3" }],
        }),
      ],
      logicFallback: "ending-1",
    },
    { id: "b2", name: "Block 2", elements: [openText("q2")] },
    { id: "b3", name: "Block 3", elements: [openText("q3")] },
  ]);

  test("finds an ending that is a block's fallback", () => {
    expect(findEndingCardUsedInLogic(survey, "ending-1")).toBe(0);
    expect(findEndingCardUsedInLogic(survey, "ending-2")).toBe(-1);
  });

  test("finds a block another block jumps to", () => {
    expect(findBlockUsedInLogic(survey, "b3")).toBe(0);
    expect(findBlockUsedInLogic(survey, "b2")).toBe(-1);
  });
});

describe("findHiddenFieldUsedInLogic and findVariableUsedInLogic", () => {
  const survey = surveyWith(
    [
      {
        id: "b1",
        name: "Block 1",
        elements: [openText("q1")],
        logic: [
          rule({
            conditions: condition({ type: "hiddenField", value: "source" }, { type: "static", value: "ad" }),
            actions: [
              {
                id: "a",
                objective: "calculate",
                variableId: "var1",
                operator: "add",
                value: { type: "static", value: 1 },
              },
            ],
          }),
        ],
      },
    ],
    { variables: [{ id: "var1", name: "score", type: "number", value: 0 }] }
  );

  test("finds a condition on the hidden field", () => {
    expect(findHiddenFieldUsedInLogic(survey, "source")).toBe(0);
    expect(findHiddenFieldUsedInLogic(survey, "other")).toBe(-1);
  });

  test("finds a calculate action on the variable", () => {
    expect(findVariableUsedInLogic(survey, "var1")).toBe(0);
    expect(findVariableUsedInLogic(survey, "var2")).toBe(-1);
  });
});

describe("isUsedInQuota", () => {
  const quota = {
    action: "endSurvey",
    endingCardId: "ending-1",
    logic: {
      connector: "and",
      conditions: [
        {
          id: "c1",
          leftOperand: { type: "element", value: "q1" },
          operator: "equals",
          rightOperand: { type: "variable", value: "var1" },
        },
      ],
    },
  } as unknown as TSurveyQuota;

  test("finds the element, variable and ending a quota names", () => {
    expect(isUsedInQuota(quota, { elementId: "q1" })).toBe(true);
    expect(isUsedInQuota(quota, { variableId: "var1" })).toBe(true);
    expect(isUsedInQuota(quota, { endingCardId: "ending-1" })).toBe(true);
    expect(isUsedInQuota(quota, { elementId: "q2" })).toBe(false);
    expect(isUsedInQuota(quota, { hiddenFieldId: "source" })).toBe(false);
  });
});

describe("isUsedInRecall", () => {
  test("returns the element whose headline recalls the id", () => {
    const survey = surveyWith([
      {
        id: "b1",
        name: "Block 1",
        elements: [openText("q1"), openText("q2", "Hi #recall:q1/fallback:friend#")],
      },
    ]);
    expect(isUsedInRecall(survey, "q1")).toBe(1);
    expect(isUsedInRecall(survey, "q2")).toBe(-1);
  });

  test("returns -2 for an enabled welcome card and the element count for an ending", () => {
    const welcome = surveyWith([{ id: "b1", name: "Block 1", elements: [openText("q1")] }], {
      welcomeCard: {
        enabled: true,
        timeToFinish: false,
        showResponseCount: false,
        headline: { default: "Hi #recall:q1/fallback:x#" },
      },
    });
    expect(isUsedInRecall(welcome, "q1")).toBe(-2);

    const ending = surveyWith([{ id: "b1", name: "Block 1", elements: [openText("q1")] }], {
      endings: [{ id: "ending-1", type: "endScreen", headline: { default: "Bye #recall:q1/fallback:x#" } }],
    });
    expect(isUsedInRecall(ending, "q1")).toBe(1);
  });
});
