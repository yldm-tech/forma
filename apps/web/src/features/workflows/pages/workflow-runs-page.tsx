import type { TWorkflowRunListFilters } from "../api";
import { WorkflowRunsTable } from "../components/runs/workflow-runs-table";
import { useWorkflowRuns } from "../hooks/queries";
import { useTrackWorkflowSurface } from "../hooks/use-track-workflow-surface";

/** forma2's RUNS_PER_PAGE. */
const RUNS_PER_PAGE = 20;

/** forma2's WorkflowRunsPage and WorkspaceWorkflowRunsPage: one workflow's runs, or with no workflowId the workspace's, which name their workflow. */
export const WorkflowRunsPage = ({
  workspaceId,
  workflowId,
}: Readonly<{ workspaceId: string; workflowId?: string }>) => {
  const filters: TWorkflowRunListFilters | undefined = workflowId ? { workflowId } : undefined;
  const query = useWorkflowRuns({ workspaceId, limit: RUNS_PER_PAGE, filters });
  useTrackWorkflowSurface(workflowId ? "workflow_runs" : "workspace_runs", { workflowId });
  return (
    <WorkflowRunsTable
      runs={query.runs}
      showWorkflowColumn={!workflowId}
      isLoading={query.isLoading}
      isError={query.isError}
      error={query.error}
      onRetry={() => void query.refetch()}
      hasNextPage={query.hasNextPage}
      isFetchingNextPage={query.isFetchingNextPage}
      isFetchNextPageError={query.isFetchNextPageError}
      onLoadMore={() => void query.fetchNextPage()}
    />
  );
};
