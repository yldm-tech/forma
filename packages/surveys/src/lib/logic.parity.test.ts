import { describe, expect, test, vi } from "vitest";
import {
  LOGIC_PARITY_CASES,
  getUncoveredLogicParityOperators,
  runLogicParityCase,
} from "@forma/types/surveys/logic-parity-fixtures";
import { evaluateLogic } from "./logic";

// Localization is deliberately stubbed with the same two-line resolver the twin suite in
// `apps/web/lib/surveyLogic/utils.parity.test.ts` uses. The two engines reach `getLocalizedValue`
// through different modules, and the parity this table is about is the evaluator's, not i18n's — an
// identical stub on both sides keeps a difference in the i18n helpers out of the comparison.
vi.mock("@/lib/i18n", () => ({
  getLocalizedValue: (value: unknown, languageId: string) =>
    typeof value === "object" && value !== null
      ? ((value as Record<string, string>)[languageId] ?? (value as Record<string, string>).default ?? "")
      : value,
}));

describe("survey logic parity (renderer engine)", () => {
  test("every operator in ZSurveyLogicConditionsOperator has a parity case", () => {
    expect(getUncoveredLogicParityOperators()).toEqual([]);
  });

  test.each(LOGIC_PARITY_CASES.map((parityCase) => [parityCase.name, parityCase] as const))(
    "%s",
    (_name, parityCase) => {
      expect(runLogicParityCase(evaluateLogic, parityCase)).toBe(parityCase.expected);
    }
  );
});
