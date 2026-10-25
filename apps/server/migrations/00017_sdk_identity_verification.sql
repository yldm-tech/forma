-- How a workspace's SDK identify (POST /api/v1|v2/client/{id}/user) may change a contact.
--
-- The workspace id in an SDK snippet is public, so before this anyone could identify as any userId and rewrite that contact's attributes, email included. Now the client API never writes a protected attribute (sdk_protected_attribute_keys, chosen by the workspace's managers; userId and email are handled separately and cannot be listed), and writes email only on a verified identify: one that carries a userHash, the HMAC-SHA256 of the userId under the workspace's identity secret, which the customer's own server computes.
--
-- sdk_identity_secret is that secret, sealed with ENCRYPTION_KEY (secret.Cipher, bound to the workspace), null until a manager generates one; it is shown once, when it is generated. sdk_identity_required makes a verified identify mandatory: an identify without a valid userHash is refused, and the v1 display and response routes stop attaching a userId that comes without one. The server refuses to require verification while there is no secret.
--
-- Constant defaults make ADD COLUMN a catalog change on PostgreSQL 11 and later: the existing rows are not rewritten, and every existing workspace keeps identifying as before, except for the email and protected attribute rules, which the server applies to all of them.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The ACCESS EXCLUSIVE lock ADD COLUMN needs waits behind any open reader of workspaces (every client API request reads it), and a migration should fail fast rather than queue every later query behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE workspaces
    ADD COLUMN sdk_identity_secret bytea,
    ADD COLUMN sdk_identity_required boolean NOT NULL DEFAULT false,
    ADD COLUMN sdk_protected_attribute_keys text[] NOT NULL DEFAULT '{}';

-- +goose Down

SET LOCAL lock_timeout = '1s';

ALTER TABLE workspaces
    DROP COLUMN sdk_protected_attribute_keys,
    DROP COLUMN sdk_identity_required,
    DROP COLUMN sdk_identity_secret;
