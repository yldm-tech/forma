import type { TFunction } from "i18next";
import { describe, expect, test, vi } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey, TSurveySummary } from "@forma/types/surveys/types";
import {
  buildElementImpressionLookup,
  constructToastMessage,
  convertFloatTo2Decimal,
  convertFloatToNDecimal,
  formatAverage,
  formatTimeToComplete,
  getElementDenominator,
  getSampleTruncation,
  satisfactionColor,
  visiblePercentage,
} from "./summary";

describe("Utils Tests", () => {
  describe("convertFloatToNDecimal", () => {
    test("should round to N decimal places", () => {
      expect(convertFloatToNDecimal(3.14159, 2)).toBe(3.14);
      expect(convertFloatToNDecimal(3.14159, 3)).toBe(3.142);
      expect(convertFloatToNDecimal(3.1, 2)).toBe(3.1);
      expect(convertFloatToNDecimal(3, 2)).toBe(3);
      expect(convertFloatToNDecimal(0.129, 2)).toBe(0.13);
    });

    test("should default to 2 decimal places if N is not provided", () => {
      expect(convertFloatToNDecimal(3.14159)).toBe(3.14);
    });
  });

  describe("convertFloatTo2Decimal", () => {
    test("should round to 2 decimal places", () => {
      expect(convertFloatTo2Decimal(3.14159)).toBe(3.14);
      expect(convertFloatTo2Decimal(3.1)).toBe(3.1);
      expect(convertFloatTo2Decimal(3)).toBe(3);
      expect(convertFloatTo2Decimal(0.129)).toBe(0.13);
    });
  });

  describe("constructToastMessage", () => {
    const mockT = vi.fn(
      (key: string, params: unknown) => `${key} ${JSON.stringify(params)}`
    ) as unknown as TFunction;
    const mockSurvey = {
      id: "survey1",
      name: "Test Survey",
      type: "app",
      status: "draft",
      blocks: [
        {
          id: "block1",
          name: "Block 1",
          elements: [
            {
              id: "q1",
              type: TSurveyElementTypeEnum.OpenText,
              headline: { default: "Q1" },
              required: false,
              charLimit: { enabled: false },
            },
            {
              id: "q2",
              type: TSurveyElementTypeEnum.MultipleChoiceSingle,
              headline: { default: "Q2" },
              required: false,
              choices: [{ id: "c1", label: { default: "Choice 1" } }],
              buttonLabel: { default: "Next" },
              shuffleOption: "none",
            },
            {
              id: "q3",
              type: TSurveyElementTypeEnum.Matrix,
              headline: { default: "Q3" },
              required: false,
              rows: [{ id: "r1", label: { default: "Row 1" } }],
              columns: [{ id: "col1", label: { default: "Col 1" } }],
              buttonLabel: { default: "Next" },
            },
          ],
        },
      ],
      questions: [],
      triggers: [],
      recontactDays: null,
      autoClose: null,
      delay: 0,
      autoComplete: null,
      singleUse: null,
      styling: null,
      surveyClosedMessage: null,
      displayOption: "displayOnce",
      welcomeCard: { enabled: false } as TSurvey["welcomeCard"],
      createdAt: new Date(),
      updatedAt: new Date(),
      languages: [],
    } as unknown as TSurvey;

    test("should construct message for matrix question type", () => {
      const message = constructToastMessage(
        TSurveyElementTypeEnum.Matrix,
        "is",
        mockSurvey,
        "q3",
        mockT,
        "MatrixValue"
      );
      expect(mockT).toHaveBeenCalledWith(
        "workspace.surveys.summary.added_filter_for_responses_where_answer_to_question",
        {
          questionIdx: 3,
          filterComboBoxValue: "MatrixValue",
          filterValue: "is",
        }
      );
      expect(message).toBe(
        'workspace.surveys.summary.added_filter_for_responses_where_answer_to_question {"questionIdx":3,"filterComboBoxValue":"MatrixValue","filterValue":"is"}'
      );
    });

    test("should construct message for matrix question type with array filterComboBoxValue", () => {
      const message = constructToastMessage(TSurveyElementTypeEnum.Matrix, "is", mockSurvey, "q3", mockT, [
        "MatrixValue1",
        "MatrixValue2",
      ]);
      expect(mockT).toHaveBeenCalledWith(
        "workspace.surveys.summary.added_filter_for_responses_where_answer_to_question",
        {
          questionIdx: 3,
          filterComboBoxValue: "MatrixValue1,MatrixValue2",
          filterValue: "is",
        }
      );
      expect(message).toBe(
        'workspace.surveys.summary.added_filter_for_responses_where_answer_to_question {"questionIdx":3,"filterComboBoxValue":"MatrixValue1,MatrixValue2","filterValue":"is"}'
      );
    });

    test("should construct message when filterComboBoxValue is undefined (skipped)", () => {
      const message = constructToastMessage(
        TSurveyElementTypeEnum.OpenText,
        "is skipped",
        mockSurvey,
        "q1",
        mockT,
        undefined
      );
      expect(mockT).toHaveBeenCalledWith(
        "workspace.surveys.summary.added_filter_for_responses_where_answer_to_question_is_skipped",
        {
          questionIdx: 1,
        }
      );
      expect(message).toBe(
        'workspace.surveys.summary.added_filter_for_responses_where_answer_to_question_is_skipped {"questionIdx":1}'
      );
    });

    test("should construct message for non-matrix question with string filterComboBoxValue", () => {
      const message = constructToastMessage(
        TSurveyElementTypeEnum.MultipleChoiceSingle,
        "is",
        mockSurvey,
        "q2",
        mockT,
        "Choice1"
      );
      expect(mockT).toHaveBeenCalledWith(
        "workspace.surveys.summary.added_filter_for_responses_where_answer_to_question",
        {
          questionIdx: 2,
          filterComboBoxValue: "Choice1",
          filterValue: "is",
        }
      );
      expect(message).toBe(
        'workspace.surveys.summary.added_filter_for_responses_where_answer_to_question {"questionIdx":2,"filterComboBoxValue":"Choice1","filterValue":"is"}'
      );
    });

    test("should construct message for non-matrix question with array filterComboBoxValue", () => {
      const message = constructToastMessage(
        TSurveyElementTypeEnum.MultipleChoiceMulti,
        "includes all of",
        mockSurvey,
        "q2", // Assuming q2 can be multi for this test case logic
        mockT,
        ["Choice1", "Choice2"]
      );
      expect(mockT).toHaveBeenCalledWith(
        "workspace.surveys.summary.added_filter_for_responses_where_answer_to_question",
        {
          questionIdx: 2,
          filterComboBoxValue: "Choice1,Choice2",
          filterValue: "includes all of",
        }
      );
      expect(message).toBe(
        'workspace.surveys.summary.added_filter_for_responses_where_answer_to_question {"questionIdx":2,"filterComboBoxValue":"Choice1,Choice2","filterValue":"includes all of"}'
      );
    });

    test("should handle questionId not found in survey", () => {
      const message = constructToastMessage(
        TSurveyElementTypeEnum.OpenText,
        "is",
        mockSurvey,
        "qNonExistent",
        mockT,
        "SomeValue"
      );
      // findIndex returns -1, so questionIdx becomes -1 + 1 = 0
      expect(mockT).toHaveBeenCalledWith(
        "workspace.surveys.summary.added_filter_for_responses_where_answer_to_question",
        {
          questionIdx: 0,
          filterComboBoxValue: "SomeValue",
          filterValue: "is",
        }
      );
      expect(message).toBe(
        'workspace.surveys.summary.added_filter_for_responses_where_answer_to_question {"questionIdx":0,"filterComboBoxValue":"SomeValue","filterValue":"is"}'
      );
    });
  });
});

