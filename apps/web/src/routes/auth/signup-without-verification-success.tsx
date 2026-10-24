import { createFileRoute, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { validatedCallbackUrl } from "../../features/auth/callback-url";
import { BackToLoginButton } from "../../features/auth/components/ui";
import { redirectIfSignedIn } from "../../features/auth/guards";
import { signupEmailFrom } from "../../features/auth/history-state";
import { callbackSearch } from "../../features/auth/search";

export const Route = createFileRoute("/auth/signup-without-verification-success")({
  validateSearch: callbackSearch,
  beforeLoad: redirectIfSignedIn,
  component: SignupWithoutVerificationSuccessPage,
});

/** Where sign-up lands when email verification is off (the self-hosted default). The sign-in button carries an invite's callback, the only way back to it for an invited visitor whose address already had an account. */
function SignupWithoutVerificationSuccessPage() {
  const { t } = useTranslation();
  const search = Route.useSearch();
  const email = useRouterState({ select: (state) => signupEmailFrom(state.location.state) });
  const callbackUrl = validatedCallbackUrl(search.callbackUrl, window.location.origin);

  if (!email) {
    return (
      <>
        <p className="text-center">{t("auth.verification-requested.invalid_token")}</p>
        <hr className="my-4" />
        <BackToLoginButton callbackUrl={callbackUrl} />
      </>
    );
  }
  return (
    <>
      <h1 className="mb-4 text-center font-bold">
        {t("auth.signup_without_verification_success.user_successfully_created")}
      </h1>
      <p className="text-center text-sm">
        {t("auth.signup_without_verification_success.user_successfully_created_info", { email })}
      </p>
      <hr className="my-4" />
      <BackToLoginButton callbackUrl={callbackUrl} email={email} />
    </>
  );
}
