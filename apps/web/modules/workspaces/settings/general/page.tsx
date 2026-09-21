import { IS_FORMA_CLOUD } from "@/lib/constants";
import { getTranslate } from "@/lingodotdev/server";
import { IdBadge } from "@/modules/ui/components/id-badge";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";
import { SettingsCard } from "@/modules/ui/components/settings-card";
import { SettingsCardGrid } from "@/modules/ui/components/settings-card-grid";
import { getWorkspaceAuth } from "@/modules/workspaces/lib/utils";
import { CustomScriptsForm } from "./components/custom-scripts-form";
import { DeleteWorkspace } from "./components/delete-workspace";
import { EditCooldownPeriodForm } from "./components/edit-cooldown-period-form";
import { EditWorkspaceNameForm } from "./components/edit-workspace-name-form";

export const GeneralSettingsPage = async (props: { params: Promise<{ workspaceId: string }> }) => {
  const params = await props.params;
  const t = await getTranslate();

  const { isReadOnly, isOwner, isManager, workspace, organization } = await getWorkspaceAuth(
    params.workspaceId
  );

  const isOwnerOrManager = isOwner || isManager;

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.workspace_settings")} />
      <SettingsCardGrid>
        <SettingsCard
          width="full"
          title={t("common.workspace_name")}
          description={t("workspace.general.workspace_name_settings_description")}>
          <EditWorkspaceNameForm workspace={workspace} isReadOnly={isReadOnly} />
        </SettingsCard>
        <SettingsCard
          width="full"
          title={t("workspace.general.recontact_cooldown_period")}
          description={t("workspace.general.recontact_cooldown_period_settings_description")}>
          <EditCooldownPeriodForm workspace={workspace} isReadOnly={isReadOnly} />
        </SettingsCard>
        {!IS_FORMA_CLOUD && (
          <SettingsCard
            width="full"
            title={t("workspace.general.custom_scripts")}
            description={t("workspace.general.custom_scripts_card_description")}>
            <CustomScriptsForm workspace={workspace} isReadOnly={!isOwnerOrManager} />
          </SettingsCard>
        )}
        <SettingsCard
          width="full"
          className="xl:col-span-2"
          title={t("workspace.general.delete_workspace")}
          description={t("workspace.general.delete_workspace_settings_description")}>
          <DeleteWorkspace
            organizationId={organization.id}
            currentWorkspace={workspace}
            isOwnerOrManager={isOwnerOrManager}
          />
        </SettingsCard>
      </SettingsCardGrid>
      <div className="space-y-2">
        <IdBadge id={workspace.id} label={t("common.workspace_id")} variant="column" />
      </div>
    </PageContentWrapper>
  );
};
