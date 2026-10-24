import { useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, useNavigate, useRouterState } from "@tanstack/react-router";
import { Provider as JotaiProvider } from "jotai";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { PageContentWrapper, PageHeader } from "../../../../../../components/ui/page";
import { SecondaryNavigation } from "../../../../../../components/ui/secondary-navigation";
import { WorkflowHeaderCta } from "../../../../../../features/workflows/components/workflow-header-cta";
import { WorkflowPageTitle } from "../../../../../../features/workflows/components/workflow-page-title";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

export interface WorkflowSearch {
  /** The workflow was just created: the editor selects its title for naming. */
  new?: true;
  /** The node whose config the inspector shows. */
  node?: string;
}

// forma2's workflows/[workflowId]/(detail) layout: the editor and the workflow's runs under a header whose title edits the name and whose menu changes the status. The editor state lives here, so it survives switching between the two tabs.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/workflows/$workflowId")({
  validateSearch: (search: Record<string, unknown>): WorkflowSearch => ({
    ...(search.new === true || search.new === 1 || search.new === "1" ? { new: true as const } : {}),
    ...(typeof search.node === "string" && search.node ? { node: search.node } : {}),
  }),
  component: WorkflowDetailLayout,
});

function WorkflowDetailLayout() {
  const { workspaceId, workflowId } = Route.useParams();
  return (
    // Keyed by workflow, so one workflow's editor state never shows on another's page.
    <JotaiProvider key={workflowId}>
      <WorkflowDetail workspaceId={workspaceId} workflowId={workflowId} />
    </JotaiProvider>
  );
}

function WorkflowDetail({ workspaceId, workflowId }: { workspaceId: string; workflowId: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const search = Route.useSearch();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const isReadOnly = workspace.access.isReadOnly;
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const isEditTab = !pathname.replace(/\/$/, "").endsWith("/runs");
  const base = `/workspaces/${workspaceId}/workflows/${workflowId}`;

  const consumeNew = useCallback(
    () => void navigate({ to: ".", search: ({ new: _new, ...rest }: WorkflowSearch) => rest, replace: true }),
    [navigate]
  );

  return (
    // The editor fills the shell's scroll area: h-full pins this column to it and the flex column hands what is left below the header to the canvas and inspector, which scroll inside.
    <PageContentWrapper className="flex h-full flex-col">
      <PageHeader
        pageTitle={
          <WorkflowPageTitle
            workflowId={workflowId}
            isReadOnly={isReadOnly}
            isEditTab={isEditTab}
            isNew={search.new === true}
            onNewConsumed={consumeNew}
          />
        }
        cta={<WorkflowHeaderCta workflowId={workflowId} isReadOnly={isReadOnly} isEditTab={isEditTab} />}>
        <SecondaryNavigation
          ariaLabel={t("common.workflows")}
          activeId={isEditTab ? "builder" : "runs"}
          navigation={[
            { id: "builder", label: t("common.editor"), href: base },
            { id: "runs", label: t("common.runs"), href: `${base}/runs` },
          ]}
        />
      </PageHeader>
      <Outlet />
    </PageContentWrapper>
  );
}
