import { useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { Link, createFileRoute, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { authApi, authOptionsQuery, resetSession } from "../../features/auth/api";
import {
  callbackPath,
  inviteTokenFromCallbackUrl,
  validatedCallbackUrl,
} from "../../features/auth/callback-url";
import { SSOButtons } from "../../features/auth/components/sso-buttons";
import {
  Alert,
  Button,
  Field,
  PasswordInput,
  TextInput,
  textLinkClasses,
} from "../../features/auth/components/ui";
import { signInFailure, twoFactorFailure } from "../../features/auth/errors";
import { redirectIfSignedIn } from "../../features/auth/guards";
import { readLastUsed, rememberLastUsed } from "../../features/auth/last-used";
import { oauthErrorVariant } from "../../features/auth/oauth-error";
import { type CallbackSearch, callbackSearch, stringParam } from "../../features/auth/search";
import { emailError, signInPasswordError } from "../../features/auth/validation";
import { type Validators, useForm } from "../../lib/form";
import { pickAttributionParams } from "../../lib/attribution";

interface LoginSearch extends CallbackSearch {
  /** The code a failed SSO callback reports. */
  error?: string;
  /** "1" after an email verification that did not sign this browser in. */
  verified?: string;
  /** Prefills the email field, as the invite page's sign-in link does. */
  email?: string;
}

export const Route = createFileRoute("/auth/login")({
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    ...callbackSearch(search),
    error: stringParam(search.error),
    verified: stringParam(search.verified),
    email: stringParam(search.email),
  }),
  beforeLoad: redirectIfSignedIn,
  loader: ({ context }) => context.queryClient.ensureQueryData(authOptionsQuery),
  component: LoginPage,
});

interface Credentials {
  email: string;
  password: string;
}

const CREDENTIAL_RULES: Validators<Credentials> = {
  email: emailError,
  password: signInPasswordError,
};

type Step = "credentials" | "totp" | "backup";

