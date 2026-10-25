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
  trendData,
} from "./trends";

const firstDay: TrendBucket = {
  start: "2026-03-01",
  end: "2026-03-01",
  responses: 2,
  completed: 1,
  completionPercentage: 50,
};

const trends: SurveyTrends = {
  interval: "day",
  timeZone: "Asia/Tokyo",
  buckets: [
    firstDay,
    { start: "2026-03-02", end: "2026-03-02", responses: 0, completed: 0, completionPercentage: 0 },
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
