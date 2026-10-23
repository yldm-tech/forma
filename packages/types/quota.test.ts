import { describe, expect, test } from "vite-plus/test";
import { ZSurveyQuotaInput } from "./quota";

const input = (surveyId: string, endingCardId: string | null = "endingone000000000000000") => ({
  surveyId,
  name: "Adults",
  limit: 100,
  logic: { connector: "and", conditions: [] },
  action: "endSurvey",
  endingCardId,
  countPartialSubmissions: false,
});

describe("ZSurveyQuotaInput", () => {
  test("accepts the Go server's UUID survey ids and forma2's cuid2 ones", () => {
    expect(ZSurveyQuotaInput.safeParse(input("0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e")).success).toBe(true);
    expect(ZSurveyQuotaInput.safeParse(input("clx0survey0000000000000")).success).toBe(true);
  });

  test("rejects a malformed survey id", () => {
    expect(ZSurveyQuotaInput.safeParse(input("Not-An-Id")).success).toBe(false);
  });

  test("keeps the ending card id to the survey document's cuid2 rule", () => {
    expect(
      ZSurveyQuotaInput.safeParse(
        input("0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e", "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f")
      ).success
    ).toBe(false);
  });
});
