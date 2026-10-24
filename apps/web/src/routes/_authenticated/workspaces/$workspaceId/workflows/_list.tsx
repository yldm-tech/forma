import { useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, useNavigate, useRouterState } from "@tanstack/react-router";
import { PlusIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../../components/ui/button";
import { PageContentWrapper, PageHeader } from "../../../../../components/ui/page";
import { SecondaryNavigation } from "../../../../../components/ui/secondary-navigation";
import { getWorkflowApiErrorMessage } from "../../../../../features/workflows/api";
import { useCreateWorkflow } from "../../../../../features/workflows/hooks/queries";
import { createEmptyWorkflowDefinition } from "../../../../../features/workflows/lib/default-workflow";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";
import { toast } from "../../../../../lib/toast";

// forma2's workflows/(list) layout: the workspace's workflows and their runs under one header.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/workflows/_list")({
  component: WorkflowsListLayout,
});

/** No dialog: the draft is created at once with a default name and an empty canvas, and the editor opens with the title selected for renaming. */
const NewWorkflowButton = ({ workspaceId }: { workspaceId: string }) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const createWorkflowMutation = useCreateWorkflow();
  const handleCreate = () => {
    if (createWorkflowMutation.isPending) return;
    createWorkflowMutation.mutate(
      {
        workspaceId,
        name: t("common.new_workflow"),
        description: null,
        status: "draft",
        definition: createEmptyWorkflowDefinition(),
      },
      {
        onSuccess: (workflow) =>
          void navigate({
            to: "/workspaces/$workspaceId/workflows/$workflowId",
            params: { workspaceId, workflowId: workflow.id },
            search: { new: true },
          }),
        onError: (error) =>
          toast.error(getWorkflowApiErrorMessage(error, t("workspace.workflows.create_failed"))),
      }
    );
  };
  return (
    <Button type="button" size="sm" loading={createWorkflowMutation.isPending} onClick={handleCreate}>
      {t("common.new_workflow")}
      <PlusIcon />
    </Button>
  );
};

function WorkflowsListLayout() {
  const { t } = useTranslation();
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const isRunsTab = pathname.replace(/\/$/, "").endsWith("/workflows/runs");
  const base = `/workspaces/${workspaceId}/workflows`;
  return (
    <PageContentWrapper>
      <PageHeader
        pageTitle={t("common.workflows")}
        cta={
          isRunsTab || workspace.access.isReadOnly ? null : <NewWorkflowButton workspaceId={workspaceId} />
        }>
        <SecondaryNavigation
          ariaLabel={t("common.workflows")}
          activeId={isRunsTab ? "runs" : "workflows"}
          navigation={[
            { id: "workflows", label: t("common.workflows"), href: base },
            { id: "runs", label: t("common.runs"), href: `${base}/runs` },
          ]}
        />
      </PageHeader>
      <Outlet />
    </PageContentWrapper>
  );
}
