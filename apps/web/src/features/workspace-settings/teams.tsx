import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { DataTable } from "../../components/ui/data-table";
import { IdBadge } from "../../components/ui/id-badge";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { organizationSettingsPath } from "../../lib/navigation";
import { type WorkspaceDetail, type WorkspaceTeam, workspaceTeamsQuery } from "../../lib/queries/workspaces";

// forma2's TeamPermissionMapping, which was never translated.
const PERMISSION_LABELS: Record<WorkspaceTeam["permission"], string> = {
  read: "Read",
  readWrite: "Read & write",
  manage: "Manage",
};

const column = createColumnHelper<WorkspaceTeam>();

/** forma2's workspace team access page: the teams that can open the workspace, and a way to the organization's teams. */
export const WorkspaceTeamsPage = ({ workspace }: { workspace: WorkspaceDetail }) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const teams = useQuery(workspaceTeamsQuery(workspace.id));
  const columns = [
    column.accessor("name", {
      header: t("workspace.teams.team_name"),
      meta: { headerClassName: "w-[30%]", cellClassName: "font-medium" },
    }),
    column.accessor("memberCount", {
      header: t("common.size"),
      meta: { headerClassName: "w-[15%]" },
      cell: (info) => t("common.count_members", { count: info.getValue() }),
    }),
    column.accessor("id", {
      header: t("common.team_id"),
      meta: { headerClassName: "w-[30%]" },
      cell: (info) => <IdBadge id={info.getValue()} />,
    }),
    column.accessor("permission", {
      header: t("workspace.teams.permission"),
      meta: { headerClassName: "w-[25%]" },
      cell: (info) => PERMISSION_LABELS[info.getValue()],
    }),
  ];

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.team_access")} />
      <SettingsCard
        title={t("common.team_access")}
        description={t("workspace.teams.team_settings_description")}
        bodyVariant="flush"
        width="full">
        <div className="mb-4 flex justify-end px-4 pt-4">
          <Button
            variant="secondary"
            size="sm"
            onClick={() =>
              void navigate({ href: organizationSettingsPath(workspace.organizationId, "teams") })
            }>
            {t("workspace.teams.manage_teams")}
          </Button>
        </div>
        <DataTable
          columns={columns}
          data={teams.data ?? []}
          getRowId={(team) => team.id}
          isLoading={teams.isPending}
          emptyMessage={
            teams.isError
              ? t("common.something_went_wrong_please_try_again")
              : t("workspace.teams.no_teams_found")
          }
          aria-label={t("common.team_access")}
        />
      </SettingsCard>
    </PageContentWrapper>
  );
};
