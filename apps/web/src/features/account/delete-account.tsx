import { type UseQueryResult, queryOptions, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { Input } from "../../components/ui/input";
import { LoadingBlock } from "../../components/ui/page";
import { PasswordInput } from "../../components/ui/password-input";
import { ApiError, api } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { organizationSettingsPath } from "../../lib/navigation";
import {
  type Me,
  authOptionsQuery,
  useDeleteAccount,
  useRequestAccountDeletion,
} from "../../lib/queries/auth";
import { forgetWorkspace } from "../../lib/storage";
import { toast } from "../../lib/toast";

/** An organization the caller is the only active owner of, which goes with their account. */
export interface SoleOwnedOrganization {
  id: string;
  name: string;
  workspaceCount: number;
}

/** GET /api/app/auth/deletion-preview: what deleting the account deletes besides the user, read from the same query the deletion runs. Never cached between openings, since ownership can change in the meantime. */
export const deletionPreviewQuery = queryOptions({
  queryKey: ["auth", "deletion-preview"] as const,
  queryFn: ({ signal }) =>
    api
      .get<{ organizations: SoleOwnedOrganization[] }>("/api/app/auth/deletion-preview", { signal })
      .then((res) => res.organizations),
  staleTime: 0,
  gcTime: 0,
});

/** forma2's DeleteAccount card. */
export const DeleteAccount = ({ user }: { user: Me }) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <div className="pb-4">
      <p className="text-sm text-slate-700">
        <strong>{t("workspace.settings.profile.warning_cannot_undo")}</strong>
      </p>
      <Button className="mt-4" variant="destructive" size="sm" onClick={() => setOpen(true)}>
        {t("workspace.settings.profile.confirm_delete_my_account")}
      </Button>
      {open && <DeleteAccountModal user={user} onClose={() => setOpen(false)} />}
    </div>
  );
};

/**
 * forma2's DeleteAccountModal, confirmed by typing the account's address. A password user also gives their password and the account goes at once, with the organizations only they own; anyone else is mailed a link that finishes the deletion, which needs SMTP. Before anything can be confirmed the modal names the organizations that would be deleted too, with the way to keep each: another owner.
 */
export const DeleteAccountModal = ({ user, onClose }: { user: Me; onClose: () => void }) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [confirmation, setConfirmation] = useState("");
  const [password, setPassword] = useState("");
  const [emailSent, setEmailSent] = useState(false);
  const deleteAccount = useDeleteAccount();
  const requestDeletion = useRequestAccountDeletion();
  const preview = useQuery(deletionPreviewQuery);
  const options = useQuery(authOptionsQuery);
  const requiresPassword = user.identityProvider === "email";
  // The emailed link is the only way an SSO user can confirm, so without SMTP their deletion cannot start.
  const emailUnavailable = !requiresPassword && options.data?.smtpConfigured === false;
  const confirmed =
    preview.isSuccess &&
    !emailUnavailable &&
    confirmation.trim().toLowerCase() === user.email.toLowerCase() &&
    (!requiresPassword || password.length > 0);

  const run = async () => {
    if (!confirmed || emailSent) return;
    if (!requiresPassword) {
      try {
        await requestDeletion.mutateAsync();
        setEmailSent(true);
      } catch (error) {
        toast.error(errorMessage(t, error));
      }
      return;
    }
    try {
      const { redirectUrl } = await deleteAccount.mutateAsync(password);
      forgetWorkspace();
      queryClient.clear();
      globalThis.location.replace(redirectUrl);
    } catch (error) {
      toast.error(
        error instanceof ApiError && error.code === "invalid_password"
          ? t("workspace.settings.profile.wrong_password")
          : errorMessage(t, error)
      );
    }
  };

  return (
    <DeleteDialog
      open
      setOpen={(open) => !open && onClose()}
      deleteWhat={t("common.account")}
      onDelete={() => void run()}
      text={t("workspace.settings.profile.account_deletion_consequences_warning")}
      isDeleting={deleteAccount.isPending || requestDeletion.isPending}
      disabled={!confirmed || emailSent}>
      <div className="py-5">
        {emailSent ? (
          <Alert variant="info">
            <AlertTitle>{t("common.account")}</AlertTitle>
            <AlertDescription>
              {t("workspace.settings.profile.account_deletion_email_sent", { email: user.email })}
            </AlertDescription>
          </Alert>
        ) : (
          <>
            <ul className="list-disc pb-6 pl-6">
              <li>
                {t(
                  "workspace.settings.profile.permanent_removal_of_all_of_your_personal_information_and_data"
                )}
              </li>
              <li>{t("workspace.settings.profile.warning_cannot_undo")}</li>
            </ul>
            <SoleOwnedOrganizations preview={preview} />
            {emailUnavailable && (
              <Alert variant="warning" role="status" className="mb-6">
                <AlertDescription>
                  {t("workspace.settings.profile.account_deletion_email_not_configured")}
                </AlertDescription>
              </Alert>
            )}
            <form
              onSubmit={(e: FormEvent) => {
                e.preventDefault();
                void run();
              }}>
              <label htmlFor="deleteAccountConfirmation">
                {t("workspace.settings.profile.please_enter_email_to_confirm_account_deletion", {
                  email: user.email,
                })}
              </label>
              <Input
                id="deleteAccountConfirmation"
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
                className="mt-2"
                autoComplete="off"
              />
              {requiresPassword && (
                <>
                  <label htmlFor="deleteAccountPassword" className="mt-4 block">
                    {t("common.password")}
                  </label>
                  <PasswordInput
                    id="deleteAccountPassword"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    autoComplete="current-password"
                    containerClassName="mt-2"
                    required
                  />
                </>
              )}
            </form>
          </>
        )}
      </div>
    </DeleteDialog>
  );
};

/** The organizations that go with the account, each with a link to its members, where another owner can be made first; nothing while there are none. The deletion stays disabled until this has loaded. */
const SoleOwnedOrganizations = ({ preview }: { preview: UseQueryResult<SoleOwnedOrganization[]> }) => {
  const { t } = useTranslation();
  if (preview.isPending) return <LoadingBlock className="mb-6" />;
  if (preview.isError) {
    return (
      <div role="alert" className="mb-6 flex flex-col items-start gap-2 text-sm text-red-700">
        <p>{errorMessage(t, preview.error)}</p>
        <Button variant="secondary" size="sm" onClick={() => void preview.refetch()}>
          {t("common.try_again")}
        </Button>
      </div>
    );
  }
  if (preview.data.length === 0) return null;
  return (
    <Alert variant="error" role="status" className="mb-6">
      <AlertDescription>
        <p>
          <Trans
            i18nKey="workspace.settings.profile.organizations_delete_message"
            components={{ b: <strong /> }}
          />
        </p>
        <ul className="mt-2 list-disc pl-5">
          {preview.data.map((organization) => (
            <li key={organization.id}>
              {t("workspace.settings.profile.sole_owned_organization_item", {
                name: organization.name,
                count: organization.workspaceCount,
              })}{" "}
              <Link
                to={organizationSettingsPath(organization.id, "teams")}
                className="font-medium underline underline-offset-2">
                {t("common.members_and_teams")}
              </Link>
            </li>
          ))}
        </ul>
        <p className="mt-2">{t("workspace.settings.profile.sole_owned_organizations_hint")}</p>
      </AlertDescription>
    </Alert>
  );
};
