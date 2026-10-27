import type { TFunction } from "i18next";
import { ApiError } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import type { FieldError } from "../../lib/api";
import { serverFieldMessage, validationMessage } from "./validation";

/** Where a refused request is shown: under the fields it names, and as a message for the form. */
export interface Failure<F extends string> {
  fields: Partial<Record<F, string>>;
  message?: string;
}

const isCode = (error: unknown, code: string): error is ApiError =>
  error instanceof ApiError && error.code === code;

/** A constraint the server reported, in the words of the matching client rule, or as the server named it when no rule words it. */
const fieldMessage = (t: TFunction, error: FieldError): string => {
  const key = serverFieldMessage(error);
  return key ? validationMessage(t, key) : `${error.field}: ${error.rule}`;
};

/** A validation error is shown under the form's own fields; one about any other input, or any other error, as the form's message. */
const genericFailure = <F extends string>(t: TFunction, error: unknown, fields: readonly F[]): Failure<F> => {
  if (error instanceof ApiError && error.code === "validation_error") {
    const failure: Failure<F> = { fields: {} };
    for (const fieldError of error.fieldErrors) {
      const field = fields.find((name) => name === fieldError.field);
      if (field) failure.fields[field] = fieldMessage(t, fieldError);
      else failure.message = fieldMessage(t, fieldError);
    }
    if (failure.message || Object.keys(failure.fields).length > 0) return failure;
  }
  return { fields: {}, message: errorMessage(t, error) };
};

/** The email and password step of sign-in. An unverified address gets forma2's prompt to check the inbox, since the server has just resent the link. */
export const signInFailure = (t: TFunction, error: unknown): Failure<"email" | "password"> => {
  if (isCode(error, "email_not_verified")) {
    return { fields: {}, message: t("auth.login.please_verify_your_email_to_continue") };
  }
  return genericFailure(t, error, ["email", "password"] as const);
};

// Codes after which the pending sign-in is gone and the cookie cleared, so the user must start again. account_not_linked is an SSO recovery whose identity was linked to another account before the code was entered.
const PENDING_SIGN_IN_GONE = new Set([
  "too_many_attempts_request_new_code",
  "invalid_two_factor_cookie",
  "account_not_linked",
]);

/** The second step of sign-in. `restart` is set when the pending sign-in no longer exists. The account lock is a 429 with its own explanation, which is shown as the server words it. */
export const twoFactorFailure = (t: TFunction, error: unknown): { message: string; restart: boolean } => {
  if (error instanceof ApiError) {
    if (PENDING_SIGN_IN_GONE.has(error.code)) return { message: error.message, restart: true };
    if (error.code === "account_temporarily_locked") return { message: error.message, restart: false };
  }
  return { message: errorMessage(t, error), restart: false };
};

/**
 * forma2's surfaceSignupError: a blocked email domain and a breached password go under the field that caused them, an invite that stopped being usable gets the invite copy, and the rest is a message for the form. forma2 showed the bare `signup_disabled` code there; the server's sentence is shown instead.
 */
export const signupFailure = (t: TFunction, error: unknown): Failure<"name" | "email" | "password"> => {
  if (isCode(error, "email_domain_not_allowed")) {
    return { fields: { email: t("auth.signup.company_email_required") } };
  }
  if (isCode(error, "password_compromised")) {
    return { fields: { password: t("auth.password_compromised") } };
  }
  if (isCode(error, "invite_token_invalid")) {
    return { fields: {}, message: t("auth.invite.invite_not_found_description") };
  }
  return genericFailure(t, error, ["name", "email", "password"] as const);
};

/** The reset link was used, has expired or was never issued; the server answers the same for all three. */
export const isInvalidResetToken = (error: unknown): boolean => isCode(error, "invalid_password_reset_token");

/** The new-password form of a reset link. */
export const resetPasswordFailure = (t: TFunction, error: unknown): Failure<"password"> => {
  if (isCode(error, "password_compromised")) return { fields: {}, message: t("auth.password_compromised") };
  if (isCode(error, "invalid_password_reset_token")) {
    return { fields: {}, message: t("c.link_expired_description") };
  }
  return genericFailure(t, error, ["password"] as const);
};

/** A single sign-on flow that could not start. SAML offered without a configured connection gets forma2's saml_connection_error toast. */
export const ssoStartFailure = (t: TFunction, error: unknown): string =>
  isCode(error, "saml_connection_error") ? t("auth.saml_connection_error") : errorMessage(t, error);

/** Any other form: the forgot-password and resend-verification requests. */
export const emailFormFailure = (t: TFunction, error: unknown): Failure<"email"> =>
  genericFailure(t, error, ["email"] as const);

/** The change-password form on the profile page: a wrong current password goes under that field and a breached new one under the new password, as the sign-up form places it. */
export const changePasswordFailure = (
  t: TFunction,
  error: unknown
): Failure<"currentPassword" | "newPassword"> => {
  if (isCode(error, "invalid_password")) {
    return {
      fields: { currentPassword: t("workspace.settings.profile.password_change.incorrect_current_password") },
    };
  }
  if (isCode(error, "password_compromised")) {
    return { fields: { newPassword: t("auth.password_compromised") } };
  }
  return genericFailure(t, error, ["currentPassword", "newPassword"] as const);
};
