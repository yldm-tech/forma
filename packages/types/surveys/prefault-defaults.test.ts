import { describe, expect, expectTypeOf, test } from "vite-plus/test";
import type { z } from "zod";
import {
  type TShuffleOption,
  type TSurveyCTAElement,
  type TSurveyOpenTextElementInputType,
  ZSurveyCesElement,
  ZSurveyCsatElement,
  ZSurveyMatrixElement,
  ZSurveyNPSElement,
  ZSurveyOpenTextElement,
  ZSurveyPictureSelectionElement,
  ZSurveyRatingElement,
} from "./elements";
import {
  ZSurveyBase,
  ZSurveyCesQuestion,
  ZSurveyCsatQuestion,
  ZSurveyMatrixQuestion,
  ZSurveyNPSQuestion,
  ZSurveyOpenTextQuestion,
  ZSurveyPictureSelectionQuestion,
  ZSurveyRatingQuestion,
} from "./types";

// These fields are filled in by `.prefault()` when a stored document omits them, so their parsed value is never undefined and the editor and the respondent UI read them as plain values. Since zod 4.6.2 an `.optional()` under the prefault widens the output type to `| undefined` again (colinhacks/zod#6587) without changing what parsing returns, so the schemas use `.prefault()` alone; the type assertions below are what catch that wrapper coming back.
describe("prefaulted survey fields", () => {
  test("parse to their default when the key is missing", () => {
    expect(ZSurveyNPSElement.shape.isColorCodingEnabled.parse(undefined)).toBe(false);
    expect(ZSurveyRatingElement.shape.isColorCodingEnabled.parse(undefined)).toBe(false);
    expect(ZSurveyCsatElement.shape.isColorCodingEnabled.parse(undefined)).toBe(false);
    expect(ZSurveyCesElement.shape.isColorCodingEnabled.parse(undefined)).toBe(false);
    expect(ZSurveyPictureSelectionElement.shape.allowMulti.parse(undefined)).toBe(false);
    expect(ZSurveyMatrixElement.shape.shuffleOption.parse(undefined)).toBe("none");
    expect(ZSurveyOpenTextElement.shape.inputType.parse(undefined)).toBe("text");
    expect(ZSurveyBase.shape.isAutoProgressingEnabled.parse(undefined)).toBe(false);
    expect(ZSurveyNPSQuestion.shape.isColorCodingEnabled.parse(undefined)).toBe(false);
    expect(ZSurveyMatrixQuestion.shape.shuffleOption.parse(undefined)).toBe("none");
    expect(ZSurveyOpenTextQuestion.shape.inputType.parse(undefined)).toBe("text");
  });

  test("keep an explicit value", () => {
    expect(ZSurveyRatingElement.shape.isColorCodingEnabled.parse(true)).toBe(true);
    expect(ZSurveyMatrixElement.shape.shuffleOption.parse("all")).toBe("all");
    expect(ZSurveyBase.shape.isAutoProgressingEnabled.parse(true)).toBe(true);
  });

  test("have a non-optional output type", () => {
    expectTypeOf<z.output<typeof ZSurveyNPSElement>["isColorCodingEnabled"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyRatingElement>["isColorCodingEnabled"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyCsatElement>["isColorCodingEnabled"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyCesElement>["isColorCodingEnabled"]>().toEqualTypeOf<boolean>();
    expectTypeOf<TSurveyCTAElement["buttonExternal"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyPictureSelectionElement>["allowMulti"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyMatrixElement>["shuffleOption"]>().toEqualTypeOf<TShuffleOption>();
    expectTypeOf<
      z.output<typeof ZSurveyOpenTextElement>["inputType"]
    >().toEqualTypeOf<TSurveyOpenTextElementInputType>();
    expectTypeOf<z.output<typeof ZSurveyBase>["isAutoProgressingEnabled"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyNPSQuestion>["isColorCodingEnabled"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyRatingQuestion>["isColorCodingEnabled"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyCsatQuestion>["isColorCodingEnabled"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyCesQuestion>["isColorCodingEnabled"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyPictureSelectionQuestion>["allowMulti"]>().toEqualTypeOf<boolean>();
    expectTypeOf<z.output<typeof ZSurveyMatrixQuestion>["shuffleOption"]>().toEqualTypeOf<TShuffleOption>();
  });
});
