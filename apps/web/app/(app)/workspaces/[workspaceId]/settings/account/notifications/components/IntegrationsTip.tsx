"use client";

import { useTranslation } from "react-i18next";
import { SlackIcon } from "@/modules/ui/components/icons";
import { useWorkspace } from "@/modules/workspaces/context/workspace-context";

export const IntegrationsTip = () => {
  const { t } = useTranslation();
  const { workspace } = useWorkspace();
  return (
    <div>
      <div className="flex max-w-4xl items-center gap-y-3 rounded-lg border border-blue-100 bg-blue-50 p-4 text-sm text-blue-900 shadow-xs md:gap-y-0 md:text-base">
        <SlackIcon className="mr-3 size-4 text-blue-400" />
        <p className="text-sm">
          {t("workspace.settings.notifications.need_slack_or_discord_notifications")}?
          <a
            href={`/workspaces/${workspace?.id}/integrations`}
            className="ml-1 cursor-pointer text-sm underline">
            {t("workspace.settings.notifications.use_the_integration")}
          </a>
        </p>
      </div>
    </div>
  );
};
