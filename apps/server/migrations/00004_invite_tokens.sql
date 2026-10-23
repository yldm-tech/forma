-- Invite links and one pending invite per address.
--
-- forma2 signed a JWT per invite link that stayed valid for its own seven days whatever happened to the invite later, so resending an invite never revoked the links sent before. Here a link is an opaque token stored only as its SHA-256 beside the invite it opens, with its own expiry: a resend replaces every outstanding link, accepting or deleting the invite removes them all.
--
-- forma2 compared invite addresses case-sensitively and had no unique constraint, so A@x.com and a@x.com could hold two invites to the same person. Addresses are now stored lowercased and an organization holds at most one invite per address, which also closes the race between two concurrent invites.

-- +goose Up

CREATE TABLE invite_tokens (
    token_hash bytea PRIMARY KEY,
    invite_id uuid NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX invite_tokens_invite_id_idx ON invite_tokens (invite_id);

DROP INDEX invites_organization_id_email_idx;
CREATE UNIQUE INDEX invites_organization_id_email_key ON invites (organization_id, lower(email));

-- +goose Down

DROP INDEX invites_organization_id_email_key;
CREATE INDEX invites_organization_id_email_idx ON invites (organization_id, lower(email));
DROP TABLE invite_tokens;
