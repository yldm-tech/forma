import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { PageContentWrapper, PageHeader } from "../../components/ui/page";
import type { WorkspaceDetail } from "../../lib/queries/workspaces";
import { type IntegrationType, integrationsQuery, webhooksQuery } from "../integrations/api";
import activepiecesLogo from "../integrations/images/activepieces.webp";
import airtableLogo from "../integrations/images/airtable.svg";
import googleSheetsLogo from "../integrations/images/google-sheets.png";
import javascriptLogo from "../integrations/images/javascript.png";
import makeLogo from "../integrations/images/make.png";
import n8nLogo from "../integrations/images/n8n.png";
import notionLogo from "../integrations/images/notion.png";
import slackLogo from "../integrations/images/slack.png";
import webhookLogo from "../integrations/images/webhook.png";
import zapierLogo from "../integrations/images/zapier.png";
import { countStatusText, webhookCounts } from "../integrations/webhooks";

interface CardProps {
  label: string;
  description: string;
  icon: ReactNode;
  /** A page of this app, or a page elsewhere, which opens in a new tab. */
  connect: { to: string; text: string } | { href: string; text: string };
  connected: boolean;
  statusText: string;
  disabled: boolean;
}

/** forma2's integration Card: what the integration does, whether it is connected, and where to set it up. */
const IntegrationCard = ({
  label,
  description,
  icon,
  connect,
  connected,
  statusText,
  disabled,
}: CardProps) => {
  return (
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
        {statusText}
      </div>
      <div className="mb-6 size-8">{icon}</div>
      <h3 className="text-lg font-bold text-slate-800">{label}</h3>
      <p className="text-xs text-slate-500">{description}</p>
      <div className="mt-4 flex gap-x-2">
        {disabled ? (
          <Button size="sm" disabled>
            {connect.text}
          </Button>
        ) : (
          <Button size="sm" asChild>
            {"to" in connect ? (
              <Link to={connect.to}>{connect.text}</Link>
            ) : (
              <a href={connect.href} target="_blank" rel="noreferrer">
                {connect.text}
              </a>
            )}
          </Button>
        )}
      </div>
    </div>
  );
};

const logo = (src: string, alt: string) => <img src={src} alt={alt} className="size-8 object-contain" />;

/** forma2's integrations page: the Javascript SDK, webhooks, the four OAuth integrations, and the automation platforms that subscribe webhooks through the v1 API, with how many each has. Read-only members see the cards but cannot open them, except webhooks and the SDK. */
export const IntegrationsPage = ({ workspace }: { workspace: WorkspaceDetail }) => {
  const { t } = useTranslation();
  const integrations = useQuery(integrationsQuery(workspace.id));
  const webhooks = useQuery(webhooksQuery(workspace.id));
  const counts = webhookCounts(webhooks.data?.webhooks ?? []);
  const isReadOnly = workspace.access.isReadOnly;
  const base = `/workspaces/${workspace.id}/integrations`;

  const isConnected = (type: IntegrationType) =>
    !!integrations.data?.integrations.some((integration) => integration.type === type);
  const oauthCard = (
    type: IntegrationType,
    slug: string,
    label: string,
    description: string,
    icon: ReactNode
  ): CardProps => ({
    label,
    description,
    icon,
    connect: { to: `${base}/${slug}`, text: isConnected(type) ? t("common.manage") : t("common.connect") },
    connected: isConnected(type),
    statusText: isConnected(type) ? t("common.connected") : t("common.not_connected"),
    disabled: isReadOnly,
  });

  const cards: CardProps[] = [
    {
      label: "Javascript SDK",
      description: t("workspace.integrations.website_or_app_integration_description"),
      icon: logo(javascriptLogo, "Javascript Logo"),
      connect: { to: `${base}/app-connection`, text: t("common.connect") },
      connected: workspace.appSetupCompleted,
      statusText: workspace.appSetupCompleted ? t("common.connected") : t("common.not_connected"),
      disabled: false,
    },
    {
      label: "Zapier",
      description: t("workspace.integrations.zapier_integration_description"),
      icon: logo(zapierLogo, "Zapier Logo"),
      connect: { href: "https://zapier.com/apps/forma/integrations", text: t("common.connect") },
      connected: counts.zapier > 0,
      statusText: countStatusText(t, counts.zapier, "zap"),
      disabled: isReadOnly,
    },
    {
      label: "Webhooks",
      description: t("workspace.integrations.webhook_integration_description"),
      icon: logo(webhookLogo, "Webhook Logo"),
      connect: { to: `${base}/webhooks`, text: t("workspace.integrations.manage_webhooks") },
      connected: counts.user > 0,
      statusText: countStatusText(t, counts.user, "webhook"),
      disabled: false,
    },
    oauthCard(
      "googleSheets",
      "google-sheets",
      "Google Sheets",
      t("workspace.integrations.google_sheet_integration_description"),
      logo(googleSheetsLogo, "Google sheets Logo")
    ),
    oauthCard(
      "airtable",
      "airtable",
      "Airtable",
      t("workspace.integrations.airtable_integration_description"),
      logo(airtableLogo, "Airtable Logo")
    ),
    oauthCard(
      "slack",
      "slack",
      "Slack",
      t("workspace.integrations.slack_integration_description"),
      logo(slackLogo, "Slack Logo")
    ),
    {
      label: "n8n",
      description: t("workspace.integrations.n8n_integration_description"),
      icon: logo(n8nLogo, "n8n Logo"),
      // forma2 labelled this card from an n8n integration row that nothing ever creates, so it always read Connect.
      connect: { href: "https://n8n.io", text: t("common.connect") },
      connected: counts.n8n > 0,
      statusText: countStatusText(t, counts.n8n, t("common.integration")),
      disabled: isReadOnly,
    },
    {
      label: "Make.com",
      description: t("workspace.integrations.make_integration_description"),
      icon: logo(makeLogo, "Make Logo"),
      connect: { href: "https://www.make.com/en/integrations/forma", text: t("common.connect") },
      connected: counts.make > 0,
      statusText: countStatusText(t, counts.make, t("common.integration")),
      disabled: isReadOnly,
    },
    oauthCard(
      "notion",
      "notion",
      "Notion",
      t("workspace.integrations.notion_integration_description"),
      logo(notionLogo, "Notion Logo")
    ),
    {
      label: "Activepieces",
      description: t("workspace.integrations.activepieces_integration_description"),
      icon: logo(activepiecesLogo, "ActivePieces Logo"),
      connect: { href: "https://www.activepieces.com/pieces/forma", text: t("common.connect") },
      connected: counts.activepieces > 0,
      statusText: countStatusText(t, counts.activepieces, t("common.integration")),
      disabled: isReadOnly,
    },
  ];

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.integrations")} />
      <div className="grid grid-cols-3 place-content-stretch gap-4 lg:grid-cols-3">
        {cards.map((card) => (
          <IntegrationCard key={card.label} {...card} />
        ))}
      </div>
    </PageContentWrapper>
  );
};
