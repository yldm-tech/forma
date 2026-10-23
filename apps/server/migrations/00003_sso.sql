-- Single sign-on (Google, GitHub, Azure AD, generic OIDC):
-- - oauth_state: one authorization request in flight, keyed by the hash of the browser's state cookie; its payload holds the state, PKCE verifier, nonce, provider and return URL (forma2: Better Auth's signed state cookie and its verification value).
-- - A credential identity may outlive its password. SSO recovery of an account whose address was never proven removes the password but keeps the row, as forma2 nulled Account.password rather than deleting it: the row is what still lets the owner set a new password through a reset while email and password sign-in is enabled.

-- +goose Up

ALTER TABLE verifications DROP CONSTRAINT verifications_purpose_check;
ALTER TABLE verifications ADD CONSTRAINT verifications_purpose_check
    CHECK (purpose IN ('email_verification', 'email_change', 'password_reset', 'sso_recovery', 'two_factor', 'signup_intent', 'account_deletion', 'oauth_state'));

ALTER TABLE user_identities DROP CONSTRAINT user_identities_check;
ALTER TABLE user_identities ADD CONSTRAINT user_identities_password_hash_check
    CHECK (provider = 'credential' OR password_hash IS NULL);

-- +goose Down

DELETE FROM user_identities WHERE provider = 'credential' AND password_hash IS NULL;
ALTER TABLE user_identities DROP CONSTRAINT user_identities_password_hash_check;
ALTER TABLE user_identities ADD CONSTRAINT user_identities_check
    CHECK ((provider = 'credential') = (password_hash IS NOT NULL));

DELETE FROM verifications WHERE purpose = 'oauth_state';
ALTER TABLE verifications DROP CONSTRAINT verifications_purpose_check;
ALTER TABLE verifications ADD CONSTRAINT verifications_purpose_check
    CHECK (purpose IN ('email_verification', 'email_change', 'password_reset', 'sso_recovery', 'two_factor', 'signup_intent', 'account_deletion'));
