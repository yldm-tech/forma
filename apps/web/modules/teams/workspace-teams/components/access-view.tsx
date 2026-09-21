"use client";

import { useTranslation } from "react-i18next";
import { AccessTable } from "@/modules/teams/workspace-teams/components/access-table";
import { ManageTeam } from "@/modules/teams/workspace-teams/components/manage-team";
import { TWorkspaceTeam } from "@/modules/teams/workspace-teams/types/team";
import { SettingsCard } from "@/modules/ui/components/settings-card";

interface AccessViewProps {
  teams: TWorkspaceTeam[];
}

export const AccessView = ({ teams }: AccessViewProps) => {
  const { t } = useTranslation();
  return (
    <>
      <SettingsCard
        title={t("common.team_access")}
        description={t("workspace.teams.team_settings_description")}
        bodyVariant="flush"
        width="full">
        {/* The table is edge-to-edge, so the control above it carries the card's gutter itself. */}
        <div className="mb-4 flex justify-end px-4 pt-4">
          <ManageTeam />
        </div>
        <AccessTable teams={teams} />
      </SettingsCard>
    </>
  );
};
