/**
 * The scale of a slider element (min, max, step) and the checks on it and on its answers. Kept free of zod so the survey runtime can import it without the schema library; `apps/server/internal/survey/surveydoc` (the document check) and `apps/server/internal/response/answers` (the answer check) port the same rules to Go.
 */

export interface TSliderScale {
  min: number;
  max: number;
  step: number;
}

/** The most positions a slider may have: more are steps a respondent can no longer tell apart on a phone, and a summary could no longer list them. */
export const SLIDER_MAX_STEPS = 1000;

/** How far a computed step count may stray from a whole number and still count as one: binary fractions make 0.3 / 0.1 come out as 2.9999999999999996. */
const STEP_TOLERANCE = 1e-9;

const isWholeNumber = (n: number): boolean =>
  Math.abs(n - Math.round(n)) <= STEP_TOLERANCE * Math.max(1, Math.abs(n));

/** The number of steps from min to max, a whole number for a valid scale. */
export const getSliderStepCount = (scale: TSliderScale): number => (scale.max - scale.min) / scale.step;

export type TSliderScaleIssueCode =
  | "notNumbers"
  | "stepNotPositive"
  | "maxNotAboveMin"
  | "stepUneven"
  | "tooManySteps";

/** The first thing wrong with a slider's scale: what it is (code, for the editor to word), the field it is wrong in, and the message the document check reports; null for a valid scale. */
export const getSliderScaleIssue = (
  scale: TSliderScale
): { code: TSliderScaleIssueCode; field: "min" | "max" | "step"; message: string } | null => {
  if (![scale.min, scale.max, scale.step].every(Number.isFinite)) {
    return { code: "notNumbers", field: "min", message: "The minimum, maximum and step must be numbers" };
  }
  if (scale.step <= 0) {
    return { code: "stepNotPositive", field: "step", message: "The step must be greater than 0" };
  }
  if (scale.max <= scale.min) {
    return { code: "maxNotAboveMin", field: "max", message: "The maximum must be greater than the minimum" };
  }
  const steps = getSliderStepCount(scale);
  if (!isWholeNumber(steps)) {
    return {
      code: "stepUneven",
      field: "step",
      message: "The step must divide the range from the minimum to the maximum evenly",
    };
  }
  if (Math.round(steps) > SLIDER_MAX_STEPS) {
    return {
      code: "tooManySteps",
      field: "step",
      message: `A slider can have at most ${String(SLIDER_MAX_STEPS)} steps`,
    };
  }
  return null;
};

/** The decimal places a value of the scale needs: those of the step or of the minimum, whichever has more. */
export const getSliderDecimals = (scale: Pick<TSliderScale, "min" | "step">): number => {
  const decimals = (n: number): number => {
    const text = String(n);
    if (text.includes("e-")) {
      return Number(text.split("e-")[1]);
    }
    return text.split(".")[1]?.length ?? 0;
  };
  return Math.min(10, Math.max(decimals(scale.step), decimals(scale.min)));
};

/** A value moved to the nearest position of the scale and within it, written with no more decimals than the scale has, so 0.1 + 0.2 is stored as 0.3. */
export const snapSliderValue = (scale: TSliderScale, value: number): number => {
  const steps = Math.round((value - scale.min) / scale.step);
  const clamped = Math.min(Math.max(steps, 0), Math.round(getSliderStepCount(scale)));
  return Number((scale.min + clamped * scale.step).toFixed(getSliderDecimals(scale)));
};

/** Whether an answer is a position of the scale: a finite number from min to max, a whole number of steps from min. */
export const isSliderValueOnScale = (scale: TSliderScale, value: unknown): boolean => {
  if (typeof value !== "number" || !Number.isFinite(value)) return false;
  const span = scale.max - scale.min;
  const tolerance = STEP_TOLERANCE * Math.max(1, Math.abs(span));
  if (value < scale.min - tolerance || value > scale.max + tolerance) return false;
  return isWholeNumber((value - scale.min) / scale.step);
};

/** The value a slider shows before the respondent has touched it: the position nearest the middle of the scale. Never stored as an answer. */
export const getSliderRestingValue = (scale: TSliderScale): number =>
  snapSliderValue(scale, scale.min + (scale.max - scale.min) / 2);
