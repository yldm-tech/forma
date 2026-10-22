import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { WORKFLOWS_PER_PAGE } from "../../../../../../features/workflows/lib/list-filters";
import { WorkflowsListPage } from "../../../../../../features/workflows/pages/workflows-list-page";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

// forma2's workflows list, /workspaces/{id}/workflows.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/workflows/_list/")({
  component: WorkflowsPage,
});

function WorkflowsPage() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return (
    <WorkflowsListPage
      key={workspaceId}
      workspaceId={workspaceId}
      isReadOnly={workspace.access.isReadOnly}
      workflowsPerPage={WORKFLOWS_PER_PAGE}
    />
  );
}
