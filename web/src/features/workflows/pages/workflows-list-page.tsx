import { useAutoAnimate } from "@formkit/auto-animate/react";
import { Link } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { readStorage, writeStorage } from "../../../lib/storage";
import { timeSince } from "../../../lib/time";
import { useDebouncedValue } from "../../../lib/use-debounced-value";
import { getWorkflowApiErrorMessage } from "../api";
import { WorkflowListActions } from "../components/workflow-list-actions";
import { WorkflowListToolbar } from "../components/workflow-list-toolbar";
import { WorkflowStatusPill } from "../components/workflow-status-pill";
import { WorkflowsListBodyLoading } from "../components/workflows-loading";
import { WorkflowsEmptyState } from "../components/workflows-empty-state";
import { useWorkflows } from "../hooks/queries";
import { useTrackWorkflowListFilters } from "../hooks/use-track-workflow-list-filters";
import { useTrackWorkflowSurface } from "../hooks/use-track-workflow-surface";
import { resolveWorkflowListSurface } from "../lib/analytics";
import {
  type TStoredWorkflowFilters,
  WORKFLOW_FILTERS_KEY,
  computeStatusIn,
  initialWorkflowFilters,
  parseStoredWorkflowFilters,
} from "../lib/list-filters";

/** forma2's WorkflowsListPage: the workspace's workflows under the filters this browser used last, a page at a time, with their actions. */
export const WorkflowsListPage = ({
  workspaceId,
  isReadOnly,
  workflowsPerPage,
}: Readonly<{ workspaceId: string; isReadOnly: boolean; workflowsPerPage: number }>) => {
  const { t, i18n } = useTranslation();
  const [animationParent] = useAutoAnimate();
  // The filters this browser used last; a stored value that cannot be read gives way to the defaults, which the write below then stores over it.
  const [filters, setFilters] = useState<TStoredWorkflowFilters>(
    () => parseStoredWorkflowFilters(readStorage(WORKFLOW_FILTERS_KEY)) ?? initialWorkflowFilters
  );
  useEffect(() => writeStorage(WORKFLOW_FILTERS_KEY, JSON.stringify(filters)), [filters]);

  const debouncedSearchValue = useDebouncedValue(filters.searchValue, 300);
  const statusIn = useMemo(() => computeStatusIn(filters.selectedStatuses), [filters.selectedStatuses]);

  // The stored filters are read before the first render, so they are settled from the start.
  useTrackWorkflowListFilters({
    isFilterInitialized: true,
    searchValue: filters.searchValue,
    debouncedSearchValue,
    selectedStatuses: filters.selectedStatuses,
    sortBy: filters.sortBy,
  });

  const {
    workflows,
    isLoading,
    isError,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
    queryKey,
  } = useWorkflows({
    workspaceId,
    limit: workflowsPerPage,
    nameContains: debouncedSearchValue.trim(),
    statusIn,
    sortBy: filters.sortBy,
  });

  const hasActiveFilters = filters.selectedStatuses.length > 0 || filters.searchValue.length > 0;

  // Reported once per screen the user lands on; null while loading or failed, neither of which is a visit. hasActiveFilters separates an empty workspace from an emptied filter.
  useTrackWorkflowSurface(
    resolveWorkflowListSurface({
      showInitialLoading: isLoading && workflows.length === 0,
      isError,
      hasActiveFilters,
      workflowCount: workflows.length,
    })
  );

  let listContent: React.ReactNode;
  if (isLoading && workflows.length === 0) {
    listContent = <WorkflowsListBodyLoading />;
  } else if (isError && workflows.length === 0) {
    listContent = (
      <div className="flex w-full flex-col items-center justify-center gap-4 py-16 text-slate-600">
        <p>{getWorkflowApiErrorMessage(error, t("common.something_went_wrong_please_try_again"))}</p>
        <Button variant="secondary" size="sm" onClick={() => void refetch()}>
          {t("common.try_again")}
        </Button>
      </div>
    );
  } else if (workflows.length === 0) {
    // The toolbar stays either way, so the filter that emptied the list stays reachable.
    listContent = <WorkflowsEmptyState filtered={hasActiveFilters} />;
  } else {
    listContent = (
      <div>
        <div ref={animationParent} className="space-y-3">
          <div className="mt-6 grid w-full grid-cols-7 place-items-center gap-3 px-6 pr-8 text-sm text-slate-800">
            <div className="col-span-2 place-self-start">{t("common.name")}</div>
            <div className="col-span-1">{t("common.status")}</div>
            <div className="col-span-1">{t("common.runs")}</div>
            <div className="col-span-1">{t("common.created_at")}</div>
            <div className="col-span-1">{t("common.updated_at")}</div>
            <div className="col-span-1">{t("common.created_by")}</div>
          </div>
          {workflows.map((workflow) => (
            <div key={workflow.id} className="relative block">
              <Link
                to="/workspaces/$workspaceId/workflows/$workflowId"
                params={{ workspaceId, workflowId: workflow.id }}
                className="grid w-full grid-cols-7 place-items-center gap-3 rounded-xl border border-slate-200 bg-white p-4 pr-8 shadow-xs transition-colors ease-in-out hover:border-slate-400">
                <div className="col-span-2 flex max-w-full items-center justify-self-start text-sm font-medium text-slate-900">
                  <div className="min-w-0 truncate">{workflow.name}</div>
                </div>
                <div className="col-span-1">
                  <WorkflowStatusPill status={workflow.status} />
                </div>
                <div className="col-span-1 text-sm text-slate-600">{workflow.runCount}</div>
                <div className="col-span-1 max-w-full truncate text-sm text-slate-600">
                  {timeSince(workflow.createdAt, i18n.language)}
                </div>
                <div className="col-span-1 max-w-full truncate text-sm text-slate-600">
                  {timeSince(workflow.updatedAt, i18n.language)}
                </div>
                <div className="col-span-1 max-w-full truncate text-sm text-slate-600">
                  {workflow.creator?.name ?? "-"}
                </div>
              </Link>
              <div className="absolute top-3.5 right-3">
                <WorkflowListActions
                  workflowId={workflow.id}
                  workflowName={workflow.name}
                  status={workflow.status}
                  workspaceId={workspaceId}
                  isReadOnly={isReadOnly}
                  queryKey={queryKey}
                />
              </div>
            </div>
          ))}
        </div>
        {hasNextPage ? (
          <div className="flex justify-center py-5">
            <Button
              onClick={() => void fetchNextPage()}
              variant="secondary"
              size="sm"
              loading={isFetchingNextPage}>
              {t("common.load_more")}
            </Button>
          </div>
        ) : null}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <WorkflowListToolbar filters={filters} onChange={setFilters} />
      {listContent}
    </div>
  );
};
