import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { WorkspaceLanguagesPage } from "../../../../../../features/workspace-settings/languages";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/workspace/languages")({
  component: LanguagesSettings,
});

function LanguagesSettings() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <WorkspaceLanguagesPage workspace={workspace} />;
}
