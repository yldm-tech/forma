import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";
import { WorkflowBuilderPage } from "../../../../../../features/workflows/pages/workflow-builder-page";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

// forma2's workflow builder, /workspaces/{id}/workflows/{workflowId}.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/workflows/$workflowId/")({
  component: WorkflowBuilderRoute,
});

function WorkflowBuilderRoute() {
  const { workspaceId, workflowId } = Route.useParams();
  const { node } = Route.useSearch();
  const navigate = useNavigate();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const setNodeParam = useCallback(
    (nodeId: string | null) =>
      void navigate({
        to: ".",
        search: (prev: { new?: true; node?: string }) => {
          const { node: _node, ...rest } = prev;
          return nodeId ? { ...rest, node: nodeId } : rest;
        },
        replace: true,
      }),
    [navigate]
  );
  return (
    <WorkflowBuilderPage
      workspaceId={workspaceId}
      workflowId={workflowId}
      isReadOnly={workspace.access.isReadOnly}
      canManage={workspace.access.canManage}
      nodeParam={node ?? null}
      setNodeParam={setNodeParam}
    />
  );
}
