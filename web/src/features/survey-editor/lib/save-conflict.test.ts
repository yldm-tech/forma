import { describe, expect, test } from "vite-plus/test";
import {
  SURVEY_MODIFIED_ELSEWHERE_ERROR_CODE,
  parseSurveyModifiedElsewhereError,
  toSurveyVersion,
} from "./save-conflict";

const MOVED_ON = new Date("2026-09-22T10:00:31.000Z");

describe("parseSurveyModifiedElsewhereError", () => {
  test("recovers the stored version from the message of a refused draft save", () => {
    // The Go server formats the stored version as RFC 3339 with nanoseconds, which drops trailing zeros.
    expect(parseSurveyModifiedElsewhereError("survey_modified_elsewhere:2026-09-22T10:00:31Z")).toEqual(
      MOVED_ON
    );
    expect(parseSurveyModifiedElsewhereError(`survey_modified_elsewhere:${MOVED_ON.toISOString()}`)).toEqual(
      MOVED_ON
    );
  });

  test("returns null for any other failure, so an unrelated error is not read as a conflict", () => {
    expect(parseSurveyModifiedElsewhereError(undefined)).toBeNull();
    expect(parseSurveyModifiedElsewhereError("")).toBeNull();
    expect(parseSurveyModifiedElsewhereError("Survey follow ups are not enabled")).toBeNull();
    expect(parseSurveyModifiedElsewhereError(SURVEY_MODIFIED_ELSEWHERE_ERROR_CODE)).toBeNull();
    expect(parseSurveyModifiedElsewhereError(`${SURVEY_MODIFIED_ELSEWHERE_ERROR_CODE}:nonsense`)).toBeNull();
  });
});

describe("toSurveyVersion", () => {
  test("rejects values that cannot be a version", () => {
    expect(toSurveyVersion(new Date("nope"))).toBeNull();
    expect(toSurveyVersion({})).toBeNull();
    expect(toSurveyVersion(true)).toBeNull();
  });

  test("keeps millisecond precision, which is what the stored column carries", () => {
    expect(toSurveyVersion("2026-09-22T10:00:00.123Z")?.getTime()).toBe(
      new Date("2026-09-22T10:00:00.123Z").getTime()
    );
  });
});
