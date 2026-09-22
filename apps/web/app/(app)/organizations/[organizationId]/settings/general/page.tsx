import { AISettingsToggle } from "@/app/(app)/workspaces/[workspaceId]/settings/organization/general/components/AISettingsToggle";
import { CreateOrganizationCard } from "@/app/(app)/workspaces/[workspaceId]/settings/organization/general/components/CreateOrganizationCard";
import { DeleteOrganization } from "@/app/(app)/workspaces/[workspaceId]/settings/organization/general/components/DeleteOrganization";
import { EditOrganizationSettingsForm } from "@/app/(app)/workspaces/[workspaceId]/settings/organization/general/components/EditOrganizationSettingsForm";
import { SecurityListTip } from "@/app/(app)/workspaces/[workspaceId]/settings/organization/general/components/SecurityListTip";
import { isInstanceAIConfigured } from "@/lib/ai/service";
import { FB_LOGO_URL, IS_FORMA_CLOUD, IS_STORAGE_CONFIGURED } from "@/lib/constants";
import { getUser } from "@/lib/user/service";
import { getTranslate } from "@/lingodotdev/server";
import { getIsMultiOrgEnabled } from "@/modules/license-check/lib/utils";
import { getOrganizationAuth } from "@/modules/organization/lib/utils";
import { getSettingsLayoutData } from "@/modules/settings/lib/navigation-data";
import { redirectBillingRoleFromRestrictedOrgSettings } from "@/modules/settings/lib/redirect-billing-role";
import { Alert, AlertDescription } from "@/modules/ui/components/alert";
import { IdBadge } from "@/modules/ui/components/id-badge";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";
import { SettingsCard } from "@/modules/ui/components/settings-card";
import { SettingsCardGrid } from "@/modules/ui/components/settings-card-grid";
import { EmailCustomizationSettings } from "@/modules/whitelabel/email-customization/components/email-customization-settings";
import packageJson from "@/package.json";

const Page = async (props: Readonly<{ params: Promise<{ organizationId: string }> }>) => {
  const params = await props.params;
  const t = await getTranslate();

  await redirectBillingRoleFromRestrictedOrgSettings(params.organizationId);

  const { session, currentUserMembership, organization, isOwner, isManager } = await getOrganizationAuth(
    params.organizationId
  );

  const [user, isMultiOrgEnabled, layoutData] = await Promise.all([
    session?.user?.id ? getUser(session.user.id) : Promise.resolve(null),
    getIsMultiOrgEnabled(),
    getSettingsLayoutData(session.user.id, organization.id),
  ]);

  const isDeleteDisabled = !isOwner || !isMultiOrgEnabled;
  const currentUserRole = currentUserMembership?.role;
  const isOwnerOrManager = isManager || isOwner;

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("workspace.settings.general.organization_settings")} />
      {!IS_STORAGE_CONFIGURED && (
        <div className="max-w-4xl">
          <Alert variant="warning" role="status">
            <AlertDescription>{t("common.storage_not_configured")}</AlertDescription>
          </Alert>
        </div>
      )}
      {!IS_FORMA_CLOUD && <SecurityListTip />}
      <SettingsCardGrid>
        <SettingsCard
          width="full"
          title={t("workspace.settings.general.organization_settings")}
          description={t("workspace.settings.general.organization_settings_description")}>
          <EditOrganizationSettingsForm organization={organization} membershipRole={currentUserRole} />
        </SettingsCard>
        <SettingsCard
          width="full"
          title={t("workspace.settings.general.ai_enabled")}
          description={t("workspace.settings.general.ai_enabled_description")}>
          <AISettingsToggle
            organization={organization}
            membershipRole={currentUserRole}
            isInstanceAIConfigured={isInstanceAIConfigured()}
          />
        </SettingsCard>
        <EmailCustomizationSettings
          width="full"
          className="xl:col-span-2"
          organization={organization}
          workspaceId={layoutData?.currentWorkspace?.id ?? ""}
          isReadOnly={!isOwnerOrManager}
          fbLogoUrl={FB_LOGO_URL}
          user={user}
          isStorageConfigured={IS_STORAGE_CONFIGURED}
        />
        {isMultiOrgEnabled && (
          <>
            <SettingsCard
              width="full"
              className="xl:col-span-2"
              title={t("workspace.settings.general.delete_organization")}
              description={t("workspace.settings.general.delete_organization_description")}>
              <DeleteOrganization
                organization={organization}
                isDeleteDisabled={isDeleteDisabled}
                isUserOwner={currentUserRole === "owner"}
              />
            </SettingsCard>
          </>
        )}
      </SettingsCardGrid>
      {isMultiOrgEnabled && <CreateOrganizationCard />}

      <div className="space-y-2">
        <IdBadge id={organization.id} label={t("common.organization_id")} variant="column" />
        <IdBadge id={packageJson.version} label={t("common.forma_version")} variant="column" />
      </div>
    </PageContentWrapper>
  );
};

export default Page;
