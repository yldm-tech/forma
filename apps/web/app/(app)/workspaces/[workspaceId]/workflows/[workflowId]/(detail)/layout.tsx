import { notFound } from "next/navigation";
import type { ReactNode } from "react";
import { WorkflowEditorProvider } from "@/modules/ee/workflows/components/workflow-editor-provider";
import { WorkflowHeaderCta } from "@/modules/ee/workflows/components/workflow-header-cta";
import { WorkflowPageTitle } from "@/modules/ee/workflows/components/workflow-page-title";
import { WorkflowSecondaryNavigation } from "@/modules/ee/workflows/components/workflow-secondary-navigation";
import { getWorkflowsRouteAuth } from "@/modules/ee/workflows/lib/auth";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";

const WorkflowDetailLayout = async (
  props: Readonly<{
    params: Promise<{ workspaceId: string; workflowId: string }>;
    children: ReactNode;
  }>
) => {
  const params = await props.params;
  const { isReadOnly, isWorkflowsEnabled } = await getWorkflowsRouteAuth(params.workspaceId);

  if (!isWorkflowsEnabled) {
    // Not entitled: the route does not exist for this installation, matching the list layout. Also
    // keeps WorkflowPageTitle/WorkflowHeaderCta/WorkflowEditorProvider from mounting — they fetch the
    // workflow through the now-403 API and would render broken states.
    notFound();
  }

  return (
    <WorkflowEditorProvider>
      {/* The editor fills the shell's scroll area instead of scrolling inside it: `h-full` pins this
          wrapper to that container's height and the flex column hands whatever is left below the
          header to the canvas + inspector row, which scroll internally. Height is therefore derived,
          never assumed — the alternative is subtracting a hardcoded guess at the chrome above. */}
      <PageContentWrapper className="flex h-full flex-col">
        <PageHeader
          pageTitle={<WorkflowPageTitle workflowId={params.workflowId} isReadOnly={isReadOnly} />}
          cta={<WorkflowHeaderCta workflowId={params.workflowId} isReadOnly={isReadOnly} />}>
          <WorkflowSecondaryNavigation workspaceId={params.workspaceId} workflowId={params.workflowId} />
        </PageHeader>
        {props.children}
      </PageContentWrapper>
    </WorkflowEditorProvider>
  );
};

export default WorkflowDetailLayout;
