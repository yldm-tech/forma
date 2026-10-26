import { describe, expect, test } from "vite-plus/test";
import { getComparableDateAnswers, getAnswerDay } from "./date-answer";
import { ZSurveySliderElement } from "./elements";
import {
  SLIDER_MAX_STEPS,
  getSliderRestingValue,
  getSliderScaleIssue,
  isSliderValueOnScale,
  snapSliderValue,
} from "./slider";

describe("slider scale", () => {
  test("names what is wrong with a scale", () => {
    expect(getSliderScaleIssue({ min: 0, max: 10, step: 1 })).toBeNull();
    expect(getSliderScaleIssue({ min: 0, max: 1, step: 0.1 })).toBeNull();
    expect(getSliderScaleIssue({ min: 0, max: SLIDER_MAX_STEPS, step: 1 })).toBeNull();
    expect(getSliderScaleIssue({ min: 0, max: 10, step: 0 })?.code).toBe("stepNotPositive");
    expect(getSliderScaleIssue({ min: 5, max: 5, step: 1 })?.code).toBe("maxNotAboveMin");
    expect(getSliderScaleIssue({ min: 0, max: 10, step: 3 })?.code).toBe("stepUneven");
    expect(getSliderScaleIssue({ min: 0, max: SLIDER_MAX_STEPS + 1, step: 1 })?.code).toBe("tooManySteps");
    expect(getSliderScaleIssue({ min: Number.NaN, max: 10, step: 1 })?.code).toBe("notNumbers");
  });

  test("the element schema refuses an invalid scale at its field", () => {
    const parsed = ZSurveySliderElement.safeParse({
      id: "s",
      type: "slider",
      headline: { default: "Q" },
      required: false,
      min: 0,
      max: 10,
      step: 4,
    });
    expect(parsed.success).toBe(false);
    expect(parsed.error?.issues[0]?.path).toEqual(["step"]);
  });

  test("positions of the scale", () => {
    const scale = { min: 0, max: 1, step: 0.1 };
    expect(isSliderValueOnScale(scale, 0.1 + 0.2)).toBe(true);
    expect(isSliderValueOnScale(scale, 0.35)).toBe(false);
    expect(isSliderValueOnScale(scale, 1.1)).toBe(false);
    expect(isSliderValueOnScale(scale, "0.5")).toBe(false);
    expect(snapSliderValue(scale, 0.1 + 0.2)).toBe(0.3);
    expect(snapSliderValue(scale, 7)).toBe(1);
    expect(getSliderRestingValue({ min: 0, max: 100, step: 5 })).toBe(50);
    expect(getSliderRestingValue({ min: 1, max: 4, step: 1 })).toBe(3);
  });
});

describe("date answers", () => {
  test("a day is compared with a date and time by the day alone", () => {
    expect(getAnswerDay("2026-03-01T09:30")).toBe("2026-03-01");
    expect(getComparableDateAnswers("2026-03-01T09:30", "2026-03-01")).toEqual(["2026-03-01", "2026-03-01"]);
    expect(getComparableDateAnswers("2026-03-01T09:30", "2026-03-01T10:00")).toEqual([
      "2026-03-01T09:30",
      "2026-03-01T10:00",
    ]);
  });
});
