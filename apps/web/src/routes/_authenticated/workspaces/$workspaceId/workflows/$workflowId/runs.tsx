import { createFileRoute } from "@tanstack/react-router";
import { WorkflowRunsPage } from "../../../../../../features/workflows/pages/workflow-runs-page";

// forma2's workflow runs, /workspaces/{id}/workflows/{workflowId}/runs.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/workflows/$workflowId/runs")({
  component: WorkflowRunsRoute,
});

function WorkflowRunsRoute() {
  const { workspaceId, workflowId } = Route.useParams();
  return <WorkflowRunsPage workspaceId={workspaceId} workflowId={workflowId} />;
}
