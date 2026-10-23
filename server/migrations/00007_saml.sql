-- SAML single sign-on (forma2: BoxyHQ SAML Jackson in a separate SAML_DATABASE_URL database):
-- - saml_connections: the identity provider a tenant and product sign in through, as Jackson keyed its connections. forma2 had one instance-wide connection (tenant 'forma.ylam.ai', product 'forma'), rebuilt at every start from the first saml-connection/*.xml file, so a later file replaces it; the unique pair keeps that at one row. idp_metadata is the metadata XML as uploaded, the only source of the IdP's endpoints and signing certificates, so a connection exported from Jackson imports as its tenant, product, name and raw metadata.
-- - saml_request: an AuthnRequest in flight, keyed by the hash of its RelayState; its payload holds the request ID the response must answer. The assertion consumer service consumes it, so a response is accepted once.
-- - saml_assertion: an assertion the consumer service accepted, keyed by the hash of a one-time code it hands the browser; the callback that checks the browser's state cookie redeems it.

-- +goose Up

CREATE TABLE saml_connections (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant text NOT NULL,
    product text NOT NULL,
    name text NOT NULL DEFAULT '',
    idp_entity_id text NOT NULL CHECK (idp_entity_id <> ''),
    idp_metadata text NOT NULL,
    UNIQUE (tenant, product)
);

ALTER TABLE verifications DROP CONSTRAINT verifications_purpose_check;
ALTER TABLE verifications ADD CONSTRAINT verifications_purpose_check
    CHECK (purpose IN ('email_verification', 'email_change', 'password_reset', 'sso_recovery', 'two_factor', 'signup_intent', 'account_deletion', 'oauth_state', 'saml_request', 'saml_assertion'));

-- +goose Down

DELETE FROM verifications WHERE purpose IN ('saml_request', 'saml_assertion');
ALTER TABLE verifications DROP CONSTRAINT verifications_purpose_check;
ALTER TABLE verifications ADD CONSTRAINT verifications_purpose_check
    CHECK (purpose IN ('email_verification', 'email_change', 'password_reset', 'sso_recovery', 'two_factor', 'signup_intent', 'account_deletion', 'oauth_state'));

DROP TABLE saml_connections;
