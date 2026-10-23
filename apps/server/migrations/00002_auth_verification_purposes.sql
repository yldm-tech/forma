-- Single-use tokens the credential module adds beside the baseline's four:
-- - two_factor: a sign-in that passed the password check and waits for a second factor (forma2: Better Auth's two-factor cookie and its verification value).
-- - signup_intent: proof that a browser created the account it later verifies, which alone may be signed in by the verification link (forma2: the signed signup_intent cookie).
-- - account_deletion: the emailed confirmation that deletes an account with no password to confirm with (forma2: Better Auth's delete-account verification value).

-- +goose Up

ALTER TABLE verifications DROP CONSTRAINT verifications_purpose_check;
ALTER TABLE verifications ADD CONSTRAINT verifications_purpose_check
    CHECK (purpose IN ('email_verification', 'email_change', 'password_reset', 'sso_recovery', 'two_factor', 'signup_intent', 'account_deletion'));
-- Every lookup but the token itself is "this user's outstanding tokens of one kind" (revoking them on a password or email change).
CREATE INDEX verifications_user_id_purpose_idx ON verifications (user_id, purpose);
DROP INDEX verifications_user_id_idx;

-- +goose Down

DELETE FROM verifications WHERE purpose IN ('two_factor', 'signup_intent', 'account_deletion');
CREATE INDEX verifications_user_id_idx ON verifications (user_id);
DROP INDEX verifications_user_id_purpose_idx;
ALTER TABLE verifications DROP CONSTRAINT verifications_purpose_check;
ALTER TABLE verifications ADD CONSTRAINT verifications_purpose_check
    CHECK (purpose IN ('email_verification', 'email_change', 'password_reset', 'sso_recovery'));
