import { z } from "zod";
import type { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type {
  DateRange,
  FilterValue,
  SelectedFilterValue,
} from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/components/response-filter-context";
import type { OptionsType } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/components/ElementsComboBox";
import { resolveDateRangePreset } from "@/lib/date-ranges";

/**
 * The single query param that carries the analysis filter, so a filtered Summary or Responses view can
 * be shared, bookmarked and survive a reload. One param rather than several because the filter is a
 * list of heterogeneous entries, not a fixed set of fields.
 *
 * Keys are one letter on purpose: a URL is a budget, and this param already carries every selected
 * option value of every multi-select. `filterComboBoxValue` for a multi-choice question with ten
 * selected options is the dominant term, so the envelope around it is kept as small as it can be
 * while staying readable in a pasted link.
 */
export const RESPONSE_FILTER_SEARCH_PARAM = "filter";

const ZEncodedFilterEntry = z.object({
  /** elementType.id */
  i: z.string().optional(),
  /** elementType.label */
  l: z.string().optional(),
  /** elementType.type — an `OptionsType` value. Left as a plain string: `getFormattedFilters` buckets only the types it knows and silently drops the rest, so an unknown value is inert rather than dangerous. */
  t: z.string().optional(),
  /** elementType.elementType — a `TSurveyElementTypeEnum` value, same reasoning as `t`. */
  e: z.string().optional(),
  /** filterType.filterValue */
  v: z.string().optional(),
  /** filterType.filterComboBoxValue */
  c: z.union([z.string(), z.array(z.string())]).optional(),
});

const ZEncodedResponseFilter = z.object({
  /** responseStatus, omitted when "all" */
  s: z.union([z.literal("complete"), z.literal("partial")]).optional(),
  /** dateRange — `f`rom / `t`o as ISO strings, `p`reset name when one produced the range */
  d: z
    .object({
      f: z.string(),
      t: z.string(),
      p: z.string().optional(),
    })
    .optional(),
  /** the filter entries */
  f: z.array(ZEncodedFilterEntry).optional(),
});

export interface TDecodedResponseFilter {
  selectedFilter: SelectedFilterValue;
  /** Absent when the URL carried no date range, so the caller leaves its own default in place. */
  dateRange?: DateRange;
}

/**
 * The state the provider starts in. A default date range only sets `to` (today) with no `from`, which
 * means "all time" and is not an active filter — the same test the two pages use to decide whether to
 * refetch. Nothing is written to the URL while this holds, so a bare link stays bare.
 */
export const isDefaultResponseFilterState = (
  selectedFilter: SelectedFilterValue,
  dateRange: DateRange
): boolean =>
  selectedFilter.filter.length === 0 && selectedFilter.responseStatus === "all" && !dateRange.from;

const toEncodedEntry = ({ elementType, filterType }: FilterValue) => ({
  ...(elementType.id === undefined ? {} : { i: elementType.id }),
  ...(elementType.label === undefined ? {} : { l: elementType.label }),
  ...(elementType.type === undefined ? {} : { t: elementType.type }),
  ...(elementType.elementType === undefined ? {} : { e: elementType.elementType }),
  ...(filterType.filterValue === undefined ? {} : { v: filterType.filterValue }),
  ...(filterType.filterComboBoxValue === undefined ? {} : { c: filterType.filterComboBoxValue }),
});

/**
 * Serialises the filter into the value of `?filter=`, or `null` when the state is the default one —
 * absence of the param is the clean state, which is what replaced the old `?referer=true` reset signal.
 */
export const encodeResponseFilterParam = (
  selectedFilter: SelectedFilterValue,
  dateRange: DateRange
): string | null => {
  const encoded: z.infer<typeof ZEncodedResponseFilter> = {};

  if (selectedFilter.responseStatus === "complete" || selectedFilter.responseStatus === "partial") {
    encoded.s = selectedFilter.responseStatus;
  }

  if (dateRange.from && dateRange.to) {
    encoded.d = {
      f: dateRange.from.toISOString(),
      t: dateRange.to.toISOString(),
      ...(dateRange.preset === undefined ? {} : { p: dateRange.preset }),
    };
  }

  if (selectedFilter.filter.length > 0) {
    encoded.f = selectedFilter.filter.map(toEncodedEntry);
  }

  if (Object.keys(encoded).length === 0) {
    return null;
  }

  return JSON.stringify(encoded);
};

const parseIsoDate = (value: string): Date | null => {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
};

/**
 * Reads `?filter=` back. Returns `null` for anything that is not a filter this build understands — a
 * truncated paste, a link from a future shape of this param, or hand-edited nonsense — so the view
 * falls back to unfiltered rather than rendering a broken filter bar.
 */
export const decodeResponseFilterParam = (raw: string): TDecodedResponseFilter | null => {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }

  const result = ZEncodedResponseFilter.safeParse(parsed);
  if (!result.success) {
    return null;
  }
  const encoded = result.data;

  const selectedFilter: SelectedFilterValue = {
    filter: (encoded.f ?? []).map((entry) => ({
      elementType: {
        ...(entry.i === undefined ? {} : { id: entry.i }),
        ...(entry.l === undefined ? {} : { label: entry.l }),
        ...(entry.t === undefined ? {} : { type: entry.t as OptionsType }),
        ...(entry.e === undefined ? {} : { elementType: entry.e as TSurveyElementTypeEnum }),
      },
      filterType: {
        filterValue: entry.v,
        filterComboBoxValue: entry.c,
      },
    })),
    responseStatus: encoded.s ?? "all",
  };

  if (!encoded.d) {
    return { selectedFilter };
  }

  const from = parseIsoDate(encoded.d.f);
  const to = parseIsoDate(encoded.d.t);
  if (!from || !to) {
    // A half-readable range would silently widen or narrow the window the link was meant to share.
    return { selectedFilter };
  }

  // The preset only labels the range; an unknown one is dropped and the explicit bounds still stand.
  // Normalised the way `resolveDateRangePreset` normalises before looking up, so a hand-edited
  // "Last 7 Days" is stored as the canonical key rather than a label nothing else will match.
  const presetKey = encoded.d.p?.toLowerCase().trim();
  const preset =
    presetKey !== undefined && resolveDateRangePreset(presetKey) !== null
      ? (presetKey as DateRange["preset"])
      : undefined;

  return {
    selectedFilter,
    dateRange: { from, to, ...(preset === undefined ? {} : { preset }) },
  };
};
