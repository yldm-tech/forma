import { AuthenticationError } from "@forma/types/errors";
import { AccountSecurity } from "@/app/(app)/workspaces/[workspaceId]/settings/account/profile/components/AccountSecurity";
import { DeleteAccount } from "@/app/(app)/workspaces/[workspaceId]/settings/account/profile/components/DeleteAccount";
import { EditProfileDetailsForm } from "@/app/(app)/workspaces/[workspaceId]/settings/account/profile/components/EditProfileDetailsForm";
import { EMAIL_VERIFICATION_DISABLED, IS_FORMA_CLOUD, PASSWORD_RESET_DISABLED } from "@/lib/constants";
import { getOrganizationsWhereUserIsSingleOwner } from "@/lib/organization/service";
import { getUser } from "@/lib/user/service";
import { getTranslate } from "@/lingodotdev/server";
import { AuthorizedAppsCard } from "@/modules/account/components/authorized-apps-card";
import { requiresPasswordConfirmationForAccountDeletion } from "@/modules/account/lib/account-deletion-auth";
import { getSession } from "@/modules/auth/lib/session";
import { getIsMultiOrgEnabled } from "@/modules/license-check/lib/utils";
import { IdBadge } from "@/modules/ui/components/id-badge";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";
import { SettingsCard } from "@/modules/ui/components/settings-card";

const Page = async () => {
  const isMultiOrgEnabled = await getIsMultiOrgEnabled();
  const t = await getTranslate();
  const session = await getSession();
  if (!session?.user) {
    throw new AuthenticationError(t("common.not_authenticated"));
  }

  const organizationsWithSingleOwner = await getOrganizationsWhereUserIsSingleOwner(session.user.id);
  const user = await getUser(session.user.id);
  if (!user) {
    throw new AuthenticationError(t("common.not_authenticated"));
  }

  const isPasswordResetEnabled = !PASSWORD_RESET_DISABLED && user.identityProvider === "email";
  const requiresPasswordConfirmation = requiresPasswordConfirmationForAccountDeletion(user);

  return (
    <PageContentWrapper width="settings">
      <PageHeader pageTitle={t("common.profile")} />
      <div>
        <SettingsCard
          title={t("workspace.settings.profile.personal_information")}
          description={t("workspace.settings.profile.update_personal_info")}>
          <EditProfileDetailsForm
            user={user}
            emailVerificationDisabled={EMAIL_VERIFICATION_DISABLED}
            isPasswordResetEnabled={isPasswordResetEnabled}
          />
        </SettingsCard>
        {user.identityProvider === "email" && (
          <SettingsCard
            title={t("common.security")}
            description={t("workspace.settings.profile.security_description")}>
            <AccountSecurity user={user} />
          </SettingsCard>
        )}

        <AuthorizedAppsCard locale={user.locale} />

        <SettingsCard
          title={t("workspace.settings.profile.delete_account")}
          description={t("workspace.settings.profile.confirm_delete_account")}>
          <DeleteAccount
            session={session}
            IS_FORMA_CLOUD={IS_FORMA_CLOUD}
            user={user}
            organizationsWithSingleOwner={organizationsWithSingleOwner}
            isMultiOrgEnabled={isMultiOrgEnabled}
            requiresPasswordConfirmation={requiresPasswordConfirmation}
          />
        </SettingsCard>
        <IdBadge id={user.id} label={t("common.profile_id")} variant="column" />
      </div>
    </PageContentWrapper>
  );
};

export default Page;