const dropOffEntry = (elementId: string, impressions: number): TSurveySummary["dropOff"][number] => ({
  elementId,
  elementType: TSurveyElementTypeEnum.OpenText,
  headline: elementId,
  ttc: 0,
  impressions,
  dropOffCount: 0,
  dropOffPercentage: 0,
});

describe("buildElementImpressionLookup", () => {
  test("keys impressions by element id, not by position", () => {
    const lookup = buildElementImpressionLookup([dropOffEntry("q1", 1900), dropOffEntry("q2", 412)]);

    expect(lookup.get("q1")).toBe(1900);
    expect(lookup.get("q2")).toBe(412);
    expect(lookup.get("a-hidden-field")).toBeUndefined();
  });

  test("an empty drop-off array is an empty lookup rather than a throw", () => {
    expect(buildElementImpressionLookup([]).size).toBe(0);
  });
});

describe("getElementDenominator", () => {
  test("derives the skip count and its share of the impressions", () => {
    expect(getElementDenominator(1900, 412)).toEqual({
      impressionCount: 1900,
      skipCount: 1488,
      skipPercentage: 78.32,
    });
  });

  test("an element nobody skipped still reports its denominator", () => {
    expect(getElementDenominator(250, 250)).toEqual({
      impressionCount: 250,
      skipCount: 0,
      skipPercentage: 0,
    });
  });

  test("an element with no impressions has nothing to be a denominator of", () => {
    expect(getElementDenominator(0, 0)).toBeNull();
  });

  test("an element the drop-off table does not cover is skipped", () => {
    expect(getElementDenominator(undefined, 42)).toBeNull();
  });

  test("more answers than impressions yields no badge rather than a negative skip count", () => {
    // The first element's impressions are replaced by the survey's `displayCount`, which is counted
    // over a different population than the filtered responses behind `responseCount`.
    expect(getElementDenominator(10, 12)).toBeNull();
  });
});

