import { notFound } from "next/navigation";
import type { ReactNode } from "react";
import { getTranslate } from "@/lingodotdev/server";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";
import { WorkspaceWorkflowsHeaderCta } from "@/modules/workflows/components/workspace-workflows-header-cta";
import { WorkspaceWorkflowsSecondaryNavigation } from "@/modules/workflows/components/workspace-workflows-secondary-navigation";
import { getWorkflowsRouteAuth } from "@/modules/workflows/lib/auth";
import { WorkflowsQueryClientProvider } from "./query-client-provider";

const WorkspaceWorkflowsLayout = async (
  props: Readonly<{ params: Promise<{ workspaceId: string }>; children: ReactNode }>
) => {
  const params = await props.params;
  const { isReadOnly, isWorkflowsEnabled } = await getWorkflowsRouteAuth(params.workspaceId);
  const t = await getTranslate();

  if (!isWorkflowsEnabled) {
    // Not entitled: this installation does not have the feature, so the route does not exist for it.
    // The nav already omits the entry, which leaves old links and bookmarks — and answering those
    // with a page whose only content is an upsell reads as a broken product rather than a smaller
    // one. The client pages must not mount either: they fetch through the now-403 workflows API.
    notFound();
  }

  return (
    <WorkflowsQueryClientProvider>
      <PageContentWrapper>
        <PageHeader
          pageTitle={t("common.workflows")}
          cta={<WorkspaceWorkflowsHeaderCta workspaceId={params.workspaceId} isReadOnly={isReadOnly} />}>
          <WorkspaceWorkflowsSecondaryNavigation workspaceId={params.workspaceId} />
        </PageHeader>
        {props.children}
      </PageContentWrapper>
    </WorkflowsQueryClientProvider>
  );
};

export default WorkspaceWorkflowsLayout;
