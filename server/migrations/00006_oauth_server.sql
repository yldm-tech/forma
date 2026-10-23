-- The OAuth 2.1 authorization server MCP clients sign in through (forma2: Better Auth's oauth-provider plugin with the MCP resource), under <issuer>/oauth2/*:
-- - oauth_clients: the clients dynamic client registration created. A confidential client's secret is kept as its SHA-256; public clients (token_endpoint_auth_method none) have none. resources are the resource indicators a client may request tokens for, the MCP resource for every registration.
-- - oauth_signing_keys: the Ed25519 keys access and ID tokens are signed with, published at <issuer>/jwks. The private key is sealed with ENCRYPTION_KEY.
-- - oauth_authorization_requests: an authorization in progress, from /oauth2/authorize until its code is redeemed or it expires. It is bound to the user once they sign in and approve; the code is kept as its hash.
-- - oauth_consents: what a user approved for a client, so authorizing the same scopes and resource again skips the consent page. Revoking it revokes the client's refresh tokens.
-- - oauth_refresh_tokens: kept as their hash and rotated on every use. The tokens issued for one authorization share a family; presenting a token that was already rotated revokes the family.
-- - oauth_access_tokens: the access tokens issued, by their jti, for introspection, userinfo and revocation. /api/mcp verifies the signed token alone, so a token stays usable there until it expires (15 minutes).

-- +goose Up

CREATE TABLE oauth_clients (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    client_id text NOT NULL UNIQUE,
    secret_hash bytea,
    name text NOT NULL DEFAULT '',
    client_uri text,
    logo_uri text,
    redirect_uris text[] NOT NULL CHECK (cardinality(redirect_uris) > 0),
    application_type text NOT NULL CHECK (application_type IN ('web', 'native')),
    token_endpoint_auth_method text NOT NULL CHECK (token_endpoint_auth_method IN ('none', 'client_secret_basic', 'client_secret_post')),
    grant_types text[] NOT NULL,
    response_types text[] NOT NULL,
    scopes text[] NOT NULL,
    resources text[] NOT NULL,
    disabled boolean NOT NULL DEFAULT false,
    CHECK ((token_endpoint_auth_method = 'none') = (secret_hash IS NULL))
);

CREATE TABLE oauth_signing_keys (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    algorithm text NOT NULL CHECK (algorithm = 'EdDSA'),
    public_key bytea NOT NULL,
    private_key bytea NOT NULL
);

CREATE TABLE oauth_authorization_requests (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    client_id text NOT NULL REFERENCES oauth_clients (client_id) ON DELETE CASCADE,
    redirect_uri text NOT NULL,
    scopes text[] NOT NULL,
    resource text NOT NULL,
    state text NOT NULL DEFAULT '',
    nonce text NOT NULL DEFAULT '',
    response_mode text NOT NULL DEFAULT '',
    prompt text[] NOT NULL DEFAULT '{}',
    code_challenge text NOT NULL,
    code_challenge_method text NOT NULL CHECK (code_challenge_method = 'S256'),
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    auth_time timestamptz,
    code_hash bytea UNIQUE,
    CHECK ((user_id IS NULL) = (auth_time IS NULL)),
    CHECK (code_hash IS NULL OR user_id IS NOT NULL)
);
CREATE INDEX oauth_authorization_requests_expires_at_idx ON oauth_authorization_requests (expires_at);

CREATE TABLE oauth_consents (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id text NOT NULL REFERENCES oauth_clients (client_id) ON DELETE CASCADE,
    scopes text[] NOT NULL,
    resources text[] NOT NULL,
    UNIQUE (user_id, client_id)
);

CREATE TABLE oauth_refresh_tokens (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    family_id uuid NOT NULL,
    client_id text NOT NULL REFERENCES oauth_clients (client_id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scopes text[] NOT NULL,
    resources text[] NOT NULL,
    auth_time timestamptz NOT NULL,
    revoked_at timestamptz
);
CREATE INDEX oauth_refresh_tokens_family_id_idx ON oauth_refresh_tokens (family_id);
CREATE INDEX oauth_refresh_tokens_user_id_client_id_idx ON oauth_refresh_tokens (user_id, client_id);
CREATE INDEX oauth_refresh_tokens_expires_at_idx ON oauth_refresh_tokens (expires_at);

CREATE TABLE oauth_access_tokens (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    client_id text NOT NULL REFERENCES oauth_clients (client_id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scopes text[] NOT NULL,
    resources text[] NOT NULL,
    refresh_family_id uuid,
    revoked_at timestamptz
);
CREATE INDEX oauth_access_tokens_expires_at_idx ON oauth_access_tokens (expires_at);
CREATE INDEX oauth_access_tokens_refresh_family_id_idx ON oauth_access_tokens (refresh_family_id);

-- +goose Down

DROP TABLE oauth_access_tokens;
DROP TABLE oauth_refresh_tokens;
DROP TABLE oauth_consents;
DROP TABLE oauth_authorization_requests;
DROP TABLE oauth_signing_keys;
DROP TABLE oauth_clients;
