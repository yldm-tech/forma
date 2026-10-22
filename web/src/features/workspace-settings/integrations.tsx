import { Link } from "@tanstack/react-router";
import { Code2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { PageContentWrapper, PageHeader } from "../../components/ui/page";
import type { WorkspaceDetail } from "../../lib/queries/workspaces";

/**
 * forma2's integrations page as far as this server goes: the Javascript SDK card, whose status is the workspace's own. The third-party integration cards (Zapier, webhooks, Google Sheets, Airtable, Slack, n8n, Make, Notion, Activepieces) arrive with their integrations module.
 */
export const IntegrationsPage = ({ workspace }: { workspace: WorkspaceDetail }) => {
  const { t } = useTranslation();
  const connected = workspace.appSetupCompleted;
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.integrations")} />
      <div className="grid grid-cols-3 place-content-stretch gap-4 lg:grid-cols-3">
        <div className="relative rounded-xl border border-slate-200 bg-white p-4 text-left shadow-xs">
          <div className="absolute top-4 right-4 flex items-center rounded-sm bg-slate-100 px-2 py-1 text-xs text-slate-500">
            <span className="relative mr-1 flex size-2">
              {connected && (
                <span className="animate-ping-slow absolute inline-flex h-full w-full rounded-full bg-green-500 opacity-75" />
              )}
              <span
                className={`relative inline-flex size-2 rounded-full ${connected ? "bg-green-500" : "bg-slate-400"}`}
              />
            </span>
            {connected ? t("common.connected") : t("common.not_connected")}
          </div>
          <div className="mb-6 size-8 text-slate-700">
            <Code2Icon className="size-8" aria-hidden="true" />
          </div>
          <h3 className="text-lg font-bold text-slate-800">Javascript SDK</h3>
          <p className="text-xs text-slate-500">
            {t("workspace.integrations.website_or_app_integration_description")}
          </p>
          <div className="mt-4 flex gap-x-2">
            <Button size="sm" asChild>
              <Link
                to="/workspaces/$workspaceId/integrations/app-connection"
                params={{ workspaceId: workspace.id }}>
                {t("common.connect")}
              </Link>
            </Button>
            <Button size="sm" variant="secondary" asChild>
              <a href="https://forma.yldm.ai/docs/app-surveys/quickstart" target="_blank" rel="noreferrer">
                {t("common.docs")}
              </a>
            </Button>
          </div>
        </div>
      </div>
    </PageContentWrapper>
  );
};
