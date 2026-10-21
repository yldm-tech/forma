import { describe, expect, test } from "vite-plus/test";
import { oauthErrorVariant } from "./oauth-error";

// forma2's modules/auth/lib/oauth-error.test.ts, plus the codes this server's callback reports.

describe("oauthErrorVariant", () => {
  test("returns null when the callback carried no error", () => {
    expect(oauthErrorVariant(undefined)).toBeNull();
    expect(oauthErrorVariant(null)).toBeNull();
    expect(oauthErrorVariant("")).toBeNull();
  });

  test.each(["account_not_linked", "OAuthAccountNotLinked"])("keeps the not-linked alert for %s", (code) => {
    expect(oauthErrorVariant(code)).toBe("account_not_linked");
  });

  test.each([
    "unable_to_create_user",
    "user_creation_failed",
    "missing_callback_url",
    "signin_without_invite_token",
    "invalid_invite_token",
    "invite_email_mismatch",
    "invite_token_validation_error",
  ])("tells the user to ask for an invite for %s", (code) => {
    expect(oauthErrorVariant(code)).toBe("signup_not_allowed");
  });

  test.each([
    "missing_default_team_id",
    "no_organization_found",
    "insufficient_role_permissions",
    "oauth_provider_not_found",
    "invalid_scope",
  ])("points at the instance configuration for %s", (code) => {
    expect(oauthErrorVariant(code)).toBe("misconfigured");
  });

  test.each([
    "state_mismatch",
    "invalid_code",
    "internal_server_error",
    "please_restart_the_process",
    "too_many_requests",
    "account_inactive",
    "oauth_code_verification_failed",
    "a_code_no_version_of_better_auth_has_ever_emitted",
  ])("falls back to the generic alert for %s", (code) => {
    expect(oauthErrorVariant(code)).toBe("generic");
  });
});
