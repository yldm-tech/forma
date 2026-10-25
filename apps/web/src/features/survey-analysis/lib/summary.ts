import type { TFunction } from "i18next";
import type { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey, TSurveySummary } from "@forma/types/surveys/types";
import { getElementsFromBlocks } from "../../survey-editor/lib/client-utils";
import { getFilterOperatorLabel, getFilterValueLabel } from "./filters";

// The summary page's formatting: forma2's summary/lib utils, element-denominator and sample-truncation, and the number formats of its cards.

export const convertFloatToNDecimal = (num: number, N: number = 2) => {
  return Math.round(num * Math.pow(10, N)) / Math.pow(10, N);
};

export const convertFloatTo2Decimal = (num: number) => {
  return Math.round(num * 100) / 100;
};

/** The toast a summary click shows. filterValue and filterComboBoxValue are the filter's wire values; the message names them by their labels. */
export const constructToastMessage = (
  elementType: TSurveyElementTypeEnum,
  filterValue: string,
  survey: TSurvey,
  elementId: string,
  t: TFunction,
  filterComboBoxValue?: string | string[]
) => {
  const elements = getElementsFromBlocks(survey.blocks);
  const elementIdx = elements.findIndex((element) => element.id === elementId);
  if (elementType === "matrix") {
    return t("workspace.surveys.summary.added_filter_for_responses_where_answer_to_question", {
      questionIdx: elementIdx + 1,
      filterComboBoxValue: filterComboBoxValue?.toString() ?? "",
      filterValue,
    });
  } else if (filterComboBoxValue === undefined) {
    return t("workspace.surveys.summary.added_filter_for_responses_where_answer_to_question_is_skipped", {
      questionIdx: elementIdx + 1,
    });
  } else {
    const values = Array.isArray(filterComboBoxValue) ? filterComboBoxValue : [filterComboBoxValue];
    return t("workspace.surveys.summary.added_filter_for_responses_where_answer_to_question", {
      questionIdx: elementIdx + 1,
      filterComboBoxValue: values.map((value) => getFilterValueLabel(elementType, value, t)).join(","),
      filterValue: getFilterOperatorLabel(elementType, filterValue, t),
    });
  }
};

export interface TElementDenominator {
  /** How many respondents reached the element — the denominator every per-choice percentage is missing. */
  impressionCount: number;
  /** Reached it and left no answer. Only meaningful against the impression count above. */
  skipCount: number;
  skipPercentage: number;
}

/**
 * The drop-off table already carries per-element impressions, so the denominator every element card is missing is in the summary payload the client is handed — it is simply not rendered anywhere but the CTA card. Keyed by element id rather than by position: the summary list and the drop-off array are built from the same element order today, but only one of them is guaranteed to hold every element (hidden fields are appended to the summary and have no impressions concept).
 */
export const buildElementImpressionLookup = (
  dropOff: TSurveySummary["dropOff"]
): ReadonlyMap<string, number> => new Map(dropOff.map((entry) => [entry.elementId, entry.impressions]));

/**
 * Derives what an element card can honestly say about its denominator, or `null` when it cannot say anything useful.
 *
 * Returns `null` when there are no impressions (nothing to be a denominator of) and when the answer count exceeds them. The second case is real rather than defensive: impressions are counted from response interaction data, except for the first element, whose count is replaced by the survey's `displayCount` from the Display table. Under a filter those two are counted over different populations, so the first element can report fewer impressions than answers — and a negative "skipped" is worse than no badge.
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

/**
 * Summary payloads cap every per-response sample list at a fixed size (`VALUES_LIMIT` in `surveySummary.ts`) while still reporting the untruncated `responseCount`. The UI therefore has to derive the truncation itself; nothing in the payload flags it.
 */
export interface TSampleTruncation {
  shown: number;
  total: number;
}

/**
 * Returns the shown/total pair when a sample list was cut short, or null when it is complete.
 *
 * `responseCount` is the untruncated number of answers for the element; `sampleCount` is the length of the capped list that actually reached the client.
 */
export const getSampleTruncation = (sampleCount: number, responseCount: number): TSampleTruncation | null => {
  if (sampleCount <= 0) return null;
  if (sampleCount >= responseCount) return null;
  return { shown: sampleCount, total: responseCount };
};

/** forma2's SummaryMetadata formatTime: an average time to complete in milliseconds, as "{m}m {s}s" from a minute on and "{s}s" below, seconds to two decimals. */
export const formatTimeToComplete = (ttc: number): string => {
  const seconds = ttc / 1000;
  if (seconds >= 60) {
    const minutes = Math.floor(seconds / 60);
    const remainingSeconds = seconds % 60;
    return `${minutes}m ${remainingSeconds.toFixed(2)}s`;
  }
  return `${seconds.toFixed(2)}s`;
};

/** The percentage a starts or completed card shows: rounded, and hidden (null) when it passes 100, which displays counted over a different population than responses can produce. */
export const visiblePercentage = (percentage: number): number | null =>
  Math.round(percentage) > 100 ? null : Math.round(percentage);

/** forma2's SatisfactionIndicator colour: above 80 emerald, from 55 orange, else rose. */
export const satisfactionColor = (percentage: number): string => {
  if (percentage > 80) return "bg-emerald-500";
  if (percentage >= 55) return "bg-orange-500";
  return "bg-rose-500";
};

/** The colour of an NPS score's dot: below 0 rose, up to 29 amber, from 30 emerald, the usual reading of an NPS. The promoters' share is not the score, so satisfactionColor does not apply. */
export const npsScoreColor = (score: number): string => {
  const rounded = Math.round(score);
  if (rounded < 0) return "bg-rose-500";
  if (rounded < 30) return "bg-amber-500";
  return "bg-emerald-500";
};

/** An NPS score as its badge prints it: rounded, with its sign ("+16", "-5", "0"). */
export const formatNpsScore = (score: number, locale: string): string =>
  new Intl.NumberFormat(locale, { maximumFractionDigits: 0, signDisplay: "exceptZero" }).format(
    Math.round(score)
  );

/** An average as the score cards print it, to two decimals. An element with no numeric answer has no average: forma2 printed JavaScript's NaN, which the server sends as null. */
export const formatAverage = (average: number | null): string =>
  average === null || Number.isNaN(average) ? "NaN" : average.toFixed(2);
