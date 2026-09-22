"use client";

import { type JSX, useState } from "react";
import { useTranslation } from "react-i18next";
import { Webhook } from "@forma/database/prisma-browser";
import { TSurvey } from "@forma/types/surveys/types";
import { WebhookModal } from "@/modules/integrations/webhooks/components/webhook-detail-modal";
import { EmptyState } from "@/modules/ui/components/empty-state";

// The settings surface never receives the signing secret — see getWebhooks in lib/webhook.ts.
type WebhookWithoutSecret = Omit<Webhook, "secret">;

interface WebhookTableProps {
  workspaceId: string;
  webhooks: WebhookWithoutSecret[];
  surveys: TSurvey[];
  children: [JSX.Element, JSX.Element[]];
  isReadOnly: boolean;
  allowInternalUrls: boolean;
}

export const WebhookTable = ({
  workspaceId,
  webhooks,
  surveys,
  children: [TableHeading, webhookRows],
  isReadOnly,
  allowInternalUrls,
}: WebhookTableProps) => {
  const [isWebhookDetailModalOpen, setWebhookDetailModalOpen] = useState(false);
  const { t } = useTranslation();
  const [activeWebhook, setActiveWebhook] = useState<WebhookWithoutSecret>({
    workspaceId,
    id: "",
    name: "",
    url: "",
    source: "user",
    triggers: [],
    surveyIds: [],
    createdAt: new Date(),
    updatedAt: new Date(),
  });

  const handleOpenWebhookDetailModalClick = (e: React.MouseEvent, webhook: WebhookWithoutSecret) => {
    e.preventDefault();
    setActiveWebhook(webhook);
    setWebhookDetailModalOpen(true);
  };

  return (
    <>
      {webhooks.length === 0 ? (
        <EmptyState text={t("workspace.integrations.webhooks.empty_webhook_message")} />
      ) : (
        <div className="rounded-lg border border-slate-200">
          {TableHeading}
          <div className="grid-cols-7">
            {webhooks.map((webhook, index) => (
              <button
                type="button"
                onClick={(e) => {
                  handleOpenWebhookDetailModalClick(e, webhook);
                }}
                className="w-full"
                key={webhook.id}>
                {webhookRows[index]}
              </button>
            ))}
          </div>
        </div>
      )}
      <WebhookModal
        open={isWebhookDetailModalOpen}
        setOpen={setWebhookDetailModalOpen}
        webhook={activeWebhook}
        surveys={surveys}
        isReadOnly={isReadOnly}
        allowInternalUrls={allowInternalUrls}
      />
    </>
  );
};
