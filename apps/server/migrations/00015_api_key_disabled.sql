-- API keys turned off because their creator lost the right to create them.
--
-- A key belongs to its organization, but it was created by a member who could manage API keys (an owner or a manager). When that member is removed from the organization, leaves it, deletes their account or is demoted below manager, the server now disables every key they created there, in the same transaction as the membership change, and records an audit event for each (apikey/disable). disabled_at is when, disabled_reason why: creator_removed, creator_left, creator_deleted or creator_demoted. A disabled key never authenticates again but stays listed, with its grants, so an owner can see what stopped working and replace it. Both are null for an enabled key, and only together set.
--
-- Existing keys are left enabled, whoever created them: disabling keys whose creator already left would break integrations without warning. The API Keys page shows those as created by a former member, so an owner can find and delete them.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. Adding nullable columns without a default is a catalog change, but the ACCESS EXCLUSIVE lock it needs waits behind any open reader of api_keys (every API request reads it), and a migration should fail fast rather than queue every later request behind it.
SET LOCAL lock_timeout = '1s';

-- The reason's own check is a column constraint, which a new, all-null column satisfies; the check that the two columns are set together is a table constraint, added NOT VALID so that adding it takes no scan, and validated by 00013 in a transaction of its own. It holds for every write from here on either way.
ALTER TABLE api_keys
    ADD COLUMN disabled_at timestamptz,
    ADD COLUMN disabled_reason text
        CONSTRAINT api_keys_disabled_reason_check CHECK (disabled_reason IN ('creator_removed', 'creator_left', 'creator_deleted', 'creator_demoted')),
    ADD CONSTRAINT api_keys_disabled_check CHECK ((disabled_at IS NULL) = (disabled_reason IS NULL)) NOT VALID;

-- +goose Down

SET LOCAL lock_timeout = '1s';

-- A release before this one has no notion of a disabled key and would accept it again, although whoever held its plaintext has left. The disabled keys are deleted instead, which their grants and SpiceDB relationships follow through the cascade and the projection triggers.
DELETE FROM api_keys WHERE disabled_at IS NOT NULL;

ALTER TABLE api_keys
    DROP COLUMN disabled_reason,
    DROP COLUMN disabled_at;
