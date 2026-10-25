import { type DateRange, type FilterValue, OptionsType, type SelectedFilterValue } from "./filter-types";

/** How many conditions the applied filter holds: its rows and, unless it is "all", the response status. The Filter trigger shows this, so a row added in the popover but not applied does not count; the keyword search has its own box and is not among them. */
export const countAppliedFilters = (filter: SelectedFilterValue): number =>
  filter.filter.length + (filter.responseStatus === "all" ? 0 : 1);

/** Whether the views show a filtered subset: a condition is applied, a keyword is searched for or the date range has a start. An empty result then means "nothing matches", not "nothing yet". */
export const hasActiveFilters = (filter: SelectedFilterValue, dateRange: DateRange): boolean =>
  countAppliedFilters(filter) > 0 || Boolean(filter.keyword?.trim()) || dateRange.from !== undefined;

/** The rows after a summary click on an element: the click's row replaces every row on that element, in the place of the first, since a range left beside the clicked value (rating > 2 and < 8, then a click on 9) would match nothing. replaced says whether the element had a row before. */
export const withSummaryFilterRow = (
  rows: FilterValue[],
  row: FilterValue
): { rows: FilterValue[]; replaced: boolean } => {
  const isOnElement = (existing: FilterValue) =>
    existing.elementType.type === OptionsType.ELEMENTS && existing.elementType.id === row.elementType.id;
  const first = rows.findIndex(isOnElement);
  if (first === -1) return { rows: [...rows, row], replaced: false };
  return {
    rows: rows.flatMap((existing, index) => {
      if (index === first) return [row];
      return isOnElement(existing) ? [] : [existing];
    }),
    replaced: true,
  };
};
