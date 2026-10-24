import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { authApi, resetSession } from "../../features/auth/api";
import { BackToLoginButton } from "../../features/auth/components/ui";
import { tokenSearch } from "../../features/auth/search";
import { ApiError } from "../../lib/api";
import { errorMessage } from "../../lib/errors";

// The link in the verification email. It is redeemed by the page, not by the loader: a preload or a mail scanner fetching the page must not use up the token.
export const Route = createFileRoute("/auth/verify-email")({
  validateSearch: tokenSearch,
  component: VerifyEmailPage,
});

/**
 * Confirms the address. The server signs in only the browser that signed up (it holds the sign-up intent cookie), which then goes to the root page; any other browser is sent to sign in with the "email verified" notice, as forma2 redirected to /auth/login?verified=1.
 */
function VerifyEmailPage() {
  const { t } = useTranslation();
  const { token } = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [failure, setFailure] = useState<string>();
  const started = useRef<string>(undefined);

  useEffect(() => {
    if (!token) return;
    if (started.current === token) return;
    started.current = token;
    authApi.verifyEmail(token).then(
      async ({ signedIn }) => {
        if (signedIn) {
          resetSession(queryClient);
          await navigate({ to: "/", replace: true });
        } else {
          await navigate({ href: "/auth/login?verified=1", replace: true });
        }
      },
      (error: unknown) =>
        setFailure(
          error instanceof ApiError && (error.code === "invalid_token" || error.code === "token_expired")
            ? t("c.link_expired_description")
            : errorMessage(t, error)
        )
    );
  }, [navigate, queryClient, t, token]);

  const error = token ? failure : t("c.link_expired_description");
  return (
    <>
      <h1 className={`mb-4 text-center font-bold ${error ? "text-red-600" : ""}`}>
        {error
          ? t("auth.email-change.email_verification_failed")
          : t("auth.email-change.email_verification_loading")}
      </h1>
      {error && (
        <>
          <p className="text-center text-sm" role="alert">
            {error}
          </p>
          <hr className="my-4" />
          <BackToLoginButton />
        </>
      )}
    </>
  );
}
