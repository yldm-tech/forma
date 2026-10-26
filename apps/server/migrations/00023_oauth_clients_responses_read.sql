-- The responses:read scope (owner decision D-AO11 of 2026-10-25): MCP's list_responses and get_survey_summary tools read survey responses only with it.
--
-- Registration records the scopes a client may request, every scope the authorization server grants at the time, and an authorization request asking for one the client did not register is refused with invalid_scope. The clients registered before this release recorded the list without responses:read, and MCP clients ask for the scopes the 401 challenge names, which now include it: without this update every client connected before the release would fail its next sign-in. Every registered client may request every scope, so each one gets responses:read; a user still approves it on the consent page before a token carries it.
--
-- oauth_clients holds one row per dynamically registered MCP client, a small table; the update takes row locks only.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. A migration should fail fast rather than queue registrations and sign-ins behind it.
SET LOCAL lock_timeout = '1s';

UPDATE oauth_clients SET scopes = array_append(scopes, 'responses:read'), updated_at = now()
    WHERE NOT ('responses:read' = ANY (scopes));

-- +goose Down

SET LOCAL lock_timeout = '1s';

-- A release before this one does not grant responses:read; its clients go back to the scopes it does grant. Consents and tokens that carry the scope keep it, and that release ignores it.
UPDATE oauth_clients SET scopes = array_remove(scopes, 'responses:read'), updated_at = now()
    WHERE 'responses:read' = ANY (scopes);
