import { expect, test } from "vite-plus/test";
import { validSurveyName } from "./rename-survey-modal";

test("a survey name is trimmed and must keep at least one character", () => {
  expect(validSurveyName("  NPS 2026 ")).toBe("NPS 2026");
  expect(validSurveyName("   ")).toBeNull();
  expect(validSurveyName("")).toBeNull();
});
