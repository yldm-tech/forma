"use client";

import Link from "next/link";
import { useTranslation } from "react-i18next";
import {
  getAIUnavailableAction,
  getAIUnavailableActionLabel,
  getAIUnavailableMessage,
} from "@/lib/ai/availability";
import type { TAIUnavailableReason } from "@/lib/ai/service";
import { capturePostHogClientEvent } from "@/lib/posthog/client";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "@/modules/ui/components/alert";
import { useWorkspace } from "@/modules/workspaces/context/workspace-context";

interface AIUnavailableAlertProps {
  /** Names the blocked capability, e.g. "AI chart generation". The reason copy is shared. */
  title: string;
  reason?: TAIUnavailableReason;
  /** Distinguishes the surface in the `upgrade_cta_clicked` PostHog event. */
  feature: string;
}

/**
 * The one way to say "AI smart tools are unavailable here". Every surface that gates on AI shows this
 * same alert, so the reason copy and the action behind it are resolved once (`lib/ai/availability`)
 * instead of per feature.
 */
export const AIUnavailableAlert = ({ title, reason, feature }: Readonly<AIUnavailableAlertProps>) => {
  const { t } = useTranslation();
  const { workspace } = useWorkspace();

  const action = workspace?.organizationId
    ? getAIUnavailableAction(reason, workspace.organizationId)
    : undefined;

  // Only a plan change is a conversion; switching a setting back on is not, so it stays untracked.
  const handleClick = () => {
    if (action && action.type !== "enable_ai") {
      capturePostHogClientEvent("upgrade_cta_clicked", { feature });
    }
  };

  return (
    <Alert variant="info" role="status">
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{getAIUnavailableMessage(reason, t)}</AlertDescription>
      {action && (
        <AlertButton asChild>
          <Link href={action.href} onClick={handleClick}>
            {getAIUnavailableActionLabel(action.type, t)}
          </Link>
        </AlertButton>
      )}
    </Alert>
  );
};
