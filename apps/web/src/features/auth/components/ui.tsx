import { Link, type LinkComponentProps } from "@tanstack/react-router";
import {
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type Ref,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { Wordmark } from "../../../components/ui/logo";

// The building blocks of the signed-out screens, after forma2's FormWrapper, Button, Input, PasswordInput, Alert and invite ContentLayout.

const join = (...classes: (string | false | undefined)[]) => classes.filter(Boolean).join(" ");

/** The card every signed-out form renders in. Mobile first: min-h-dvh, because 100vh on a phone is the large viewport and pushes a centred card under the browser chrome. The logo is the website's mark and wordmark and leads to the instance's own root in this tab: the website on forma.yldm.ai, the operator's page on a self-hosted instance. */
export function FormWrapper({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-dvh w-full flex-col items-center justify-center bg-slate-100 px-4 py-8 sm:px-6 sm:py-12">
      <main className="w-full max-w-sm rounded-xl bg-white p-6 shadow-2xl sm:p-8">
        <div className="mb-8 text-center">
          <a
            href="/"
            aria-label={t("common.forma_homepage")}
            className="focus-visible:ring-ring inline-block rounded-md text-3xl focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden sm:text-4xl">
            <Wordmark />
          </a>
        </div>
        {children}
      </main>
    </div>
  );
}

/** The invite page's message, in the same card as every other signed-out screen: a headline, what it means, and the actions under it, stacked. */
export function ContentLayout({
  headline,
  description,
  children,
}: {
  headline: string;
  description?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <FormWrapper>
      <div className="text-center">
        <h1 className="mb-3 text-xl font-semibold text-balance text-slate-800">{headline}</h1>
        {description && <div className="space-y-2 text-sm text-pretty text-slate-600">{description}</div>}
        {children && <div className="mt-6 flex flex-col gap-2">{children}</div>}
      </div>
    </FormWrapper>
  );
}

type Variant = "primary" | "secondary" | "ghost";

const VARIANTS: Record<Variant, string> = {
  primary: "bg-slate-900 text-white hover:bg-slate-700",
  secondary: "border border-slate-300 bg-white text-slate-800 hover:bg-slate-50",
  ghost: "text-slate-700 hover:bg-slate-100",
};

const buttonClasses = (variant: Variant, className?: string) =>
  join(
    "inline-flex h-11 min-w-0 items-center justify-center gap-2 rounded-md px-4 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-hidden disabled:cursor-not-allowed disabled:opacity-50 sm:h-9",
    VARIANTS[variant],
    className
  );

export function Button({
  variant = "primary",
  loading = false,
  className,
  disabled,
  children,
  type = "button",
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; loading?: boolean }) {
  return (
    <button
      type={type}
      className={buttonClasses(variant, className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}>
      {loading && (
        <span
          aria-hidden="true"
          className="size-4 shrink-0 animate-spin rounded-full border-2 border-current border-t-transparent"
        />
      )}
      {children}
    </button>
  );
}

/** A router link drawn as a button. */
export function ButtonLink({
  variant = "primary",
  className,
  ...rest
}: LinkComponentProps<"a"> & { variant?: Variant; className?: string }) {
  return <Link className={buttonClasses(variant, className)} {...rest} />;
}

export const textLinkClasses =
  "inline-flex min-h-6 items-center justify-center rounded-sm py-1 font-semibold text-slate-600 underline hover:text-slate-700 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-hidden";

/** A labelled input with its error under it. The input gets `id`, and `aria-describedby` points at the error when there is one. */
export function Field({
  id,
  label,
  error,
  children,
  hideLabel = false,
}: {
  id: string;
  label: string;
  error?: string;
  children: ReactNode;
  hideLabel?: boolean;
}) {
  return (
    <div className="w-full space-y-1 text-left">
      <label htmlFor={id} className={hideLabel ? "sr-only" : "block text-sm font-medium text-slate-800"}>
        {label}
      </label>
      {children}
      {error && (
        <p id={`${id}-error`} role="alert" className="text-sm text-red-600">
          {error}
        </p>
      )}
    </div>
  );
}

const inputClasses =
  "flex h-10 w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-base text-slate-800 placeholder:text-slate-500 focus:border-slate-500 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-red-500 sm:text-sm";

/** text-base below sm keeps iOS Safari from zooming the page when the field takes focus. */
export function TextInput({
  error,
  className,
  ref,
  ...rest
}: InputHTMLAttributes<HTMLInputElement> & { error?: string; ref?: Ref<HTMLInputElement> }) {
  return (
    <input
      ref={ref}
      className={join(inputClasses, className)}
      aria-invalid={error ? true : undefined}
      aria-describedby={error && rest.id ? `${rest.id}-error` : undefined}
      {...rest}
    />
  );
}

export function PasswordInput({
  error,
  ref,
  ...rest
}: Omit<InputHTMLAttributes<HTMLInputElement>, "type"> & { error?: string; ref?: Ref<HTMLInputElement> }) {
  const { t } = useTranslation();
  const [visible, setVisible] = useState(false);
  return (
    <div className="relative">
      <input
        ref={ref}
        type={visible ? "text" : "password"}
        className={join(inputClasses, "pr-11")}
        aria-invalid={error ? true : undefined}
        aria-describedby={error && rest.id ? `${rest.id}-error` : undefined}
        {...rest}
      />
      <button
        type="button"
        className="focus-visible:ring-ring absolute top-1/2 right-1 flex size-9 -translate-y-1/2 items-center justify-center rounded-md text-slate-400 hover:text-slate-600 focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden"
        aria-label={visible ? t("common.hide_password") : t("common.show_password")}
        aria-pressed={visible}
        onClick={() => setVisible((v) => !v)}>
        <svg
          aria-hidden="true"
          viewBox="0 0 24 24"
          className="size-5"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round">
          <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z" />
          <circle cx="12" cy="12" r="3" />
          {visible && <path d="m3 3 18 18" />}
        </svg>
      </button>
    </div>
  );
}

const ALERTS = {
  error: "border-red-200 bg-red-50 text-red-800",
  success: "border-green-200 bg-green-50 text-green-800",
  warning: "border-amber-200 bg-amber-50 text-amber-900",
} as const;

/** A message box. Errors are announced at once (role alert); other notices politely (role status). */
export function Alert({
  variant,
  title,
  children,
}: {
  variant: keyof typeof ALERTS;
  title?: string;
  children?: ReactNode;
}) {
  return (
    <div
      role={variant === "error" ? "alert" : "status"}
      className={join("mb-4 rounded-md border p-3 text-left text-sm", ALERTS[variant])}>
      {title && <p className="font-semibold">{title}</p>}
      {children && <div className={title ? "mt-1" : undefined}>{children}</div>}
    </div>
  );
}

/** The "Log in" button of the notice pages, returning to callbackUrl afterwards when there is one, with the email field filled in when the page knows the address. */
export function BackToLoginButton({ callbackUrl, email }: { callbackUrl?: string | null; email?: string }) {
  const { t } = useTranslation();
  return (
    <ButtonLink
      to="/auth/login"
      search={{ ...(callbackUrl ? { callbackUrl } : {}), ...(email ? { email } : {}) }}
      className="w-full">
      {t("auth.signup.log_in")}
    </ButtonLink>
  );
}
