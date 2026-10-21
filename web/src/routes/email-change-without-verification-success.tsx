import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { BackToLoginButton, FormWrapper } from "../features/auth/components/ui";
import { redirectIfSignedIn } from "../features/auth/guards";

// Where the profile page sends a user whose email change was applied at once (verification off); they were signed out on the way.
export const Route = createFileRoute("/email-change-without-verification-success")({
  beforeLoad: redirectIfSignedIn,
  component: EmailChangeWithoutVerificationSuccessPage,
});

function EmailChangeWithoutVerificationSuccessPage() {
  const { t } = useTranslation();
  return (
    <FormWrapper>
      <h1 className="mb-4 text-center font-bold">{t("auth.email-change.email_change_success")}</h1>
      <p className="text-center text-sm">{t("auth.email-change.email_change_success_description")}</p>
      <hr className="my-4" />
      <BackToLoginButton />
    </FormWrapper>
  );
}
