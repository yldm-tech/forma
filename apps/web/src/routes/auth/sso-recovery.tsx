import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { authApi, resetSession } from "../../features/auth/api";
import { callbackPath } from "../../features/auth/callback-url";
import { Alert, BackToLoginButton } from "../../features/auth/components/ui";
import { tokenSearch } from "../../features/auth/search";
import { ApiError } from "../../lib/api";
import { errorMessage } from "../../lib/errors";

// The link mailed when an SSO sign-in asserted an address that already has an account. Redeeming it links the identity and signs this browser in, or, for an account with two-factor authentication, sends it to the sign-in page's code step, which does both once the code is accepted; it is a CSRF-checked POST from this page, never a GET a crawler or a preload could trigger.
export const Route = createFileRoute("/auth/sso-recovery")({
  validateSearch: tokenSearch,
  component: SSORecoveryPage,
});

/** Success goes where the SSO sign-in was headed. Any failure but a rate limit goes to the sign-in page with forma2's "could not be linked" alert, as its recovery route redirected. */
function SSORecoveryPage() {
  const { t } = useTranslation();
  const { token } = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [failure, setFailure] = useState<string>();
  const started = useRef<string>(undefined);

  useEffect(() => {
    if (started.current === (token ?? "")) return;
    started.current = token ?? "";
    const notLinked = () =>
      navigate({ to: "/auth/login", search: { error: "account_not_linked" }, replace: true });
    if (!token) {
      void notLinked();
      return;
    }
    authApi.completeSSORecovery(token).then(
      async ({ redirectUrl, status }) => {
        if (status === "two_factor_required") {
          await navigate({
            to: "/auth/login",
            search: { twoFactor: "1", callbackUrl: redirectUrl },
            replace: true,
          });
          return;
        }
        resetSession(queryClient);
        await navigate({ href: callbackPath(redirectUrl, window.location.origin), replace: true });
      },
      (error: unknown) => {
        if (error instanceof ApiError && error.isRateLimited) setFailure(errorMessage(t, error));
        else void notLinked();
      }
    );
  }, [navigate, queryClient, t, token]);

  if (failure) {
    return (
      <>
        <Alert variant="error">{failure}</Alert>
        <BackToLoginButton />
      </>
    );
  }
  return (
    <p className="text-center text-sm text-slate-700" role="status">
      {t("common.loading")}
    </p>
  );
}
