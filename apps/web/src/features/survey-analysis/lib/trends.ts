import type { ElementTrend, SurveyTrends, TrendBucket, TrendInterval, TrendMetric } from "../api";

// The trend card's arithmetic, kept apart from the component so it is tested on its own: which series a view shows, the y scale it is drawn on, and how a bucket and a value are written.

/** What the trend chart shows: the responses per bucket, their completion rate, or one score element's metric. */
export type TrendView = "responses" | "completion" | `element:${string}`;

/** One bar or point of the chart: the bucket, the value drawn (null leaves a gap), and for the responses view the completed part of the bar. */
export interface TrendDatum {
  bucket: TrendBucket;
  value: number | null;
  completed?: number;
  /** How many answers the value is taken over, for an element's metric. */
  answered?: number;
}

/** A y scale: the domain the plot spans and the values a grid line is drawn at. */
export interface TrendScale {
  min: number;
  max: number;
  ticks: number[];
}

/** The element trend a view names, if it names one the trend has. */
export const viewElement = (trends: SurveyTrends, view: TrendView): ElementTrend | undefined =>
  view.startsWith("element:")
    ? trends.elements.find((element) => element.elementId === view.slice("element:".length))
    : undefined;

/** The data the view draws, one per bucket. */
export const trendData = (trends: SurveyTrends, view: TrendView): TrendDatum[] => {
  if (view === "responses") {
    return trends.buckets.map((bucket) => ({ bucket, value: bucket.responses, completed: bucket.completed }));
  }
  if (view === "completion") {
    return trends.buckets.map((bucket) => ({
      bucket,
      value: bucket.responses > 0 ? bucket.completionPercentage : null,
    }));
  }
  const element = viewElement(trends, view);
  return trends.buckets.map((bucket, i) => ({
    bucket,
    value: element?.points[i]?.value ?? null,
    answered: element?.points[i]?.answered ?? 0,
  }));
};

/** The smallest of 1, 2 and 5 times a power of ten at or above value; 1 for nothing to show. */
export const niceCeil = (value: number): number => {
  if (!(value > 0)) return 1;
  const power = 10 ** Math.floor(Math.log10(value));
  const fraction = value / power;
  if (fraction <= 1) return power;
  if (fraction <= 2) return 2 * power;
  if (fraction <= 5) return 5 * power;
  return 10 * power;
};

/** The scale of a count: from zero to a round number at or above the largest count, with whole-number grid lines at the ends and the middle. */
export const countScale = (largest: number): TrendScale => {
  const max = niceCeil(largest);
  return { min: 0, max, ticks: Number.isInteger(max / 2) ? [0, max / 2, max] : [0, max] };
};

/** The scale of a metric: the NPS from -100 to 100, a share from 0 to 100 %, an average from 1 to the element's top rating (the largest value drawn where the element has no range). */
export const metricScale = (
  metric: TrendMetric | "percentage",
  range: number | undefined,
  values: (number | null)[]
): TrendScale => {
  if (metric === "nps") return { min: -100, max: 100, ticks: [-100, -50, 0, 50, 100] };
  if (metric === "satisfied" || metric === "percentage")
    return { min: 0, max: 100, ticks: [0, 25, 50, 75, 100] };
  const largest = Math.max(1, ...values.filter((v): v is number => v !== null));
  const top = Math.max(range ?? 0, Math.ceil(largest), 2);
  return { min: 1, max: top, ticks: [1, (1 + top) / 2, top] };
};

/** A local date of the trend ("2026-03-01") as the UTC midnight the formatters read it at, so the day never shifts with the browser's zone. */
const parseDay = (day: string): Date => new Date(`${day}T00:00:00Z`);

/** The short label under a bucket: its first day, or its month and year for a month. */
export const bucketLabel = (bucket: TrendBucket, interval: TrendInterval, locale: string): string =>
  new Intl.DateTimeFormat(
    locale,
    interval === "month"
      ? { month: "short", year: "numeric", timeZone: "UTC" }
      : { month: "short", day: "numeric", timeZone: "UTC" }
  ).format(parseDay(bucket.start));

/** The days a bucket covers, as its tooltip and table print them: one date, or the range from its first to its last day. */
export const bucketRangeLabel = (bucket: TrendBucket, locale: string): string => {
  const format = new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
  return bucket.start === bucket.end
    ? format.format(parseDay(bucket.start))
    : format.formatRange(parseDay(bucket.start), parseDay(bucket.end));
};

/** A value as the chart writes it: the NPS signed and rounded, a share rounded with a percent sign, an average to two decimals, a count as a whole number, and an axis tick of an average to one decimal at most. */
export const formatTrendValue = (
  value: number,
  metric: TrendMetric | "percentage" | "count" | "tick",
  locale: string
): string => {
  switch (metric) {
    case "nps":
      return new Intl.NumberFormat(locale, { maximumFractionDigits: 0, signDisplay: "exceptZero" }).format(
        Math.round(value)
      );
    case "satisfied":
    case "percentage":
      return new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 0 }).format(
        value / 100
      );
    case "average":
      return new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(
        value
      );
    case "tick":
      return new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value);
    default:
      return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(value);
  }
};

/** Which bucket labels the x axis prints so they do not crowd: every one up to maxLabels, else every n-th, and the last one too when it is far enough from the one before. */
export const labelledBuckets = (count: number, maxLabels: number): Set<number> => {
  const step = Math.max(1, Math.ceil(count / Math.max(1, maxLabels)));
  const out = new Set<number>();
  for (let i = 0; i < count; i += step) out.add(i);
  if (count > 0 && !out.has(count - 1) && (count - 1) % step > step / 2) out.add(count - 1);
  return out;
};
