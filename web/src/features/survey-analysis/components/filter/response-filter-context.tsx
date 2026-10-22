import type { TResponseFilterCriteria } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import { useLocation, useRouter } from "@tanstack/react-router";
import {
  type Dispatch,
  type ReactNode,
  type SetStateAction,
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import type { DateRange, SelectedFilterValue } from "../../lib/filter-types";
import { getFormattedFilters, getTodayDate } from "../../lib/filters";
import {
  RESPONSE_FILTER_SEARCH_PARAM,
  decodeResponseFilter,
  encodeResponseFilter,
} from "../../lib/response-filter-url";

interface ResponseFilterContextValue {
  selectedFilter: SelectedFilterValue;
  setSelectedFilter: Dispatch<SetStateAction<SelectedFilterValue>>;
  dateRange: DateRange;
  setDateRange: Dispatch<SetStateAction<DateRange>>;
  resetState: () => void;
  /** The server's criteria for the current filter, shared by the summary, the response list, the count and the filtered export. */
  filterCriteria: TResponseFilterCriteria;
}

const ResponseFilterContext = createContext<ResponseFilterContextValue | undefined>(undefined);

const DEFAULT_FILTER: SelectedFilterValue = { filter: [], responseStatus: "all" };

const defaultDateRange = (): DateRange => ({ from: undefined, to: getTodayDate() });

/** The filter a link opened the view with, read once, before the first render, so a filtered link fetches its filtered data straight away. */
const initialFilterFromSearch = (search: Record<string, unknown>) =>
  decodeResponseFilter(search[RESPONSE_FILTER_SEARCH_PARAM]);

/**
 * forma2's ResponseFilterProvider: the filter the summary and responses views share, kept in the `?filter=` search param so a filtered view is linkable and survives a reload. It lives in the survey's layout route, so it survives the Summary ⇄ Responses navigation; that navigation drops the query string, which is why the param is written again whenever the path changes. Writes replace the history entry: narrowing a filter is not a history the Back button should walk.
 */
export const ResponseFilterProvider = ({ survey, children }: { survey: TSurvey; children: ReactNode }) => {
  const router = useRouter();
  const location = useLocation();
  const [initial] = useState(() => initialFilterFromSearch(location.search as Record<string, unknown>));
  const [selectedFilter, setSelectedFilter] = useState<SelectedFilterValue>(
    initial?.selectedFilter ?? DEFAULT_FILTER
  );
  const [dateRange, setDateRange] = useState<DateRange>(initial?.dateRange ?? defaultDateRange);

  const resetState = useCallback(() => {
    setDateRange(defaultDateRange());
    setSelectedFilter(DEFAULT_FILTER);
  }, []);

  const search = location.search as Record<string, unknown>;
  const currentParam = JSON.stringify(search[RESPONSE_FILTER_SEARCH_PARAM] ?? null);
  useEffect(() => {
    const next = encodeResponseFilter(selectedFilter, dateRange);
    if (JSON.stringify(next) === currentParam) return;
    const nextSearch = { ...search, [RESPONSE_FILTER_SEARCH_PARAM]: next ?? undefined };
    void router.navigate({
      href: `${location.pathname}${router.options.stringifySearch(nextSearch)}`,
      replace: true,
    });
    // search is read for the other params only; the param itself is compared through currentParam.
    // oxlint-disable-next-line react/exhaustive-deps -- see above
  }, [selectedFilter, dateRange, currentParam, location.pathname, router]);

  const filterCriteria = useMemo(
    () => getFormattedFilters(survey, selectedFilter, dateRange),
    [survey, selectedFilter, dateRange]
  );

  const value = useMemo(
    () => ({ selectedFilter, setSelectedFilter, dateRange, setDateRange, resetState, filterCriteria }),
    [selectedFilter, dateRange, resetState, filterCriteria]
  );

  return <ResponseFilterContext.Provider value={value}>{children}</ResponseFilterContext.Provider>;
};

export const useResponseFilter = (): ResponseFilterContextValue => {
  const context = useContext(ResponseFilterContext);
  if (context === undefined) {
    throw new Error("useResponseFilter must be used within a ResponseFilterProvider");
  }
  return context;
};
