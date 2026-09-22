import { describe, expect, test } from "vitest";
import { OperationNotAllowedError } from "@forma/types/errors";
import {
  SURVEY_MODIFIED_ELSEWHERE_ERROR_CODE,
  assertSurveyNotModifiedElsewhere,
  parseSurveyModifiedElsewhereError,
  toSurveyVersion,
} from "./save-conflict";

const LOADED = new Date("2026-09-22T10:00:00.000Z");
const MOVED_ON = new Date("2026-09-22T10:00:31.000Z");

describe("assertSurveyNotModifiedElsewhere", () => {
  test("refuses a save built on a version the survey has moved past", () => {
    expect(() => assertSurveyNotModifiedElsewhere(LOADED, MOVED_ON)).toThrow(OperationNotAllowedError);
  });

  test("names the stored version in the error, so the client can adopt it and overwrite deliberately", () => {
    let thrown: unknown;
    try {
      assertSurveyNotModifiedElsewhere(LOADED, MOVED_ON);
    } catch (error) {
      thrown = error;
    }

    expect((thrown as Error).message).toBe(
      `${SURVEY_MODIFIED_ELSEWHERE_ERROR_CODE}:${MOVED_ON.toISOString()}`
    );
    // The action client returns `message` as `serverError` only for an expected error; an unexpected one
    // is replaced by a generic string and reported to Sentry, which would lose the version.
    expect((thrown as Error).name).toBe("OperationNotAllowedError");
  });

  test("allows a save whose version matches the stored row", () => {
    expect(() => assertSurveyNotModifiedElsewhere(LOADED, new Date(LOADED))).not.toThrow();
  });

  test("allows a save that supplies no version, so a caller that never loaded the survey still writes", () => {
    expect(() => assertSurveyNotModifiedElsewhere(undefined, MOVED_ON)).not.toThrow();
    expect(() => assertSurveyNotModifiedElsewhere(null, MOVED_ON)).not.toThrow();
    expect(() => assertSurveyNotModifiedElsewhere("not a date", MOVED_ON)).not.toThrow();
  });

  test("allows a version ahead of the stored row rather than locking the editor out", () => {
    expect(() => assertSurveyNotModifiedElsewhere(MOVED_ON, LOADED)).not.toThrow();
  });

  test("accepts a version that arrived as a string, which the loose draft schema does not parse", () => {
    expect(() => assertSurveyNotModifiedElsewhere(LOADED.toISOString(), MOVED_ON)).toThrow(
      OperationNotAllowedError
    );
    expect(() => assertSurveyNotModifiedElsewhere(MOVED_ON.toISOString(), MOVED_ON)).not.toThrow();
  });

  test("treats a sub-second difference as a conflict", () => {
    expect(() => assertSurveyNotModifiedElsewhere(LOADED, new Date(LOADED.getTime() + 1))).toThrow(
      OperationNotAllowedError
    );
  });
});

describe("parseSurveyModifiedElsewhereError", () => {
  test("recovers the stored version from the serverError a conflict produced", () => {
    let thrown: unknown;
    try {
      assertSurveyNotModifiedElsewhere(LOADED, MOVED_ON);
    } catch (error) {
      thrown = error;
    }

    expect(parseSurveyModifiedElsewhereError((thrown as Error).message)).toEqual(MOVED_ON);
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
