import { useSuspenseQuery } from "@tanstack/react-query";
import { Link, createFileRoute, notFound, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { authApi, authOptionsQuery } from "../../features/auth/api";
import { inviteUrl } from "../../features/auth/callback-url";
import { PasswordChecks } from "../../features/auth/components/password-checks";
import { SSOButtons } from "../../features/auth/components/sso-buttons";
import { Turnstile } from "../../features/auth/components/turnstile";
import {
  Alert,
  Button,
  Field,
  PasswordInput,
  TextInput,
  textLinkClasses,
} from "../../features/auth/components/ui";
import { signupFailure } from "../../features/auth/errors";
import { redirectIfSignedIn, signupAvailable } from "../../features/auth/guards";
import { withSignupEmail } from "../../features/auth/history-state";
import { readLastUsed } from "../../features/auth/last-used";
import { stringParam } from "../../features/auth/search";
import { emailError, nameError, passwordError } from "../../features/auth/validation";
import { type Validators, useForm } from "../../lib/form";

interface SignupSearch {
  inviteToken?: string;
  /** Prefills the email field, as the invite page's sign-up link does. */
  email?: string;
  /** email_domain_not_allowed when an SSO sign-up was refused for a personal address. */
  error?: string;
}

export const Route = createFileRoute("/auth/signup")({
  validateSearch: (search: Record<string, unknown>): SignupSearch => ({
    inviteToken: stringParam(search.inviteToken),
    email: stringParam(search.email),
    error: stringParam(search.error),
  }),
  beforeLoad: redirectIfSignedIn,
  loaderDeps: ({ search }) => ({ inviteToken: search.inviteToken }),
  loader: async ({ context, deps }) => {
    const options = await context.queryClient.ensureQueryData(authOptionsQuery);
    if (!(await signupAvailable(options.signupEnabled, deps.inviteToken))) throw notFound();
  },
  component: SignupPage,
});

interface SignupValues {
  name: string;
  email: string;
  password: string;
}

const SIGNUP_RULES: Validators<SignupValues> = {
  name: nameError,
  email: emailError,
  password: passwordError,
};

function SignupPage() {
  const { t, i18n } = useTranslation();
  const search = Route.useSearch();
  const navigate = useNavigate();
  const { data: options } = useSuspenseQuery(authOptionsQuery);

  const origin = window.location.origin;
  const inviteToken = search.inviteToken;
  // An invited visitor returns to the invite after signing in; everyone else to the root page.
  const returnToUrl = inviteToken ? inviteUrl(inviteToken, origin) : new URL("/", origin).toString();

  const [showEmailForm, setShowEmailForm] = useState(false);
  const [turnstileToken, setTurnstileToken] = useState<string>();
  const [turnstileReset, setTurnstileReset] = useState(0);
  const [subscribeToSecurityUpdates, setSubscribeToSecurityUpdates] = useState(false);
  const [lastUsed] = useState(readLastUsed);
  const [ssoError, setSsoError] = useState<string>();
  const turnstileSiteKey = options.turnstileSiteKey;

  const form = useForm<SignupValues>({
    defaultValues: { name: "", email: search.email ?? "", password: "" },
    validators: SIGNUP_RULES,
    onSubmit: async ({ name, email, password }) => {
      if (turnstileSiteKey && !turnstileToken) {
        form.setFormError(t("auth.signup.please_verify_captcha"));
        return;
      }
      const normalizedEmail = email.toLowerCase();
      try {
        await authApi.signup({
          name,
          email: normalizedEmail,
          password,
          inviteToken,
          userLocale: i18n.language,
          turnstileToken,
          subscribeToSecurityUpdates,
        });
      } catch (error) {
        // A token is single-use, so a refused attempt needs a fresh challenge.
        if (turnstileSiteKey) {
          setTurnstileToken(undefined);
          setTurnstileReset((n) => n + 1);
        }
        const failure = signupFailure(t, error);
        for (const [field, message] of Object.entries(failure.fields)) {
          form.setFieldError(field as keyof SignupValues, message);
        }
        form.setFormError(failure.message);
        return;
      }
      // The answer is the same whether or not the address already had an account, and so is where the visitor goes next. Both screens carry the invite back, for an invited visitor who already has an account and must sign in to reach it.
      const callbackUrl = inviteToken ? returnToUrl : undefined;
      await navigate({
        to: options.emailVerificationRequired
          ? "/auth/verification-requested"
          : "/auth/signup-without-verification-success",
        search: callbackUrl ? { callbackUrl } : {},
        state: withSignupEmail(normalizedEmail),
      });
    },
  });

  const showForm = options.emailAuthEnabled && showEmailForm;

  return (
    <div className="text-center">
      <h1 className="mb-4 text-xl font-semibold text-balance text-slate-800">{t("auth.signup.title")}</h1>
      {search.error === "email_domain_not_allowed" && (
        <Alert variant="error">{t("auth.signup.company_email_required")}</Alert>
      )}
      {form.formError && <Alert variant="error">{form.formError}</Alert>}
      {ssoError && <Alert variant="error">{ssoError}</Alert>}

      {options.emailAuthEnabled && (
        <form onSubmit={(event) => void form.handleSubmit(event)} className="mb-2" noValidate>
          {showForm && (
            <div>
              <div className="space-y-2">
                <Field id="signup-name" label={t("common.full_name")} error={form.errors.name}>
                  <TextInput
                    id="signup-name"
                    name="name"
                    autoComplete="name"
                    // oxlint-disable-next-line jsx-a11y/no-autofocus -- revealed by the user's own click on "Continue with Email", as forma2 focused it
                    autoFocus
                    placeholder="Full name"
                    value={form.values.name}
                    error={form.errors.name}
                    onChange={(event) => form.setValue("name", event.target.value)}
                  />
                </Field>
                <Field id="signup-email" label={t("common.email")} error={form.errors.email}>
                  <TextInput
                    id="signup-email"
                    type="email"
                    name="email"
                    autoComplete="email"
                    inputMode="email"
                    autoCapitalize="none"
                    autoCorrect="off"
                    spellCheck={false}
                    placeholder="work@email.com"
                    value={form.values.email}
                    error={form.errors.email}
                    onChange={(event) => form.setValue("email", event.target.value)}
                  />
                </Field>
                <Field id="signup-password" label={t("common.password")} error={form.errors.password}>
                  <PasswordInput
                    id="signup-password"
                    name="password"
                    // This form creates the account, so a password manager should offer to generate and store one, not fill in the current one.
                    autoComplete="new-password"
                    placeholder="*******"
                    required
                    value={form.values.password}
                    error={form.errors.password}
                    onChange={(event) => form.setValue("password", event.target.value)}
                  />
                </Field>
              </div>
              <PasswordChecks password={form.values.password} />
            </div>
          )}
          {showForm && turnstileSiteKey && (
            <Turnstile
              siteKey={turnstileSiteKey}
              resetKey={turnstileReset}
              onToken={setTurnstileToken}
              onError={() => form.setFormError(t("auth.signup.captcha_failed"))}
            />
          )}
          {showForm && (
            <label
              htmlFor="security-updates"
              className="my-4 flex cursor-pointer items-start gap-x-3 rounded-md border border-slate-200 bg-slate-100 p-3 text-left">
              <input
                id="security-updates"
                type="checkbox"
                checked={subscribeToSecurityUpdates}
                onChange={(event) => setSubscribeToSecurityUpdates(event.target.checked)}
                className="mt-0.5 size-4 shrink-0"
              />
              <span className="text-sm font-medium text-slate-700">
                {t("auth.signup.security_updates_title")}
                <span className="block text-xs font-normal text-slate-600">
                  {t("auth.signup.security_updates_description")}
                </span>
              </span>
            </label>
          )}
          <Button
            type={showForm ? "submit" : "button"}
            onClick={showForm ? undefined : () => setShowEmailForm(true)}
            disabled={showForm && !form.isValid}
            loading={showForm && form.isSubmitting}
            className="w-full">
            {t("auth.continue_with_email")}
          </Button>
        </form>
      )}

      <SSOButtons
        providers={options.ssoProviders}
        oidcDisplayName={options.oidcDisplayName}
        returnToUrl={returnToUrl}
        source="signup"
        lastUsed={lastUsed}
        onError={setSsoError}
      />

      <div className="mt-9 text-center text-xs">
        <span className="leading-5 text-slate-500">{t("auth.signup.have_an_account")}</span>
        <br />
        <Link
          to="/auth/login"
          search={inviteToken ? { callbackUrl: returnToUrl } : {}}
          className={textLinkClasses}>
          {t("auth.signup.log_in")}
        </Link>
      </div>
    </div>
  );
}
