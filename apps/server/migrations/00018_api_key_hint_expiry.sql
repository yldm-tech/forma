-- A key's hint and its optional expiry.
--
-- key_hint is the last four characters of the plaintext key, written when the key is created, so the API Keys page can show "fbk_…abcd" and an owner can match a key found in a log, a repository or a leak report against the list. Four base64url characters give away 24 of the secret's 256 bits, nothing a guess can use. Keys created before this migration have no hint (null): their plaintext was never stored, so it cannot be recovered.
--
-- expires_at is when the key stops working, chosen when it is created, or null for a key that works until it is deleted (every existing key, and the default). An expired key is refused like a disabled one: it never authenticates again, and stays listed so an owner can see what stopped working and replace it.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The ACCESS EXCLUSIVE lock ADD COLUMN needs waits behind any open reader of api_keys (every API request reads it), and a migration should fail fast rather than queue every later request behind it.
SET LOCAL lock_timeout = '1s';

-- The hint's check is a column constraint, which a new, all-null column satisfies.
ALTER TABLE api_keys
    ADD COLUMN key_hint text
        CONSTRAINT api_keys_key_hint_check CHECK (char_length(key_hint) = 4),
    ADD COLUMN expires_at timestamptz;

-- +goose Down

SET LOCAL lock_timeout = '1s';

-- A release before this one has no notion of expiry and would accept an expired key again. The expired keys are deleted instead, which their grants and SpiceDB relationships follow through the cascade and the projection triggers. A key that has not expired yet keeps working, without an expiry, until it is deleted.
DELETE FROM api_keys WHERE expires_at <= now();

ALTER TABLE api_keys
    DROP COLUMN expires_at,
    DROP COLUMN key_hint;
