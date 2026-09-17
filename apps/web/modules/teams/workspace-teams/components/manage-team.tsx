"use client";

import { useRouter } from "next/navigation";
import { useTranslation } from "react-i18next";
import { Button } from "@/modules/ui/components/button";
import { useWorkspace } from "@/modules/workspaces/context/workspace-context";

export const ManageTeam = () => {
  const { t } = useTranslation();
  const { workspace } = useWorkspace();

  const router = useRouter();

  const handleManageTeams = () => {
    router.push(`/organizations/${workspace?.organizationId}/settings/teams`);
  };

  return (
    <Button variant="secondary" size="sm" onClick={handleManageTeams}>
      {t("workspace.teams.manage_teams")}
    </Button>
  );
};
