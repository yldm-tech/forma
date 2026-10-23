import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { BackToLoginButton } from "../../../../features/auth/components/ui";
import { redirectIfSignedIn } from "../../../../features/auth/guards";

export const Route = createFileRoute("/auth/forgot-password/reset/success")({
  beforeLoad: redirectIfSignedIn,
  component: ResetPasswordSuccessPage,
});

function ResetPasswordSuccessPage() {
  const { t } = useTranslation();
  return (
    <div>
      <h1 className="mb-4 text-center font-bold">{t("auth.forgot-password.reset.success.heading")}</h1>
      <p className="text-center">{t("auth.forgot-password.reset.success.text")}</p>
      <div className="mt-3 text-center">
        <BackToLoginButton />
      </div>
    </div>
  );
}
