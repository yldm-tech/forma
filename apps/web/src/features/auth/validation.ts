// forma2's input rules for the auth forms. forma2 wrote their messages in English inside its zod schemas; here each rule returns the key of its message (auth.validation.*), and the forms show it through validationMessage in the user's language.

import type { TFunction } from "i18next";
import type { FieldError } from "../../lib/api";
import type { Validators } from "../../lib/form";

export const PASSWORD_MIN_LENGTH = 8;
export const PASSWORD_MAX_LENGTH = 128;

export const MESSAGES = {
  invalidEmail: "auth.validation.invalid_email",
  passwordTooShort: "auth.validation.password_too_short",
  passwordTooLong: "auth.validation.password_too_long",
  passwordPattern: "auth.validation.password_needs_upper_and_digit",
  nameTooShort: "auth.validation.name_required",
  nameFormat: "auth.validation.name_invalid",
  captchaRequired: "auth.validation.captcha_required",
} as const;

export type ValidationKey = (typeof MESSAGES)[keyof typeof MESSAGES];

/** The user-facing message of a rule's key. Each key is spelled out so the translation scan sees it read. */
export const validationMessage = (t: TFunction, key: ValidationKey): string => {
  switch (key) {
    case "auth.validation.invalid_email":
      return t("auth.validation.invalid_email");
    case "auth.validation.password_too_short":
      return t("auth.validation.password_too_short", { min: PASSWORD_MIN_LENGTH });
    case "auth.validation.password_too_long":
      return t("auth.validation.password_too_long", { max: PASSWORD_MAX_LENGTH });
    case "auth.validation.password_needs_upper_and_digit":
      return t("auth.validation.password_needs_upper_and_digit");
    case "auth.validation.name_required":
      return t("auth.validation.name_required");
    case "auth.validation.name_invalid":
      return t("auth.validation.name_invalid");
    case "auth.validation.captcha_required":
      return t("auth.validation.captcha_required");
  }
};

/** A form's rules with their keys turned into messages, for useForm. Build it with useMemo on `t`, so the rules stay stable until the language changes. */
export const translatedRules = <V>(t: TFunction, rules: Validators<V>): Validators<V> => {
  const translated: Validators<V> = {};
  for (const name of Object.keys(rules) as (keyof V)[]) {
    const rule = rules[name];
    if (!rule) continue;
    translated[name] = (value, values) => {
      const key = rule(value, values);
      return key && validationMessage(t, key as ValidationKey);
    };
  }
  return translated;
};

// zod 4's z.email() pattern, which the Go server's userinput.ValidEmailFormat reproduces.
const EMAIL_PATTERN =
  /^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9-]*\.)+[A-Za-z]{2,}$/;

// ZUserName: letters, combining marks, space, and ' , . & ( ) digits -.
const NAME_PATTERN = /^[\p{L}\p{M} ',.&()\d-]+$/u;

// ZUserPassword's pattern. Without the s flag "." stops at line terminators, so a password containing one never matches, as on the server.
const PASSWORD_PATTERN = /^(?=.*[A-Z])(?=.*\d).*$/;

/** z.email(): the address is not trimmed, surrounding whitespace fails. */
export const emailError = (email: string): ValidationKey | undefined =>
  EMAIL_PATTERN.test(email) ? undefined : MESSAGES.invalidEmail;

/** The sign-in form's password rule: length only, so an older password that predates the composition rule can still sign in. Lengths are UTF-16 code units, as zod counts them. */
export const signInPasswordError = (password: string): ValidationKey | undefined => {
  if (password.length < PASSWORD_MIN_LENGTH) return MESSAGES.passwordTooShort;
  if (password.length > PASSWORD_MAX_LENGTH) return MESSAGES.passwordTooLong;
  return undefined;
};

/** ZUserPassword: 8 to 128 characters with at least one uppercase letter and one digit. */
export const passwordError = (password: string): ValidationKey | undefined =>
  signInPasswordError(password) ?? (PASSWORD_PATTERN.test(password) ? undefined : MESSAGES.passwordPattern);

/** ZUserName, which validates the trimmed value. */
export const nameError = (name: string): ValidationKey | undefined => {
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

/** The message key for a constraint the server reported on a field, the one the matching client rule returns; undefined for a constraint no rule words. */
export const serverFieldMessage = (error: FieldError): ValidationKey | undefined => {
  const field = error.field.toLowerCase();
  if (field === "email") return MESSAGES.invalidEmail;
  if (field === "name") return error.rule === "name_format" ? MESSAGES.nameFormat : MESSAGES.nameTooShort;
  if (field === "turnstiletoken") return MESSAGES.captchaRequired;
  if (field.includes("password")) {
    if (error.rule === "min" || error.rule === "required") return MESSAGES.passwordTooShort;
    if (error.rule === "max") return MESSAGES.passwordTooLong;
    if (error.rule === "password_composition") return MESSAGES.passwordPattern;
  }
  return undefined;
};
