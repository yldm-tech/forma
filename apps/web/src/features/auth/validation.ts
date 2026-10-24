// forma2's input rules for the auth forms, with their messages. forma2 wrote these messages in English inside its zod schemas (packages/types/user.ts and the forms' own schemas) rather than in the locale catalogues, and zod supplied the rest, so they are carried over verbatim and are not translated.

import type { FieldError } from "../../lib/api";

export const PASSWORD_MIN_LENGTH = 8;
export const PASSWORD_MAX_LENGTH = 128;

export const MESSAGES = {
  invalidEmail: "Invalid email address",
  passwordTooShort: "Password must be at least 8 characters long",
  passwordTooLong: "Password must be 128 characters or less",
  passwordPattern: "Invalid string: must match pattern /^(?=.*[A-Z])(?=.*\\d).*$/",
  nameTooShort: "Name should be at least 1 character long",
  nameFormat: "Invalid name format",
  captchaRequired: "CAPTCHA verification required",
} as const;

// zod 4's z.email() pattern, which the Go server's userinput.ValidEmailFormat reproduces.
const EMAIL_PATTERN =
  /^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9-]*\.)+[A-Za-z]{2,}$/;

// ZUserName: letters, combining marks, space, and ' , . & ( ) digits -.
const NAME_PATTERN = /^[\p{L}\p{M} ',.&()\d-]+$/u;

// ZUserPassword's pattern. Without the s flag "." stops at line terminators, so a password containing one never matches, as on the server.
const PASSWORD_PATTERN = /^(?=.*[A-Z])(?=.*\d).*$/;

/** z.email(): the address is not trimmed, surrounding whitespace fails. */
export const emailError = (email: string): string | undefined =>
  EMAIL_PATTERN.test(email) ? undefined : MESSAGES.invalidEmail;

/** The sign-in form's password rule: length only, so an older password that predates the composition rule can still sign in. Lengths are UTF-16 code units, as zod counts them. */
export const signInPasswordError = (password: string): string | undefined => {
  if (password.length < PASSWORD_MIN_LENGTH) return MESSAGES.passwordTooShort;
  if (password.length > PASSWORD_MAX_LENGTH) return MESSAGES.passwordTooLong;
  return undefined;
};

/** ZUserPassword: 8 to 128 characters with at least one uppercase letter and one digit. */
export const passwordError = (password: string): string | undefined =>
  signInPasswordError(password) ?? (PASSWORD_PATTERN.test(password) ? undefined : MESSAGES.passwordPattern);

/** ZUserName, which validates the trimmed value. */
export const nameError = (name: string): string | undefined => {
  const trimmed = name.trim();
  if (trimmed.length < 1) return MESSAGES.nameTooShort;
  if (!NAME_PATTERN.test(trimmed)) return MESSAGES.nameFormat;
  return undefined;
};

export interface PasswordCheck {
  key:
    | "auth.signup.password_validation_uppercase_and_lowercase"
    | "auth.signup.password_validation_minimum_8_and_maximum_128_characters"
    | "auth.signup.password_validation_contain_at_least_1_number";
  passed: boolean;
}

/**
 * The checklist under a new password, as forma2's PasswordChecks drew it. It is guidance, not the rule: it asks for a lowercase letter, which the schema does not require, and does not show the schema's uppercase-and-digit pattern on its own line.
 */
export const passwordChecks = (password: string): PasswordCheck[] => [
  {
    key: "auth.signup.password_validation_uppercase_and_lowercase",
    passed: /^(?=.*[A-Z])(?=.*[a-z])/.test(password),
  },
  {
    key: "auth.signup.password_validation_minimum_8_and_maximum_128_characters",
    passed: password.length >= PASSWORD_MIN_LENGTH && password.length <= PASSWORD_MAX_LENGTH,
  },
  { key: "auth.signup.password_validation_contain_at_least_1_number", passed: /\d/.test(password) },
];

/** The message for a constraint the server reported on a field, worded as the matching client rule words it. */
export const serverFieldMessage = (error: FieldError): string => {
  const field = error.field.toLowerCase();
  if (field === "email") return MESSAGES.invalidEmail;
  if (field === "name") return error.rule === "name_format" ? MESSAGES.nameFormat : MESSAGES.nameTooShort;
  if (field === "turnstiletoken") return MESSAGES.captchaRequired;
  if (field.includes("password")) {
    if (error.rule === "min" || error.rule === "required") return MESSAGES.passwordTooShort;
    if (error.rule === "max") return MESSAGES.passwordTooLong;
    if (error.rule === "password_composition") return MESSAGES.passwordPattern;
  }
  return `${error.field}: ${error.rule}`;
};
