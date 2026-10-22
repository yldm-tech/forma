import type { TFunction } from "i18next";
import { beforeAll, describe, expect, test } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import { initI18n } from "../../lib/i18n";
import {
  emailFormFailure,
  resetPasswordFailure,
  signInFailure,
  signupFailure,
  twoFactorFailure,
} from "./errors";
import { MESSAGES } from "./validation";

let t: TFunction;

beforeAll(async () => {
  t = (await initI18n("en-US")).t;
});

const apiError = (status: number, code: string, message: string, details?: Record<string, unknown>) =>
  new ApiError(status, code, message, details);

const validation = (...fields: { field: string; rule: string }[]) =>
  apiError(400, "validation_error", "Invalid input", { fields });

describe("signInFailure", () => {
  test("points an unverified address at its inbox", () => {
    expect(signInFailure(t, apiError(403, "email_not_verified", "Email not verified"))).toEqual({
      fields: {},
      message: "Please verify your email address. We've sent you a verification link.",
    });
  });

  test("shows the server's wording for wrong credentials and an inactive account", () => {
    expect(
      signInFailure(t, apiError(401, "invalid_email_or_password", "Invalid email or password")).message
    ).toBe("Invalid email or password");
    expect(signInFailure(t, apiError(403, "account_inactive", "Your account is inactive.")).message).toBe(
      "Your account is inactive."
    );
  });

  test("words a rate limit as forma2 did", () => {
    expect(signInFailure(t, new ApiError(429, "too_many_requests", "x", undefined, 30)).message).toBe(
      "Maximum number of requests reached. Please try again later."
    );
  });

  test("never shows the internals of a server failure or a network error", () => {
    const generic = "Something went wrong. Please try again.";
    expect(signInFailure(t, apiError(500, "internal_server_error", "pq: deadlock")).message).toBe(generic);
    expect(signInFailure(t, new TypeError("Failed to fetch")).message).toBe(generic);
  });

  test("puts a field the server refused under that field", () => {
    expect(signInFailure(t, validation({ field: "email", rule: "email" }))).toEqual({
      fields: { email: MESSAGES.invalidEmail },
    });
  });
});

describe("twoFactorFailure", () => {
  test("keeps the user on the code step for a wrong code", () => {
    expect(twoFactorFailure(t, apiError(400, "invalid_code", "Invalid code"))).toEqual({
      message: "Invalid code",
      restart: false,
    });
  });

  test.each([
    ["too_many_attempts_request_new_code", "Too many attempts. Please request a new code."],
    ["invalid_two_factor_cookie", "Invalid two factor cookie"],
  ])("sends the user back to their password when the pending sign-in is gone (%s)", (code, message) => {
    expect(twoFactorFailure(t, apiError(400, code, message))).toEqual({ message, restart: true });
  });

  test("explains the account lock in the server's words rather than as a generic rate limit", () => {
    const locked = new ApiError(429, "account_temporarily_locked", "Your account is temporarily locked.");
    expect(twoFactorFailure(t, locked)).toEqual({
      message: "Your account is temporarily locked.",
      restart: false,
    });
  });
});

describe("signupFailure", () => {
  test("puts a blocked domain under the email field", () => {
    expect(signupFailure(t, apiError(400, "email_domain_not_allowed", "x"))).toEqual({
      fields: { email: "Please sign up with your company email address." },
    });
  });

  test("puts a breached password under the password field", () => {
    expect(signupFailure(t, apiError(400, "password_compromised", "x"))).toEqual({
      fields: { password: "This password has appeared in a data breach. Please choose a different one." },
    });
  });

  test("words an unusable invite with the invite copy, never naming why", () => {
    expect(signupFailure(t, apiError(400, "invite_token_invalid", "x"))).toEqual({
      fields: {},
      message: "The invitation code cannot be found or has already been used.",
    });
  });

  test("shows a closed sign-up as a sentence, not forma2's bare code", () => {
    expect(
      signupFailure(t, apiError(400, "signup_disabled", "Signup is disabled on this instance.")).message
    ).toBe("Signup is disabled on this instance.");
  });

  test("maps validation errors onto the form's fields and anything else to the form", () => {
    expect(
      signupFailure(
        t,
        validation(
          { field: "name", rule: "name_format" },
          { field: "password", rule: "password_composition" },
          { field: "turnstileToken", rule: "required" }
        )
      )
    ).toEqual({
      fields: { name: MESSAGES.nameFormat, password: MESSAGES.passwordPattern },
      message: MESSAGES.captchaRequired,
    });
  });
});

describe("resetPasswordFailure", () => {
  test("shows a breached password", () => {
    expect(resetPasswordFailure(t, apiError(400, "password_compromised", "x")).message).toBe(
      "This password has appeared in a data breach. Please choose a different one."
    );
  });

  test("shows an invalid or expired link as forma2's expired-link copy", () => {
    expect(resetPasswordFailure(t, apiError(400, "invalid_password_reset_token", "x")).message).toBe(
      "The link you used is no longer valid."
    );
  });
});

test("emailFormFailure shows a disabled reset in the server's words", () => {
  expect(
    emailFormFailure(t, apiError(403, "operation_not_allowed", "Password reset is disabled")).message
  ).toBe("Password reset is disabled");
});
