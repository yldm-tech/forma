import { useSuspenseQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { pickAttributionParams } from "../../../lib/attribution";
import { type Validators, useForm } from "../../../lib/form";
import { authApi, authOptionsQuery } from "../api";
import { inviteUrl } from "../callback-url";
import { signupFailure } from "../errors";
import { withSignupEmail } from "../history-state";
import { readLastUsed } from "../last-used";
import type { CallbackSearch } from "../search";
import { emailError, nameError, passwordError } from "../validation";
import { PasswordChecks } from "./password-checks";
import { SSOButtons } from "./sso-buttons";
import { Turnstile } from "./turnstile";
import { Alert, Button, Field, PasswordInput, TextInput, textLinkClasses } from "./ui";
import { appHref } from "../../../lib/base-path";

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

/** What the page passes on from its address: the invite being accepted, an email to prefill (the invite page's sign-up link), and email_domain_not_allowed when an SSO sign-up was refused for a personal address. */
export interface SignupFormSearch {
  inviteToken?: string;
  email?: string;
  error?: string;
}

/**
 * forma2's SignupForm, shared by /auth/signup and the first administrator's /setup/signup. Forma Cloud offers the monthly product update emails where other instances offer the security ones, and the instance's terms and privacy links sit under the form.
 */
export function SignupForm({ search }: { search: SignupFormSearch }) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { data: options } = useSuspenseQuery(authOptionsQuery);

  const origin = window.location.origin;
  const inviteToken = search.inviteToken;
  // An invited visitor returns to the invite after signing in; everyone else to the root page.
  const returnToUrl = inviteToken ? inviteUrl(inviteToken, origin) : new URL(appHref("/"), origin).toString();
  // The marketing attribution of this page's address travels to the sign-in page with the link, as forma2 forwarded it.
  const loginSearch: CallbackSearch = {
    ...(inviteToken ? { callbackUrl: returnToUrl } : {}),
    ...pickAttributionParams(new URLSearchParams(window.location.search)),
  };

  const [showEmailForm, setShowEmailForm] = useState(false);
  const [turnstileToken, setTurnstileToken] = useState<string>();
  const [turnstileReset, setTurnstileReset] = useState(0);
  const [subscribeToSecurityUpdates, setSubscribeToSecurityUpdates] = useState(false);
  const [subscribeToProductUpdates, setSubscribeToProductUpdates] = useState(false);
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
          subscribeToProductUpdates,
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
          {showForm &&
            (options.isFormaCloud ? (
              <UpdatesOptIn
                id="product-updates"
                checked={subscribeToProductUpdates}
                onChange={setSubscribeToProductUpdates}
                title={t("auth.signup.product_updates_title")}
                description={t("auth.signup.product_updates_description")}
              />
            ) : (
              <UpdatesOptIn
                id="security-updates"
                checked={subscribeToSecurityUpdates}
                onChange={setSubscribeToSecurityUpdates}
                title={t("auth.signup.security_updates_title")}
                description={t("auth.signup.security_updates_description")}
              />
            ))}
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

      <TermsPrivacyLinks termsUrl={options.termsUrl} privacyUrl={options.privacyUrl} />

      <div className="mt-9 text-center text-xs">
        <span className="leading-5 text-slate-500">{t("auth.signup.have_an_account")}</span>
        <br />
        <Link to="/auth/login" search={loginSearch} className={textLinkClasses}>
          {t("auth.signup.log_in")}
        </Link>
      </div>
    </div>
  );
}

/** One of the two mailing list opt-ins, forma2's checkbox card. */
const UpdatesOptIn = ({
  id,
  checked,
  onChange,
  title,
  description,
}: {
  id: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  title: string;
  description: string;
}) => (
  <label
    htmlFor={id}
    className="my-4 flex cursor-pointer items-start gap-x-3 rounded-md border border-slate-200 bg-slate-100 p-3 text-left">
    <input
      id={id}
      type="checkbox"
      checked={checked}
      onChange={(event) => onChange(event.target.checked)}
      className="mt-0.5 size-4 shrink-0"
    />
    <span className="text-sm font-medium text-slate-700">
      {title}
      <span className="block text-xs font-normal text-slate-600">{description}</span>
    </span>
  </label>
);

/** forma2's TermsPrivacyLinks: the instance's TERMS_URL and PRIVACY_URL, each when set, opened in a new tab. */
export const TermsPrivacyLinks = ({ termsUrl, privacyUrl }: { termsUrl?: string; privacyUrl?: string }) => {
  const { t } = useTranslation();
  if (!termsUrl && !privacyUrl) return null;
  const linkClasses =
    "inline-flex min-h-6 items-center rounded-sm py-1 font-semibold hover:text-slate-700 focus-visible:ring-2 focus-visible:ring-brand-dark focus-visible:outline-hidden";
  return (
    <div className="mt-3 text-center text-xs text-pretty text-slate-500">
      {termsUrl && (
        <a className={linkClasses} href={termsUrl} rel="noreferrer" target="_blank">
          {t("auth.signup.terms_of_service")}
        </a>
      )}
      {termsUrl && privacyUrl && <span> {t("common.and")} </span>}
      {privacyUrl && (
        <a className={linkClasses} href={privacyUrl} rel="noreferrer" target="_blank">
          {t("auth.signup.privacy_policy")}
        </a>
      )}
      <hr className="mt-3" />
    </div>
  );
};
