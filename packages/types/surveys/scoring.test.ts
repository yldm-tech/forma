import { describe, expect, test } from "vite-plus/test";
import { TSurveyElementTypeEnum, type TSurveyElement, ZSurveyMultipleChoiceSingleElement } from "./elements";
import {
  applyBlockScore,
  getElementScore,
  getNewScoreVariableName,
  getScoreVariable,
  isScoredElement,
} from "./scoring";
import type { TSurveyVariable } from "./types";

const single: TSurveyElement = {
  id: "capital",
  type: TSurveyElementTypeEnum.MultipleChoiceSingle,
  headline: { default: "Capital of France?", de: "Hauptstadt von Frankreich?" },
  required: true,
  choices: [
    { id: "paris", label: { default: "Paris", de: "Paris" }, score: 3 },
    { id: "lyon", label: { default: "Lyon", de: "Lyon" }, score: -1 },
    { id: "nice", label: { default: "Nice", de: "Nizza" } },
    { id: "other", label: { default: "Other", de: "Andere" }, score: 0.5 },
  ],
};

const multi: TSurveyElement = {
  id: "fruits",
  type: TSurveyElementTypeEnum.MultipleChoiceMulti,
  headline: { default: "Which are fruits?" },
  required: false,
  choices: [
    { id: "apple", label: { default: "Apple" }, score: 1 },
    { id: "pear", label: { default: "Pear" }, score: 1 },
    { id: "carrot", label: { default: "Carrot" }, score: -2 },
    { id: "other", label: { default: "Other" }, score: 0.1 },
  ],
};

const picture: TSurveyElement = {
  id: "logo",
  type: TSurveyElementTypeEnum.PictureSelection,
  headline: { default: "Which logo?" },
  required: false,
  allowMulti: true,
  choices: [
    { id: "a", imageUrl: "/storage/x/public/a.png", score: 2 },
    { id: "b", imageUrl: "/storage/x/public/b.png", score: 0.2 },
  ],
};

const unscored: TSurveyElement = {
  id: "colour",
  type: TSurveyElementTypeEnum.MultipleChoiceSingle,
  headline: { default: "Favourite colour?" },
  required: false,
  choices: [
    { id: "red", label: { default: "Red" } },
    { id: "blue", label: { default: "Blue" } },
  ],
};

const scoreVariable: TSurveyVariable = { id: "v1", name: "score", type: "number", value: 0, isScore: true };
const otherVariable: TSurveyVariable = { id: "v2", name: "bonus", type: "number", value: 0 };

describe("element score", () => {
  test("a single choice answer earns its choice's score, matched in any language", () => {
    expect(getElementScore(single, "Paris")).toBe(3);
    expect(getElementScore(single, "Lyon")).toBe(-1);
    expect(getElementScore(single, "Nizza")).toBe(0);
  });

  test("free text earns the score of the other choice", () => {
    expect(getElementScore(single, "Marseille")).toBe(0.5);
  });

  test("an unanswered or unscored element earns nothing", () => {
    expect(getElementScore(single, undefined)).toBeUndefined();
    expect(getElementScore(single, "")).toBeUndefined();
    expect(getElementScore(unscored, "Red")).toBeUndefined();
    expect(isScoredElement(unscored)).toBe(false);
    expect(isScoredElement(single)).toBe(true);
  });

  test("a multiple choice answer adds up its choices, each counted once", () => {
    expect(getElementScore(multi, ["Apple", "Pear"])).toBe(2);
    expect(getElementScore(multi, ["Apple", "Carrot"])).toBe(-1);
    expect(getElementScore(multi, ["Apple", "tomato", "potato"])).toBe(1.1);
    expect(getElementScore(multi, [])).toBeUndefined();
  });

  test("a picture selection answer names its choices by id", () => {
    expect(getElementScore(picture, ["a", "b"])).toBeCloseTo(2.2);
    expect(getElementScore(picture, ["missing"])).toBe(0);
  });
});

describe("total score variable", () => {
  test("is the number variable marked isScore", () => {
    expect(getScoreVariable([otherVariable, scoreVariable])).toBe(scoreVariable);
    expect(getScoreVariable([otherVariable])).toBeUndefined();
    expect(getScoreVariable(undefined)).toBeUndefined();
  });

  test("a new one gets a name no variable has", () => {
    expect(getNewScoreVariableName([otherVariable])).toBe("score");
    expect(getNewScoreVariableName([scoreVariable, { ...otherVariable, name: "score_2" }])).toBe("score_3");
  });
});

describe("block score", () => {
  test("raises the total score by what the block's answers earned", () => {
    const next = applyBlockScore(
      [otherVariable, scoreVariable],
      [single, multi, unscored],
      { capital: "Paris", fruits: ["Apple", "Carrot"], colour: "Red" },
      { v1: 4, v2: 7 }
    );
    expect(next).toEqual({ v1: 6, v2: 7 });
  });

  test("starts from the variable's initial value when the response has none yet", () => {
    expect(applyBlockScore([{ ...scoreVariable, value: 10 }], [single], { capital: "Paris" }, {})).toEqual({
      v1: 13,
    });
  });

  test("rounds away binary fractions", () => {
    expect(applyBlockScore([scoreVariable], [picture], { logo: ["b"] }, { v1: 0.1 })).toEqual({ v1: 0.3 });
  });

  test("leaves the variables alone without a total score variable or a scored answer", () => {
    const current = { v2: 1 };
    expect(applyBlockScore([otherVariable], [single], { capital: "Paris" }, current)).toBe(current);
    expect(applyBlockScore([scoreVariable], [single, unscored], { colour: "Red" }, current)).toBe(current);
  });
});

describe("choice score schema", () => {
  test("takes a bounded number", () => {
    const element = {
      id: "q",
      type: "multipleChoiceSingle",
      headline: { default: "Q" },
      required: false,
      choices: [
        { id: "a", label: { default: "A" }, score: 1 },
        { id: "b", label: { default: "B" } },
      ],
    };
    expect(ZSurveyMultipleChoiceSingleElement.safeParse(element).success).toBe(true);
    const tooBig = {
      ...element,
      choices: [{ id: "a", label: { default: "A" }, score: 2_000_000 }, element.choices[1]],
    };
    expect(ZSurveyMultipleChoiceSingleElement.safeParse(tooBig).success).toBe(false);
  });
});
