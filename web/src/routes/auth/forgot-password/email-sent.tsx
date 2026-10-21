import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { BackToLoginButton } from "../../../features/auth/components/ui";
import { redirectIfSignedIn } from "../../../features/auth/guards";

export const Route = createFileRoute("/auth/forgot-password/email-sent")({
  beforeLoad: redirectIfSignedIn,
  component: EmailSentPage,
});

function EmailSentPage() {
  const { t } = useTranslation();
  return (
    <div>
      <h1 className="mb-4 text-center font-bold">{t("auth.forgot-password.email-sent.heading")}</h1>
      <p className="text-center">{t("auth.forgot-password.email-sent.text")}</p>
      <div className="mt-5 text-center">
        <BackToLoginButton />
      </div>
    </div>
  );
}
