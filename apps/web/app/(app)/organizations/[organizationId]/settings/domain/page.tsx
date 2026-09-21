import { notFound } from "next/navigation";
import { AuthenticationError } from "@forma/types/errors";
import { PrettyUrlsTable } from "@/app/(app)/workspaces/[workspaceId]/settings/organization/domain/components/pretty-urls-table";
import { IS_FORMA_CLOUD, IS_STORAGE_CONFIGURED } from "@/lib/constants";
import { getTranslate } from "@/lingodotdev/server";
import { getOrganizationAuth } from "@/modules/organization/lib/utils";
import { getSettingsLayoutData } from "@/modules/settings/lib/navigation-data";
import { redirectBillingRoleFromRestrictedOrgSettings } from "@/modules/settings/lib/redirect-billing-role";
import { getSurveysWithSlugsByOrganizationId } from "@/modules/survey/lib/slug";
import { Alert, AlertDescription } from "@/modules/ui/components/alert";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";
import { SettingsCard } from "@/modules/ui/components/settings-card";
import { FaviconCustomizationSettings } from "@/modules/whitelabel/favicon-customization/components/favicon-customization-settings";

const Page = async (props: Readonly<{ params: Promise<{ organizationId: string }> }>) => {
  const params = await props.params;
  const t = await getTranslate();

  if (IS_FORMA_CLOUD) {
    return notFound();
  }

  await redirectBillingRoleFromRestrictedOrgSettings(params.organizationId);

  const { session, organization, isOwner, isManager } = await getOrganizationAuth(params.organizationId);
  if (!session) {
    throw new AuthenticationError(t("common.not_authenticated"));
  }

  const [surveys, layoutData] = await Promise.all([
    getSurveysWithSlugsByOrganizationId(organization.id),
    getSettingsLayoutData(session.user.id, organization.id),
  ]);
  const isOwnerOrManager = isManager || isOwner;

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.domain")} />

      {!IS_STORAGE_CONFIGURED && (
        <div className="max-w-4xl">
          <Alert variant="warning" role="status">
            <AlertDescription>{t("common.storage_not_configured")}</AlertDescription>
          </Alert>
        </div>
      )}

      <FaviconCustomizationSettings
        width="full"
        organization={organization}
        workspaceId={layoutData?.currentWorkspace?.id ?? ""}
        isReadOnly={!isOwnerOrManager}
        isStorageConfigured={IS_STORAGE_CONFIGURED}
      />

      <SettingsCard
        width="full"
        title={t("workspace.settings.domain.title")}
        description={t("workspace.settings.domain.description")}
        bodyVariant="flush">
        <PrettyUrlsTable surveys={surveys} />
      </SettingsCard>
    </PageContentWrapper>
  );
};

export default Page;
