import { useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { Input } from "../../components/ui/input";
import { PasswordInput } from "../../components/ui/password-input";
import { ApiError } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { type Me, useDeleteAccount, useRequestAccountDeletion } from "../../lib/queries/auth";
import { forgetWorkspace } from "../../lib/storage";
import { toast } from "../../lib/toast";

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
 * forma2's DeleteAccountModal, confirmed by typing the account's address. A password user also gives their password and the account goes at once, with the organizations only they own; anyone else is mailed a link that finishes the deletion.
 */
export const DeleteAccountModal = ({ user, onClose }: { user: Me; onClose: () => void }) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [confirmation, setConfirmation] = useState("");
  const [password, setPassword] = useState("");
  const [emailSent, setEmailSent] = useState(false);
  const deleteAccount = useDeleteAccount();
  const requestDeletion = useRequestAccountDeletion();
  const requiresPassword = user.identityProvider === "email";
  const confirmed =
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
                placeholder={user.email}
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
