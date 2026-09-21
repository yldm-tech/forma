import { TWorkspace } from "@forma/types/workspace";
import { getTranslate } from "@/lingodotdev/server";
import { Alert, AlertDescription } from "@/modules/ui/components/alert";
import { SettingsCard, type TSettingsCardWidth } from "@/modules/ui/components/settings-card";
import { EditBranding } from "@/modules/whitelabel/remove-branding/components/edit-branding";

interface BrandingSettingsCardProps {
  workspace: TWorkspace;
  isReadOnly: boolean;
  width?: TSettingsCardWidth;
}

export const BrandingSettingsCard = async ({
  workspace,
  isReadOnly,
  width,
}: Readonly<BrandingSettingsCardProps>) => {
  const t = await getTranslate();

  const brandingContent = (
    <div className="space-y-4">
      <EditBranding
        type="linkSurvey"
        isEnabled={workspace.linkSurveyBranding}
        workspaceId={workspace.id}
        isReadOnly={isReadOnly}
      />
      <EditBranding
        type="appSurvey"
        isEnabled={workspace.inAppSurveyBranding}
        workspaceId={workspace.id}
        isReadOnly={isReadOnly}
      />
    </div>
  );

  return (
    <SettingsCard
      width={width}
      title={t("workspace.look.forma_branding")}
      description={t("workspace.look.forma_branding_settings_description")}
      bodyVariant="padded">
      {brandingContent}
      {isReadOnly && (
        <Alert variant="warning" className="mt-4" role="status">
          <AlertDescription>
            {t("common.only_owners_managers_and_manage_access_members_can_perform_this_action")}
          </AlertDescription>
        </Alert>
      )}
    </SettingsCard>
  );
};