function LoginPage() {
  const { t } = useTranslation();
  const search = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: options } = useSuspenseQuery(authOptionsQuery);

  const origin = window.location.origin;
  // forma2 fell back to WEBAPP_URL itself, so an SSO sign-in without a callback lands on the root page.
  const callbackUrl = validatedCallbackUrl(search.callbackUrl, origin) ?? new URL("/", origin).toString();
  const inviteToken = inviteTokenFromCallbackUrl(callbackUrl, origin);
  const errorVariant = oauthErrorVariant(search.error);

  const [showEmailForm, setShowEmailForm] = useState(false);
  const [step, setStep] = useState<Step>("credentials");
  const [code, setCode] = useState("");
  const [codeError, setCodeError] = useState<string>();
  const [verifyingCode, setVerifyingCode] = useState(false);
  const [lastUsed] = useState(readLastUsed);
  const [ssoError, setSsoError] = useState<string>();
  const emailRef = useRef<HTMLInputElement>(null);

  const finishSignIn = async () => {
    resetSession(queryClient);
    await navigate({ href: callbackPath(callbackUrl, origin) });
  };

  const form = useForm<Credentials>({
    defaultValues: { email: search.email ?? "", password: "" },
    validators: CREDENTIAL_RULES,
    onSubmit: async ({ email, password }) => {
      rememberLastUsed("Email");
      try {
        const { status } = await authApi.signIn(email.toLowerCase(), password);
        if (status === "two_factor_required") {
          setCode("");
          setCodeError(undefined);
          setStep("totp");
          return;
        }
        await finishSignIn();
      } catch (error) {
        const failure = signInFailure(t, error);
        for (const [field, message] of Object.entries(failure.fields)) {
          form.setFieldError(field as keyof Credentials, message);
        }
        form.setFormError(failure.message);
      }
    },
  });

  useEffect(() => {
    if (showEmailForm && step === "credentials") emailRef.current?.focus();
  }, [showEmailForm, step]);

  const submitCode = async (event: FormEvent) => {
    event.preventDefault();
    if (!code.trim() || verifyingCode) return;
    setVerifyingCode(true);
    setCodeError(undefined);
    try {
      if (step === "backup") await authApi.verifyBackupCode(code.trim());
      else await authApi.verifyTotp(code);
      await finishSignIn();
    } catch (error) {
      const failure = twoFactorFailure(t, error);
      if (failure.restart) {
        setStep("credentials");
        form.setFormError(failure.message);
      } else {
        setCodeError(failure.message);
      }
    } finally {
      setVerifyingCode(false);
    }
  };

  const switchStep = (next: Step) => {
    setStep(next);
    setCode("");
    setCodeError(undefined);
  };

  const heading =
    step === "backup"
      ? t("auth.login.enter_your_backup_code")
      : step === "totp"
        ? t("auth.login.enter_your_two_factor_authentication_code")
        : t("auth.login.login_to_your_account");

  const oauthAlert = (() => {
    switch (errorVariant) {
      case "account_not_linked":
        return {
          title: t("auth.login.oauth_account_not_linked_title"),
          description: t("auth.login.oauth_account_not_linked_description"),
        };
      case "signup_not_allowed":
        return {
          title: t("auth.login.oauth_signup_not_allowed_title"),
          description: t("auth.login.oauth_signup_not_allowed_description"),
        };
      case "misconfigured":
        return {
          title: t("auth.login.oauth_misconfigured_title"),
          description: t("auth.login.oauth_misconfigured_description"),
        };
      case "generic":
        return {
          title: t("auth.login.oauth_generic_error_title"),
          description: t("auth.login.oauth_generic_error_description"),
        };
      default:
        return null;
    }
  })();

  return (
    <div className="text-center">
      <h1 className="mb-4 text-xl font-semibold text-balance text-slate-800">{heading}</h1>
      {search.verified === "1" && (
        <Alert variant="success" title={t("auth.login.email_verified_sign_in_title")}>
          <p>{t("auth.login.email_verified_sign_in_description")}</p>
        </Alert>
      )}
      {oauthAlert && step === "credentials" && (
        <Alert variant="error" title={oauthAlert.title}>
          <p>{oauthAlert.description}</p>
        </Alert>
      )}
      {form.formError && <Alert variant="error">{form.formError}</Alert>}
      {ssoError && <Alert variant="error">{ssoError}</Alert>}

      <div className="space-y-2">
        {step === "credentials" ? (
          <form onSubmit={(event) => void form.handleSubmit(event)} className="space-y-2" noValidate>
            {showEmailForm && (
              <div className="space-y-2">
                <Field id="login-email" label={t("common.email")} error={form.errors.email}>
                  <TextInput
                    id="login-email"
                    ref={emailRef}
                    type="email"
                    name="email"
                    autoComplete="email"
                    inputMode="email"
                    autoCapitalize="none"
                    autoCorrect="off"
                    spellCheck={false}
                    required
                    placeholder="work@email.com"
                    value={form.values.email}
                    error={form.errors.email}
                    onChange={(event) => form.setValue("email", event.target.value)}
                  />
                </Field>
                <Field id="login-password" label={t("common.password")} error={form.errors.password}>
                  <PasswordInput
                    id="login-password"
                    name="password"
                    autoComplete="current-password"
                    placeholder="*******"
                    required
                    value={form.values.password}
                    error={form.errors.password}
                    onChange={(event) => form.setValue("password", event.target.value)}
                  />
                </Field>
                {options.passwordResetEnabled && (
                  <div className="text-right">
                    <Link
                      to="/auth/forgot-password"
                      className="inline-flex min-h-6 items-center rounded-sm py-1 text-sm text-slate-500 hover:text-slate-800 focus-visible:ring-2 focus-visible:ring-slate-800 focus-visible:outline-hidden">
                      {t("auth.login.forgot_your_password")}
                    </Link>
                  </div>
                )}
              </div>
            )}
            {options.emailAuthEnabled && (
              <Button
                type={showEmailForm ? "submit" : "button"}
                onClick={showEmailForm ? undefined : () => setShowEmailForm(true)}
                className="w-full"
                loading={form.isSubmitting}>
                <span className="truncate">{t("auth.login.login_with_email")}</span>
                {lastUsed === "Email" && (
                  <span className="shrink-0 text-xs opacity-50">{t("auth.last_used")}</span>
                )}
              </Button>
            )}
          </form>
        ) : (
          <form onSubmit={(event) => void submitCode(event)} className="space-y-2" noValidate>
            {step === "totp" ? (
              <Field
                id="login-totp"
                label={t("auth.login.enter_your_two_factor_authentication_code")}
                error={codeError}
                hideLabel>
                <TextInput
                  id="login-totp"
                  name="totpCode"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  pattern="\d{6}"
                  maxLength={6}
                  // oxlint-disable-next-line jsx-a11y/no-autofocus -- the code field is the only thing on this step; focusing it is what a user expects after their password
                  autoFocus
                  className="text-center text-lg tracking-[0.5em]"
                  value={code}
                  error={codeError}
                  onChange={(event) => setCode(event.target.value.replace(/\D/g, "").slice(0, 6))}
                />
              </Field>
            ) : (
              <Field id="login-backup" label={t("auth.login.backup_code")} error={codeError} hideLabel>
                <TextInput
                  id="login-backup"
                  name="backupCode"
                  required
                  autoComplete="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  // oxlint-disable-next-line jsx-a11y/no-autofocus -- the code field is the only thing on this step
                  autoFocus
                  placeholder="XXXXX-XXXXX"
                  value={code}
                  error={codeError}
                  onChange={(event) => setCode(event.target.value)}
                />
              </Field>
            )}
            <Button type="submit" className="w-full" loading={verifyingCode} disabled={!code.trim()}>
              {t("common.submit")}
            </Button>
          </form>
        )}

        {step === "credentials" && (
          <SSOButtons
            providers={options.ssoProviders}
            oidcDisplayName={options.oidcDisplayName}
            returnToUrl={callbackUrl}
            source="signin"
            lastUsed={lastUsed}
            onError={setSsoError}
          />
        )}
      </div>

      {options.signupEnabled && step === "credentials" && (
        <div className="mt-9 text-center text-xs">
          <span className="leading-5 text-slate-500">{t("auth.login.new_to_forma")}</span>
          <br />
          <Link
            to="/auth/signup"
            // The marketing attribution of this page's address travels to the sign-up page, as forma2 forwarded it.
            search={{
              ...(inviteToken ? { inviteToken } : {}),
              ...pickAttributionParams(new URLSearchParams(window.location.search)),
            }}
            className={textLinkClasses}>
            {t("auth.login.create_an_account")}
          </Link>
        </div>
      )}

      {step === "totp" && (
        <div className="mt-9 text-center text-xs">
          <span className="leading-5 text-slate-500">{t("auth.login.lost_access")}</span>
          <div className="flex flex-col">
            <button type="button" className={textLinkClasses} onClick={() => switchStep("backup")}>
              {t("auth.login.use_a_backup_code")}
            </button>
            <button
              type="button"
              className={`mt-4 ${textLinkClasses}`}
              onClick={() => switchStep("credentials")}>
              {t("common.go_back")}
            </button>
          </div>
        </div>
      )}

      {step === "backup" && (
        <div className="mt-9 text-center text-xs">
          <button type="button" className={textLinkClasses} onClick={() => switchStep("totp")}>
            {t("common.go_back")}
          </button>
        </div>
      )}
    </div>
  );
}
