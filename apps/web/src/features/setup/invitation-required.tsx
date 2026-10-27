import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { errorMessage } from "../../lib/errors";
import { signOut } from "../../lib/queries/auth";
import { toast } from "../../lib/toast";

/** What a user without an organization sees, in place of the step that creates one, on an invite-only instance where only the owner of an organization may create another: they get in through an invitation, so they are told to ask an owner for one, and can sign out to come back through its link. */
export const InvitationRequired = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [signingOut, setSigningOut] = useState(false);

  const logOut = async () => {
    setSigningOut(true);
    try {
      await signOut(queryClient, { reason: "user_initiated" });
    } catch (error) {
      setSigningOut(false);
      toast.error(errorMessage(t, error));
      return;
    }
    await navigate({ href: "/auth/login" });
  };

  return (
    <div className="flex flex-col items-center gap-y-4">
      <h1 className="text-2xl font-medium">{t("organization_creation.invitation_required_title")}</h1>
      <p className="text-slate-600">{t("organization_creation.invitation_required_description")}</p>
      <Button
        type="button"
        variant="secondary"
        className="w-80"
        loading={signingOut}
        onClick={() => void logOut()}>
        {t("common.logout")}
      </Button>
    </div>
  );
};
