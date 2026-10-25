import type { DateRange, SelectedFilterValue } from "./filter-types";

/** How many conditions the applied filter holds: its rows and, unless it is "all", the response status. The Filter trigger shows this, so a row added in the popover but not applied does not count; the keyword search has its own box and is not among them. */
export const countAppliedFilters = (filter: SelectedFilterValue): number =>
  filter.filter.length + (filter.responseStatus === "all" ? 0 : 1);

/** Whether the views show a filtered subset: a condition is applied, a keyword is searched for or the date range has a start. An empty result then means "nothing matches", not "nothing yet". */
export const hasActiveFilters = (filter: SelectedFilterValue, dateRange: DateRange): boolean =>
  countAppliedFilters(filter) > 0 || Boolean(filter.keyword?.trim()) || dateRange.from !== undefined;
