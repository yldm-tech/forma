import { useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { Link, createFileRoute, useNavigate, useRouterState } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { authApi, authOptionsQuery, sessionQuery } from "../../features/auth/api";
import { validatedCallbackUrl } from "../../features/auth/callback-url";
import { Alert, Button, Field, TextInput } from "../../features/auth/components/ui";
import { emailFormFailure } from "../../features/auth/errors";
import { redirectIfSignedIn } from "../../features/auth/guards";
import { signupEmailFrom } from "../../features/auth/history-state";
import { type CallbackSearch, callbackSearch, stringParam } from "../../features/auth/search";
import { emailError } from "../../features/auth/validation";
import { type Validators, useForm } from "../../lib/form";

interface VerificationRequestedSearch extends CallbackSearch {
  /** sso_recovery when an SSO sign-in for an existing account was sent a link to prove the address. */
  purpose?: string;
}

export const Route = createFileRoute("/auth/verification-requested")({
  validateSearch: (search: Record<string, unknown>): VerificationRequestedSearch => ({
    ...callbackSearch(search),
    purpose: stringParam(search.purpose),
  }),
  beforeLoad: redirectIfSignedIn,
  loader: ({ context }) => context.queryClient.ensureQueryData(authOptionsQuery),
  component: VerificationRequestedPage,
});

const EMAIL_RULES: Validators<{ email: string }> = { email: emailError };

/**
 * Where a sign-up that needs a verified address lands, and the page emailed links point to for a fresh link. Nothing on it depends on whether the address has an account: the copy is conditional, the resend answers the same for every address, and the sign-in link is always offered, so the page cannot be used to learn which addresses are registered.
 */
function VerificationRequestedPage() {
  const { t } = useTranslation();
  const search = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: options } = useSuspenseQuery(authOptionsQuery);
  const signupEmail = useRouterState({ select: (state) => signupEmailFrom(state.location.state) });

  const callbackUrl = validatedCallbackUrl(search.callbackUrl, window.location.origin);
  const recovery = search.purpose === "sso_recovery";
  const [resent, setResent] = useState(false);

  // The link may be opened in another tab, which signs this browser in; coming back to this one then moves on, as forma2 reloaded the page to the same effect.
  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState !== "visible") return;
      void queryClient.fetchQuery({ ...sessionQuery, staleTime: 0 }).then((user) => {
        if (user) void navigate({ to: "/", replace: true });
      });
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [navigate, queryClient]);

  const form = useForm({
    defaultValues: { email: signupEmail ?? "" },
    validators: EMAIL_RULES,
    onSubmit: async ({ email }) => {
      setResent(false);
      try {
        await authApi.resendVerification(email.toLowerCase());
        setResent(true);
      } catch (error) {
        const failure = emailFormFailure(t, error);
        if (failure.fields.email) form.setFieldError("email", failure.fields.email);
        form.setFormError(failure.message);
      }
    },
  });

  // A resend makes sense for a sign-up's address; an SSO recovery link is sent again only by signing in again.
  const canResend = options.smtpConfigured && !recovery;

  return (
    <>
      <h1 className="mb-4 text-center text-lg font-semibold text-slate-900">
        {t("auth.verification-requested.please_confirm_your_email_address")}
      </h1>
      {!options.smtpConfigured ? (
        <Alert variant="warning" title={t("auth.verification-requested.email_not_configured_title")}>
          <p>{t("auth.verification-requested.email_not_configured_description")}</p>
        </Alert>
      ) : signupEmail ? (
        <p className="text-center text-sm break-words text-slate-700">
          {t("auth.verification-requested.verification_email_successfully_sent_info", { email: signupEmail })}
        </p>
      ) : (
        recovery && (
          <p className="text-center text-sm text-slate-700">
            {t("auth.verification-requested.new_email_verification_success")}
          </p>
        )
      )}
      {canResend && (
        <>
          <hr className="my-4" />
          <p className="text-center text-xs text-slate-500">
            {t("auth.verification-requested.you_didnt_receive_an_email_or_your_link_expired")}
          </p>
          {resent && (
            <div className="mt-4">
              <Alert variant="success">
                {t("auth.verification-requested.verification_email_resent_successfully")}
              </Alert>
            </div>
          )}
          {form.formError && (
            <div className="mt-4">
              <Alert variant="error">{form.formError}</Alert>
            </div>
          )}
          <form onSubmit={(event) => void form.handleSubmit(event)} className="mt-5 space-y-2" noValidate>
            {!signupEmail && (
              <Field id="verification-email" label={t("common.email")} error={form.errors.email}>
                <TextInput
                  id="verification-email"
                  type="email"
                  name="email"
                  autoComplete="email"
                  inputMode="email"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  value={form.values.email}
                  error={form.errors.email}
                  onChange={(event) => form.setValue("email", event.target.value)}
                />
              </Field>
            )}
            <Button type="submit" variant="secondary" className="w-full" loading={form.isSubmitting}>
              {t("auth.verification-requested.resend_verification_email")}
            </Button>
          </form>
        </>
      )}
      <p className="mt-4 text-center text-xs text-slate-500">
        {t("auth.signup.have_an_account")}{" "}
        <Link
          to="/auth/login"
          search={callbackUrl ? { callbackUrl } : {}}
          className="font-semibold text-slate-600 underline">
          {t("auth.signup.log_in")}
        </Link>
      </p>
    </>
  );
}
