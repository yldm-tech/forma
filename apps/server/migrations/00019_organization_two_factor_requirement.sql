-- An organization's requirement that its members use two-factor authentication.
--
-- two_factor_enforced_at is null while two-factor authentication is optional, which every existing organization keeps. Once an owner requires it, the column holds the end of the grace period they chose: until then a member who signs in with a password and has not turned on two-factor authentication is reminded to, and from then on is refused everything in the organization except reading its name and leaving it, until they turn it on. Members who sign in through an external identity provider (SSO, SAML) cannot enrol here and are exempt; their provider decides how they authenticate. API keys are not users and are not affected.
--
-- A nullable column without a default makes ADD COLUMN a catalog change: no existing row is rewritten.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The ACCESS EXCLUSIVE lock ADD COLUMN needs waits behind any open reader of organizations (most authorization decisions read it), and a migration should fail fast rather than queue every later request behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE organizations ADD COLUMN two_factor_enforced_at timestamptz;

-- +goose Down

SET LOCAL lock_timeout = '1s';

ALTER TABLE organizations DROP COLUMN two_factor_enforced_at;
