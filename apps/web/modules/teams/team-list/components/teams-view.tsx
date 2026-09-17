import { ResourceNotFoundError } from "@forma/types/errors";
import { TOrganizationRole } from "@forma/types/memberships";
import { getTranslate } from "@/lingodotdev/server";
import { getMembersByOrganizationId } from "@/modules/organization/settings/teams/lib/membership";
import { TeamsTable } from "@/modules/teams/team-list/components/teams-table";
import { getTeams } from "@/modules/teams/team-list/lib/team";
import { getWorkspacesByOrganizationId } from "@/modules/teams/team-list/lib/workspace";
import { SettingsCard } from "@/modules/ui/components/settings-card";

interface TeamsViewProps {
  organizationId: string;
  membershipRole?: TOrganizationRole;
  currentUserId: string;
}

export const TeamsView = async ({ organizationId, membershipRole, currentUserId }: TeamsViewProps) => {
  const t = await getTranslate();

  const [teams, orgMembers, orgWorkspaces] = await Promise.all([
    getTeams(currentUserId, organizationId),
    getMembersByOrganizationId(organizationId),
    getWorkspacesByOrganizationId(organizationId),
  ]);

  if (!teams) {
    throw new ResourceNotFoundError(t("common.teams"), null);
  }

  return (
    <SettingsCard
      title={t("workspace.settings.teams.teams")}
      description={t("workspace.settings.teams.teams_description")}
      // The table runs edge to edge.
      bodyVariant="flush">
      <TeamsTable
        teams={teams}
        membershipRole={membershipRole}
        organizationId={organizationId}
        orgMembers={orgMembers}
        orgWorkspaces={orgWorkspaces}
        currentUserId={currentUserId}
      />
    </SettingsCard>
  );
};
