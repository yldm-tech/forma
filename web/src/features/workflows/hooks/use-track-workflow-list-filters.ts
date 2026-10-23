import { useEffect, useRef } from "react";
import type { TWorkflowSortBy, TWorkflowStatus } from "../definition";
import { WORKFLOW_CLIENT_EVENTS, trackWorkflowEvent } from "../lib/analytics";

interface WorkflowListFilterState {
  isFilterInitialized: boolean;
  searchValue: string;
  debouncedSearchValue: string;
  selectedStatuses: TWorkflowStatus[];
  sortBy: TWorkflowSortBy;
}

/**
 * forma2's useTrackWorkflowListFilters: one workflow_list_filtered per settled change of the list toolbar. It skips the stored filters the page starts with (they are not a user action) and waits for the search debounce to catch up, so typing reports once per pause rather than per keystroke.
 */
export const useTrackWorkflowListFilters = ({
  isFilterInitialized,
  searchValue,
  debouncedSearchValue,
  selectedStatuses,
  sortBy,
}: WorkflowListFilterState): void => {
  const hasSettledInitialFiltersRef = useRef(false);

  useEffect(() => {
    if (!isFilterInitialized || debouncedSearchValue !== searchValue) return;
    if (!hasSettledInitialFiltersRef.current) {
      hasSettledInitialFiltersRef.current = true;
      return;
    }
    trackWorkflowEvent(WORKFLOW_CLIENT_EVENTS.listFiltered, {
      has_search: searchValue.trim().length > 0,
      status_filter: [...selectedStatuses].sort((a, b) => a.localeCompare(b)),
      sort_by: sortBy,
      includes_archived: selectedStatuses.includes("archived"),
    });
  }, [isFilterInitialized, searchValue, debouncedSearchValue, selectedStatuses, sortBy]);
};