describe("getSampleTruncation", () => {
  test("returns the shown/total pair when the sample list was capped", () => {
    expect(getSampleTruncation(50, 312)).toEqual({ shown: 50, total: 312 });
  });

  test("returns null when every answer made it into the sample list", () => {
    expect(getSampleTruncation(12, 12)).toBeNull();
  });

  test("returns null for an empty sample list so the empty state stays the only message", () => {
    expect(getSampleTruncation(0, 0)).toBeNull();
    expect(getSampleTruncation(0, 7)).toBeNull();
  });

  test("returns null when the sample list is longer than the reported count", () => {
    // Defensive: a count that lags behind the samples must not render a "50 / 10" line.
    expect(getSampleTruncation(50, 10)).toBeNull();
  });
});

describe("formatTimeToComplete", () => {
  test("renders seconds to two decimals below a minute", () => {
    expect(formatTimeToComplete(12_345)).toBe("12.35s");
    expect(formatTimeToComplete(500)).toBe("0.50s");
  });

  test("splits minutes off from a minute on", () => {
    expect(formatTimeToComplete(60_000)).toBe("1m 0.00s");
    expect(formatTimeToComplete(125_500)).toBe("2m 5.50s");
  });
});

describe("visiblePercentage", () => {
  test("rounds to a whole percent", () => {
    expect(visiblePercentage(33.33)).toBe(33);
    expect(visiblePercentage(66.5)).toBe(67);
  });

  test("hides a share above 100, which displays and responses counted over different populations produce", () => {
    expect(visiblePercentage(100.4)).toBe(100);
    expect(visiblePercentage(100.5)).toBeNull();
    expect(visiblePercentage(250)).toBeNull();
  });
});

describe("satisfactionColor", () => {
  test("follows forma2's thresholds", () => {
    expect(satisfactionColor(80.01)).toBe("bg-emerald-500");
    expect(satisfactionColor(80)).toBe("bg-orange-500");
    expect(satisfactionColor(55)).toBe("bg-orange-500");
    expect(satisfactionColor(54.99)).toBe("bg-rose-500");
  });
});

describe("formatAverage", () => {
  test("prints two decimals", () => {
    expect(formatAverage(3.456)).toBe("3.46");
    expect(formatAverage(4)).toBe("4.00");
  });

  test("an average of no answers prints as forma2 printed NaN", () => {
    expect(formatAverage(null)).toBe("NaN");
    expect(formatAverage(Number.NaN)).toBe("NaN");
  });
});
