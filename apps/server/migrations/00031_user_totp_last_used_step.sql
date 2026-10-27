-- The time step of the last TOTP code accepted for a user (RFC 6238 section 5.2), so a code works once: sign-in, enabling and disabling two-factor authentication refuse a code from that step or an earlier one, inside the same locked transaction that accepts it (internal/auth acceptTOTP). Before this, a code stayed good for its whole acceptance window, about 90 seconds at sign-in, and one code could complete two separate pending sign-ins.
--
-- Null until a code is accepted, and again after setup stores a new key, whose steps have nothing to do with the old one's. Existing rows start null, so the first code after the upgrade is accepted as before.
--
-- A nullable column without a default makes ADD COLUMN a catalog change: no existing row is rewritten.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The ACCESS EXCLUSIVE lock ADD COLUMN needs waits behind any open reader of user_totp (every second-factor sign-in reads it), and a migration should fail fast rather than queue every later sign-in behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE user_totp ADD COLUMN last_used_step bigint;

-- +goose Down

SET LOCAL lock_timeout = '1s';

-- A release before this one does not read the column; dropping it only forgets which steps were used, so a code accepted in the last 90 seconds is accepted once more.
-- squawk-ignore ban-drop-column
ALTER TABLE user_totp DROP COLUMN last_used_step;
