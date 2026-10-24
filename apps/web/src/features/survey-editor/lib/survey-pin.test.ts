import { describe, expect, test } from "vite-plus/test";
import { SURVEY_PIN_PATTERN, generateSurveyPin } from "./survey-pin";

/** A generator that hands out the given values in turn. */
const sequence = (...values: number[]) => {
  let next = 0;
  return (array: Uint32Array<ArrayBuffer>) => {
    array[0] = values[next++] ?? 0;
    return array;
  };
};

describe("generateSurveyPin", () => {
  test("returns four digits, zero-padded", () => {
    expect(generateSurveyPin(sequence(7))).toBe("0007");
    expect(generateSurveyPin(sequence(123_456_789))).toBe("6789");
  });

  test("draws again rather than favour the low PINs with the top of the range", () => {
    expect(generateSurveyPin(sequence(4_294_967_295, 4_294_960_000, 42))).toBe("0042");
  });

  test("is not the fixed 1234 forma2 used, and varies between calls", () => {
    const pins = new Set(Array.from({ length: 50 }, () => generateSurveyPin()));
    for (const pin of pins) expect(pin).toMatch(SURVEY_PIN_PATTERN);
    expect(pins.size).toBeGreaterThan(40);
  });
});
