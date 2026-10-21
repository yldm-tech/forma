import { describe, expect, test } from "vitest";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurveySummary } from "@forma/types/surveys/types";
import {
  buildElementImpressionLookup,
  getElementDenominator,
} from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/summary/lib/element-denominator";

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
