import { createFileRoute } from "@tanstack/react-router";
import { WorkflowRunsPage } from "../../../../../../features/workflows/pages/workflow-runs-page";

// forma2's workspace runs, /workspaces/{id}/workflows/runs: every workflow's runs, newest first.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/workflows/_list/runs")({
  component: WorkspaceWorkflowRunsRoute,
});

function WorkspaceWorkflowRunsRoute() {
  const { workspaceId } = Route.useParams();
  return <WorkflowRunsPage workspaceId={workspaceId} />;
}
