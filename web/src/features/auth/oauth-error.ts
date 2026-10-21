/**
 * The alert the sign-in page shows for the `?error=<code>` a failed SSO callback lands on, forma2's getOAuthErrorVariant. Codes collapse into what the user can do about them; anything unrecognized is `generic`, so a new code degrades to "try again" rather than to silence.
 */
export type OAuthErrorVariant = "account_not_linked" | "signup_not_allowed" | "misconfigured" | "generic";

const VARIANT_BY_CODE = new Map<string, OAuthErrorVariant>([
  ["account_not_linked", "account_not_linked"],
  ["OAuthAccountNotLinked", "account_not_linked"],
  ["unable_to_create_user", "signup_not_allowed"],
  ["user_creation_failed", "signup_not_allowed"],
  ["missing_callback_url", "signup_not_allowed"],
  ["signin_without_invite_token", "signup_not_allowed"],
  ["invalid_invite_token", "signup_not_allowed"],
  ["invite_email_mismatch", "signup_not_allowed"],
  ["invite_token_validation_error", "signup_not_allowed"],
  ["missing_default_team_id", "misconfigured"],
  ["no_organization_found", "misconfigured"],
  ["insufficient_role_permissions", "misconfigured"],
  ["oauth_provider_not_found", "misconfigured"],
  ["invalid_scope", "misconfigured"],
]);

export const oauthErrorVariant = (error?: string | null): OAuthErrorVariant | null => {
  if (!error) return null;
  return VARIANT_BY_CODE.get(error) ?? "generic";
};
