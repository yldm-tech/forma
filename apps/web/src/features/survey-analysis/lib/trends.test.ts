import { describe, expect, test } from "vite-plus/test";
import type { SurveyTrends, TrendBucket } from "../api";
import {
  bucketLabel,
  bucketRangeLabel,
  countScale,
  formatTrendValue,
  labelledBuckets,
  metricScale,
  niceCeil,
  scoreScale,
  sliderScale,
  tooltipPosition,
  trendData,
} from "./trends";

const firstDay: TrendBucket = {
  start: "2026-03-01",
  end: "2026-03-01",
  partial: false,
  responses: 2,
  completed: 1,
  completionPercentage: 50,
};

const trends: SurveyTrends = {
  interval: "day",
  timeZone: "Asia/Tokyo",
  buckets: [
    firstDay,
    {
      start: "2026-03-02",
      end: "2026-03-02",
      partial: false,
      responses: 0,
      completed: 0,
      completionPercentage: 0,
    },
  ],
  elements: [
    {
      elementId: "nps",
      type: "nps",
      headline: "Recommend?",
      metric: "nps",
      points: [
        { answered: 2, value: 0 },
        { answered: 0, value: null },
      ],
    },
  ],
  score: {
    variableId: "total",
    name: "score",
    points: [
      { answered: 1, value: 7.5 },
      { answered: 0, value: null },
    ],
  },
};

describe("trendData", () => {
  test("draws the responses with their completed part", () => {
    expect(trendData(trends, "responses").map((d) => [d.value, d.completed])).toEqual([
      [2, 1],
      [0, 0],
    ]);
  });

  test("leaves a gap where a bucket has no responses to take a rate over", () => {
    expect(trendData(trends, "completion").map((d) => d.value)).toEqual([50, null]);
  });

  test("draws an element's metric with how many answered", () => {
    expect(trendData(trends, "element:nps").map((d) => [d.value, d.answered])).toEqual([
      [0, 2],
      [null, 0],
    ]);
  });

  test("an element the trend lacks has no values", () => {
    expect(trendData(trends, "element:gone").map((d) => d.value)).toEqual([null, null]);
  });

  test("draws the average total score with how many completed responses carry one", () => {
    expect(trendData(trends, "score").map((d) => [d.value, d.answered])).toEqual([
      [7.5, 1],
      [null, 0],
    ]);
    expect(trendData({ ...trends, score: null }, "score").map((d) => d.value)).toEqual([null, null]);
  });
});

describe("scales", () => {
  test("niceCeil rounds up to 1, 2 or 5 times a power of ten", () => {
    expect([0, 1, 3, 7, 10, 11, 42, 180].map(niceCeil)).toEqual([1, 1, 5, 10, 10, 20, 50, 200]);
  });

  test("a count scale has whole-number ticks", () => {
    expect(countScale(0)).toEqual({ min: 0, max: 1, ticks: [0, 1] });
    expect(countScale(37)).toEqual({ min: 0, max: 50, ticks: [0, 25, 50] });
  });

  test("the NPS spans -100 to 100, shares 0 to 100, averages 1 to the top rating", () => {
    expect(metricScale("nps", undefined, [])).toMatchObject({ min: -100, max: 100 });
    expect(metricScale("satisfied", 5, [])).toMatchObject({ min: 0, max: 100 });
    expect(metricScale("percentage", undefined, [])).toMatchObject({ min: 0, max: 100 });
    expect(metricScale("average", 7, [3.5])).toEqual({ min: 1, max: 7, ticks: [1, 4, 7] });
    // Without a range the largest value sets the top.
    expect(metricScale("average", undefined, [null, 8.4])).toMatchObject({ min: 1, max: 9 });
  });

  test("a slider's scale is its own, widened to a value off it", () => {
    expect(sliderScale(0, 100, [70, null])).toEqual({ min: 0, max: 100, ticks: [0, 50, 100] });
    expect(sliderScale(-5, 5, [8])).toEqual({ min: -5, max: 8, ticks: [-5, 1.5, 8] });
    // A scale without a span still has one to draw on.
    expect(sliderScale(3, 3, [])).toEqual({ min: 3, max: 4, ticks: [3, 3.5, 4] });
  });

  test("a score scale starts at zero, or below it for a negative score", () => {
    expect(scoreScale([7.5, null])).toEqual({ min: 0, max: 10, ticks: [0, 5, 10] });
    expect(scoreScale([])).toEqual({ min: 0, max: 1, ticks: [0, 0.5, 1] });
    expect(scoreScale([-3, 12])).toEqual({ min: -5, max: 20, ticks: [-5, 0, 20] });
    expect(scoreScale([-3])).toEqual({ min: -5, max: 0, ticks: [-5, -2.5, 0] });
  });
});

describe("labels", () => {
  test("a bucket is labelled by its day, or by its month, whatever the browser's zone", () => {
    const bucket = firstDay;
    expect(bucketLabel(bucket, "day", "en-US")).toBe("Mar 1");
    expect(bucketLabel(bucket, "month", "en-US")).toBe("Mar 2026");
    expect(bucketRangeLabel(bucket, "en-US")).toBe("Mar 1, 2026");
    expect(bucketRangeLabel({ ...bucket, end: "2026-03-07" }, "en-US")).toMatch(/^Mar 1\s*–\s*7, 2026$/);
  });

  test("values are written as their metric reads", () => {
    expect(formatTrendValue(33.33, "nps", "en-US")).toBe("+33");
    expect(formatTrendValue(-5, "nps", "en-US")).toBe("-5");
    expect(formatTrendValue(66.67, "percentage", "en-US")).toBe("67%");
    expect(formatTrendValue(4, "average", "en-US")).toBe("4.00");
    expect(formatTrendValue(1200, "count", "en-US")).toBe("1,200");
    expect(formatTrendValue(5.5, "tick", "en-US")).toBe("5.5");
    expect(formatTrendValue(3, "tick", "en-US")).toBe("3");
  });

  test("the x axis labels at most the given number of buckets", () => {
    expect([...labelledBuckets(5, 8)]).toEqual([0, 1, 2, 3, 4]);
    const many = labelledBuckets(30, 8);
    expect(many.size).toBeLessThanOrEqual(8);
    expect(many.has(0)).toBe(true);
  });
});

describe("tooltipPosition", () => {
  test("puts the tooltip beside the bucket's column, on the side with more room", () => {
    // The second of four buckets ends at half the plot: the tooltip starts right of it.
    expect(tooltipPosition(1, 4, 10)).toEqual({ left: "calc(50% + 0.5rem)", top: "10%" });
    // The third starts at half the plot: the tooltip ends left of it.
    expect(tooltipPosition(2, 4, 10)).toEqual({ right: "calc(50% + 0.5rem)", top: "10%" });
  });

  test("keeps level with the value and inside the plot", () => {
    // A value near the top, as a high NPS is, has the tooltip grow down from it; one near the bottom, up from it.
    expect(tooltipPosition(0, 4, 5)).toMatchObject({ top: "5%" });
    expect(tooltipPosition(0, 4, 80)).toMatchObject({ bottom: "20%" });
    expect(tooltipPosition(0, 4, null)).toMatchObject({ top: "0%" });
  });

  test("a lone bucket's tooltip stays inside the plot", () => {
    expect(tooltipPosition(0, 1, 10)).toEqual({ left: "0.5rem", top: "10%" });
  });
});
