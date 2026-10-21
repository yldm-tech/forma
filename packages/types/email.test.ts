import { describe, expect, test } from "vitest";
import { ZLinkSurveyEmailData } from "./email";

const baseInput = {
  surveyId: "survey1",
  email: "respondent@example.com",
  locale: "en-US" as const,
};

describe("ZLinkSurveyEmailData", () => {
  test("accepts a single address", () => {
    const result = ZLinkSurveyEmailData.safeParse(baseInput);

    expect(result.success).toBe(true);
    expect(result.data?.email).toBe("respondent@example.com");
  });

  test.each([
    ["comma-separated list", "a@example.com, b@example.com"],
    ["semicolon-separated list", "a@example.com; b@example.com"],
    ["angle-bracket display name", "Support <a@example.com>, b@example.com"],
    ["newline-separated list", "a@example.com\nb@example.com"],
  ])("rejects a %s, which nodemailer would expand into several recipients", (_label, email) => {
    const result = ZLinkSurveyEmailData.safeParse({ ...baseInput, email });

    expect(result.success).toBe(false);
  });

  test("drops a caller-supplied survey name so it cannot reach the email template", () => {
    const result = ZLinkSurveyEmailData.safeParse({
      ...baseInput,
      surveyName: "URGENT: your payroll account is locked",
    });

    expect(result.success).toBe(true);
    expect(result.data).not.toHaveProperty("surveyName");
  });
});
