import type { TSurveySummary } from "@forma/types/surveys/types";
import { convertFloatTo2Decimal } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/summary/lib/utils";

export interface TElementDenominator {
  /** How many respondents reached the element — the denominator every per-choice percentage is missing. */
  impressionCount: number;
  /** Reached it and left no answer. Only meaningful against the impression count above. */
  skipCount: number;
  skipPercentage: number;
}

/**
 * The drop-off table already carries per-element impressions, so the denominator every element card is
 * missing is in the summary payload the client is handed — it is simply not rendered anywhere but the
 * CTA card. Keyed by element id rather than by position: the summary list and the drop-off array are
 * built from the same element order today, but only one of them is guaranteed to hold every element
 * (hidden fields are appended to the summary and have no impressions concept).
 */
export const buildElementImpressionLookup = (
  dropOff: TSurveySummary["dropOff"]
): ReadonlyMap<string, number> => new Map(dropOff.map((entry) => [entry.elementId, entry.impressions]));

/**
 * Derives what an element card can honestly say about its denominator, or `null` when it cannot say
 * anything useful.
 *
 * Returns `null` when there are no impressions (nothing to be a denominator of) and when the answer
 * count exceeds them. The second case is real rather than defensive: impressions are counted from
 * response interaction data, except for the first element, whose count is replaced by the survey's
 * `displayCount` from the Display table. Under a filter those two are counted over different
 * populations, so the first element can report fewer impressions than answers — and a negative
 * "skipped" is worse than no badge.
 */
export const getElementDenominator = (
  impressionCount: number | undefined,
  responseCount: number
): TElementDenominator | null => {
  if (impressionCount === undefined || impressionCount <= 0 || responseCount > impressionCount) {
    return null;
  }

  const skipCount = impressionCount - responseCount;

  return {
    impressionCount,
    skipCount,
    skipPercentage: convertFloatTo2Decimal((skipCount / impressionCount) * 100),
  };
};
