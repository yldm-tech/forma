import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { authApi, resetSession } from "../features/auth/api";
import { BackToLoginButton, FormWrapper } from "../features/auth/components/ui";
import { tokenSearch } from "../features/auth/search";

// The link mailed to a new address. It is redeemed by the page, not by the loader, so a preload cannot use it up.
export const Route = createFileRoute("/verify-email-change")({
  validateSearch: tokenSearch,
  component: VerifyEmailChangePage,
});

type Status = "loading" | "success" | "error";

/** Confirms the change, then signs this browser out so the user signs in again with the new address, as forma2 did. The sign-out is best effort: the change has already happened. */
function VerifyEmailChangePage() {
  const { t } = useTranslation();
  const { token } = Route.useSearch();
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<Status>(token ? "loading" : "error");
  const started = useRef<string>(undefined);

  useEffect(() => {
    if (!token || started.current === token) return;
    started.current = token;
    authApi.confirmEmailChange(token).then(
      () => {
        setStatus("success");
        void authApi
          .signOut()
          .catch(() => undefined)
          .finally(() => resetSession(queryClient));
      },
      () => setStatus("error")
    );
  }, [queryClient, token]);

  const heading = {
    loading: t("auth.email-change.email_verification_loading"),
    success: t("auth.email-change.email_change_success"),
    error: t("auth.email-change.email_verification_failed"),
  }[status];
  const description = {
    loading: t("auth.email-change.email_verification_loading_description"),
    success: t("auth.email-change.email_change_success_description"),
    error: t("auth.email-change.invalid_or_expired_token"),
  }[status];

  return (
    <FormWrapper>
      <h1 className={`mb-4 text-center font-bold ${status === "error" ? "text-red-600" : ""}`}>{heading}</h1>
      <p className="text-center text-sm" role={status === "loading" ? "status" : undefined}>
        {description}
      </p>
      <hr className="my-4" />
      <BackToLoginButton />
    </FormWrapper>
  );
}
