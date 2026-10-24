import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { sessionQuery } from "../../features/auth/api";
import { Button } from "../../components/ui/button";
import { Alert, ButtonLink } from "../../features/auth/components/ui";
import { tokenSearch } from "../../features/auth/search";
import { errorMessage } from "../../lib/errors";
import { loginHref } from "../../lib/navigation";
import { useConfirmAccountDeletion } from "../../lib/queries/auth";
import { forgetWorkspace } from "../../lib/storage";

// The link mailed to a user without a password who asked to delete their account (forma2's Better Auth delete-user callback). The server deletes only from a session of the same user, so a signed-out browser signs in first and comes back; the deletion itself is a CSRF-checked POST sent when the user confirms here, never on page load, so a preload or a link scanner cannot trigger it.
export const Route = createFileRoute("/auth/delete-account")({
  validateSearch: tokenSearch,
  beforeLoad: async ({ context, location }) => {
    const user = await context.queryClient.fetchQuery(sessionQuery);
    if (!user) throw redirect({ href: loginHref(location.href), replace: true });
  },
  component: DeleteAccountPage,
});

function DeleteAccountPage() {
  const { t } = useTranslation();
  const { token } = Route.useSearch();
  const queryClient = useQueryClient();
  const confirm = useConfirmAccountDeletion();
  const [failure, setFailure] = useState<string>();

  const run = async () => {
    if (!token) return;
    try {
      const { redirectUrl } = await confirm.mutateAsync(token);
      forgetWorkspace();
      queryClient.clear();
      globalThis.location.replace(redirectUrl);
    } catch (error) {
      setFailure(errorMessage(t, error));
    }
  };

  return (
    <>
      <h1 className="mb-4 text-center font-bold">{t("workspace.settings.profile.delete_account")}</h1>
      {!token || failure ? (
        <Alert variant="error">{failure ?? t("auth.verification-requested.invalid_token")}</Alert>
      ) : (
        <ul className="mb-6 list-disc space-y-1 pl-6 text-left text-sm text-slate-700">
          <li>
            {t("workspace.settings.profile.permanent_removal_of_all_of_your_personal_information_and_data")}
          </li>
          <li>{t("workspace.settings.profile.warning_cannot_undo")}</li>
        </ul>
      )}
      <div className="flex flex-col gap-3">
        {token && !failure && (
          <Button
            variant="destructive"
            className="h-11 w-full sm:h-9"
            loading={confirm.isPending}
            onClick={() => void run()}>
            {t("workspace.settings.profile.confirm_delete_my_account")}
          </Button>
        )}
        <ButtonLink to="/account/settings/profile" variant="secondary" className="w-full">
          {t("common.cancel")}
        </ButtonLink>
      </div>
    </>
  );
}
