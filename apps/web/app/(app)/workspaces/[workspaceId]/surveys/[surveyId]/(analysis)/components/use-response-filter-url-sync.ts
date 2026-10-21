"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import type {
  DateRange,
  SelectedFilterValue,
} from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/components/response-filter-context";
import {
  RESPONSE_FILTER_SEARCH_PARAM,
  decodeResponseFilterParam,
  encodeResponseFilterParam,
} from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/lib/response-filter-url";

interface UseResponseFilterUrlSyncProps {
  selectedFilter: SelectedFilterValue;
  dateRange: DateRange;
  applyFilterFromUrl: (decoded: { selectedFilter: SelectedFilterValue; dateRange?: DateRange }) => void;
}

/**
 * Two-way sync between the analysis filter and the `?filter=` search param, so a filtered Summary or
 * Responses view is linkable and survives a reload.
 *
 * - On first hydrate a readable `?filter=` is applied to the provider's state; an unreadable one is
 *   ignored and the view stays unfiltered. A URL with no param leaves the default state untouched,
 *   which is what keeps a bare link cheap — no filter, no refetch.
 * - Afterwards state changes are mirrored with the native `history.replaceState`, which the Next App
 *   Router supports for shallow updates: the URL changes with no navigation and no server round trip.
 *   Replace rather than push because narrowing a filter is not a history the Back button should walk.
 */
export const useResponseFilterUrlSync = ({
  selectedFilter,
  dateRange,
  applyFilterFromUrl,
}: Readonly<UseResponseFilterUrlSyncProps>) => {
  const searchParams = useSearchParams();
  const pathname = usePathname();

  // State (not a ref) so the write-back effect below stays inert during the same render pass that
  // applies the initial param — otherwise it would wipe the param before the decoded state lands.
  const [hasAppliedInitialFilter, setHasAppliedInitialFilter] = useState(false);

  useEffect(() => {
    if (hasAppliedInitialFilter) return;
    setHasAppliedInitialFilter(true);

    const raw = searchParams?.get(RESPONSE_FILTER_SEARCH_PARAM);
    if (!raw) return;

    const decoded = decodeResponseFilterParam(raw);
    if (decoded) {
      applyFilterFromUrl(decoded);
    }
  }, [hasAppliedInitialFilter, searchParams, applyFilterFromUrl]);

  useEffect(() => {
    if (!hasAppliedInitialFilter) return;

    const url = new URL(window.location.href);
    const currentParam = url.searchParams.get(RESPONSE_FILTER_SEARCH_PARAM);
    const nextParam = encodeResponseFilterParam(selectedFilter, dateRange);
    if (currentParam === nextParam) return;

    if (nextParam) {
      url.searchParams.set(RESPONSE_FILTER_SEARCH_PARAM, nextParam);
    } else {
      url.searchParams.delete(RESPONSE_FILTER_SEARCH_PARAM);
    }
    // Preserve the router's history state — Next piggybacks internal data on the entry.
    window.history.replaceState(window.history.state, "", url.toString());
    // `pathname` is a trigger rather than a value read here: the provider lives in the `(analysis)`
    // layout and survives the Summary ⇄ Responses navigation, but that navigation drops the query
    // string, so the param has to be written again against the new path.
  }, [hasAppliedInitialFilter, selectedFilter, dateRange, pathname]);
};
