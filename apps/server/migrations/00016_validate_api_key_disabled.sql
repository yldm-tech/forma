-- Validates api_keys_disabled_check, which 00012 added NOT VALID: VALIDATE CONSTRAINT takes only a SHARE UPDATE EXCLUSIVE lock, so reads and writes of api_keys go on while it scans, and it runs in its own transaction, as an online migration should. Every row passes: 00012 added both columns null, and the constraint has held for every write since.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE api_keys VALIDATE CONSTRAINT api_keys_disabled_check;

-- +goose Down

-- A validated constraint cannot be marked not valid again; 00012's Down drops it with its columns.
