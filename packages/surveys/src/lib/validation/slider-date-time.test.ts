import type { TFunction } from "i18next";
import { describe, expect, test, vi } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurveyDateElement, TSurveySliderElement } from "@forma/types/surveys/elements";
import { validateElementResponse } from "./evaluator";

// The translation function returns the key, so the assertions read which message was chosen.
vi.mock("@/lib/i18n", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/i18n")>();
  return { ...actual, getTranslations: () => ((key: string) => key) as unknown as TFunction };
});

const slider = (overrides: Partial<TSurveySliderElement> = {}): TSurveySliderElement => ({
  id: "s",
  type: TSurveyElementTypeEnum.Slider,
  headline: { default: "How many?" },
  required: false,
  min: 0,
  max: 1,
  step: 0.1,
  ...overrides,
});

const dateTime = (overrides: Partial<TSurveyDateElement> = {}): TSurveyDateElement => ({
  id: "d",
  type: TSurveyElementTypeEnum.Date,
  headline: { default: "When?" },
  required: false,
  format: "y-M-d",
  includeTime: true,
  ...overrides,
});

const messages = (result: ReturnType<typeof validateElementResponse>) => result.errors.map((e) => e.message);

describe("slider answers", () => {
  test("a position of the scale passes, binary fractions included", () => {
    for (const value of [0, 0.3, 0.1 + 0.2, 1]) {
      expect(validateElementResponse(slider(), value, "default").valid).toBe(true);
    }
  });

  test("a number off the scale or between its steps is refused", () => {
    for (const value of [1.1, -0.1, 0.35, "0.5"]) {
      expect(messages(validateElementResponse(slider(), value, "default"))).toEqual([
        "errors.invalid_format",
      ]);
    }
  });

  test("a required slider left untouched asks for a value", () => {
    expect(messages(validateElementResponse(slider({ required: true }), undefined, "default"))).toEqual([
      "slider.choose_a_value",
    ]);
  });
});

describe("date answers with a time of day", () => {
  test("a date and time passes", () => {
    expect(validateElementResponse(dateTime(), "2026-03-01T09:30", "default").valid).toBe(true);
  });

  test("a day without the time asks for the time", () => {
    expect(messages(validateElementResponse(dateTime(), "2026-03-01", "default"))).toEqual([
      "date_time.select_a_time",
    ]);
  });

  test("a date element without the time takes the day alone", () => {
    expect(validateElementResponse(dateTime({ includeTime: false }), "2026-03-01", "default").valid).toBe(
      true
    );
  });

  test("date rules hold the answer to its day", () => {
    const element = dateTime({
      validation: { logic: "and", rules: [{ id: "r", type: "isLaterThan", params: { date: "2026-03-01" } }] },
    });
    // The fixed bound is exclusive and names a day: no time on that day is later than it.
    expect(validateElementResponse(element, "2026-03-01T23:59", "default").valid).toBe(false);
    expect(validateElementResponse(element, "2026-03-02T00:00", "default").valid).toBe(true);
  });
});
