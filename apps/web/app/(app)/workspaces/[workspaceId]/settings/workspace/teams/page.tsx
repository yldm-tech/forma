import { getSettingsPageMetadata } from "@/modules/settings/lib/metadata";
import { WorkspaceTeams } from "@/modules/teams/workspace-teams/page";

export const generateMetadata = () => getSettingsPageMetadata("common.team_access");

export default WorkspaceTeams;
