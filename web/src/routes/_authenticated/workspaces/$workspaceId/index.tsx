import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { PageContentWrapper, PageHeader } from "../../../../components/ui/page";
import { workspaceQuery } from "../../../../lib/queries/workspaces";

// The workspace's landing page. Its content, the survey list, belongs to the survey module.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/")({
  component: WorkspaceHome,
});

function WorkspaceHome() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={workspace.name} />
    </PageContentWrapper>
  );
}
