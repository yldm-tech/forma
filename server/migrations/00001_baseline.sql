-- Baseline schema for the Go rewrite. It carries the business meaning of forma2's 52 Prisma models, redesigned rather than copied.
--
-- Conventions:
-- - snake_case plural tables, timestamptz everywhere.
-- - uuid primary keys defaulting to uuidv7() (PostgreSQL 18+). Rows migrated from forma2 get uuidv5(<forma legacy namespace>, <old id>) instead, so every foreign key can be rewritten without a mapping table, and an old id arriving from outside (a survey link, an SDK snippet) is resolved by the same function.
-- - Closed value sets are text with a CHECK, not PostgreSQL enums, so adding a value is a one-line migration and removing one is possible at all.
-- - Tables whose rows come and go faster than an hour (sessions, verifications) are not migrated from forma2; their shape is free to differ.
--
-- Deliberately not here yet: the OAuth authorization server and SAML tables (P5), and River's queue tables, which River's own migrator owns (P4).

-- +goose Up

-- ---- tenancy ----

CREATE TABLE organizations (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    whitelabel jsonb NOT NULL DEFAULT '{}',
    is_ai_smart_tools_enabled boolean NOT NULL DEFAULT false,
    -- IANA zone for human-facing timestamps (exports, integrations); null means UTC.
    display_time_zone text,
    -- Null keeps history forever and disables that retention pass for the organization.
    workflow_run_retention_days integer CHECK (workflow_run_retention_days > 0),
    display_retention_days integer CHECK (display_retention_days > 0)
);

CREATE TABLE organization_billing (
    organization_id uuid PRIMARY KEY REFERENCES organizations (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    stripe_customer_id text UNIQUE,
    limits jsonb NOT NULL,
    usage_cycle_anchor timestamptz,
    stripe jsonb
);

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    email text NOT NULL,
    email_verified boolean NOT NULL DEFAULT false,
    locale text NOT NULL DEFAULT 'en-US',
    notification_settings jsonb NOT NULL DEFAULT '{}',
    last_login_at timestamptz,
    is_active boolean NOT NULL DEFAULT true
);
-- Sign-in and invites match addresses case-insensitively; two accounts that differ only in case are one person.
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name text NOT NULL,
    -- The environment id that SDK snippets installed before the workspace rename still send; the client API accepts it as an alias.
    legacy_environment_id text UNIQUE,
    styling jsonb NOT NULL DEFAULT '{"allowStyleOverwrite": true}',
    config jsonb NOT NULL DEFAULT '{}',
    recontact_days integer NOT NULL DEFAULT 7 CHECK (recontact_days >= 0),
    link_survey_branding boolean NOT NULL DEFAULT true,
    in_app_survey_branding boolean NOT NULL DEFAULT true,
    placement text NOT NULL DEFAULT 'bottomRight' CHECK (placement IN ('bottomLeft', 'bottomRight', 'topLeft', 'topRight', 'center')),
    click_outside_close boolean NOT NULL DEFAULT true,
    overlay text NOT NULL DEFAULT 'none' CHECK (overlay IN ('none', 'light', 'dark')),
    logo jsonb,
    app_setup_completed boolean NOT NULL DEFAULT false,
    custom_head_scripts text,
    UNIQUE (organization_id, name)
);

CREATE TABLE memberships (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    role text NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'manager', 'member', 'billing')),
    accepted boolean NOT NULL DEFAULT false,
    PRIMARY KEY (user_id, organization_id)
);
CREATE INDEX memberships_organization_id_idx ON memberships (organization_id);

CREATE TABLE teams (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name text NOT NULL,
    UNIQUE (organization_id, name)
);

CREATE TABLE team_members (
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    role text NOT NULL CHECK (role IN ('admin', 'contributor')),
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_members_user_id_idx ON team_members (user_id);

CREATE TABLE workspace_teams (
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    permission text NOT NULL DEFAULT 'read' CHECK (permission IN ('read', 'readWrite', 'manage')),
    PRIMARY KEY (workspace_id, team_id)
);
CREATE INDEX workspace_teams_team_id_idx ON workspace_teams (team_id);

CREATE TABLE invites (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    email text NOT NULL,
    name text,
    creator_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    acceptor_id uuid REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    role text NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'manager', 'member', 'billing'))
);
CREATE INDEX invites_organization_id_email_idx ON invites (organization_id, lower(email));

-- Replaces forma2's `Invite.teamIds text[]`, whose ids no foreign key protected.
CREATE TABLE invite_teams (
    invite_id uuid NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    PRIMARY KEY (invite_id, team_id)
);
CREATE INDEX invite_teams_team_id_idx ON invite_teams (team_id);

CREATE TABLE api_keys (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    label text NOT NULL,
    -- bcrypt of the secret. Keys minted before forma2 added bcrypt hold the plain SHA-256 here instead.
    hashed_key text NOT NULL,
    -- SHA-256 of the secret, the lookup key. For the pre-bcrypt keys above it equals hashed_key, which is how the migration fills it.
    lookup_hash text NOT NULL UNIQUE,
    organization_access jsonb NOT NULL DEFAULT '{}',
    last_used_at timestamptz
);
CREATE INDEX api_keys_organization_id_idx ON api_keys (organization_id);

CREATE TABLE api_key_workspaces (
    api_key_id uuid NOT NULL REFERENCES api_keys (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    permission text NOT NULL CHECK (permission IN ('read', 'write', 'manage')),
    PRIMARY KEY (api_key_id, workspace_id)
);
CREATE INDEX api_key_workspaces_workspace_id_idx ON api_key_workspaces (workspace_id);

-- ---- identity ----

-- One row per way a user can sign in: a password ("credential"), or an external identity (google, github, azuread, openid, saml).
CREATE TABLE user_identities (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('credential', 'google', 'github', 'azuread', 'openid', 'saml')),
    -- Identity namespace for OIDC-style providers; empty for providers with one global namespace.
    issuer text NOT NULL DEFAULT '',
    subject text NOT NULL,
    -- bcrypt, credential identities only. forma2's hashes carry over unchanged.
    password_hash text,
    CHECK ((provider = 'credential') = (password_hash IS NOT NULL)),
    UNIQUE (provider, issuer, subject)
);
CREATE INDEX user_identities_user_id_idx ON user_identities (user_id);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- SHA-256 of the cookie token; the token itself is never stored.
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    ip_address inet,
    user_agent text
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- Single-use tokens: email verification, email change, password reset, SSO recovery. Replaces forma2's Redis-only verification records and its PasswordResetToken table.
CREATE TABLE verifications (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    purpose text NOT NULL CHECK (purpose IN ('email_verification', 'email_change', 'password_reset', 'sso_recovery')),
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    -- The address the token was sent to; signup verification has no user yet.
    email text NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    payload jsonb NOT NULL DEFAULT '{}',
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz
);
CREATE INDEX verifications_user_id_idx ON verifications (user_id);
CREATE INDEX verifications_expires_at_idx ON verifications (expires_at);

CREATE TABLE user_totp (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- AES-GCM ciphertext under the application key, prefixed with the key version.
    secret_ciphertext bytea NOT NULL,
    backup_codes_ciphertext bytea NOT NULL,
    verified boolean NOT NULL DEFAULT false,
    failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
    locked_until timestamptz
);

-- ---- survey definition ----

CREATE TABLE languages (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    code text NOT NULL,
    alias text,
    UNIQUE (workspace_id, code)
);

CREATE TABLE action_classes (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name text NOT NULL,
    description text,
    type text NOT NULL CHECK (type IN ('code', 'noCode')),
    key text,
    no_code_config jsonb,
    UNIQUE (workspace_id, key),
    UNIQUE (workspace_id, name)
);
CREATE INDEX action_classes_workspace_id_created_at_idx ON action_classes (workspace_id, created_at);

CREATE TABLE contact_attribute_keys (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key text NOT NULL,
    name text,
    description text,
    is_unique boolean NOT NULL DEFAULT false,
    type text NOT NULL DEFAULT 'custom' CHECK (type IN ('default', 'custom')),
    data_type text NOT NULL DEFAULT 'string' CHECK (data_type IN ('string', 'number', 'date')),
    UNIQUE (workspace_id, key)
);
CREATE INDEX contact_attribute_keys_workspace_id_created_at_idx ON contact_attribute_keys (workspace_id, created_at);

CREATE TABLE contacts (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE
);
CREATE INDEX contacts_workspace_id_created_at_idx ON contacts (workspace_id, created_at);

-- Exactly one typed value per row, chosen by the key's data_type. forma2 also dual-wrote every value as text; that copy is gone.
CREATE TABLE contact_attributes (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    contact_id uuid NOT NULL REFERENCES contacts (id) ON DELETE CASCADE,
    attribute_key_id uuid NOT NULL REFERENCES contact_attribute_keys (id) ON DELETE CASCADE,
    value_text text,
    value_number double precision,
    value_date timestamptz,
    CHECK (num_nonnulls(value_text, value_number, value_date) = 1),
    UNIQUE (contact_id, attribute_key_id)
);
CREATE INDEX contact_attributes_key_text_idx ON contact_attributes (attribute_key_id, value_text) WHERE value_text IS NOT NULL;
CREATE INDEX contact_attributes_key_number_idx ON contact_attributes (attribute_key_id, value_number) WHERE value_number IS NOT NULL;
CREATE INDEX contact_attributes_key_date_idx ON contact_attributes (attribute_key_id, value_date) WHERE value_date IS NOT NULL;

CREATE TABLE segments (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    title text NOT NULL,
    description text,
    is_private boolean NOT NULL DEFAULT true,
    filters jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(filters) = 'array'),
    UNIQUE (workspace_id, title)
);

CREATE TABLE surveys (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    segment_id uuid REFERENCES segments (id) ON DELETE SET NULL,
    name text NOT NULL,
    type text NOT NULL DEFAULT 'app' CHECK (type IN ('link', 'app')),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'inProgress', 'paused', 'completed')),
    redirect_url text,
    -- The survey document. Validated against the versioned JSON Schema in the service layer; `blocks` replaces forma2's duplicated `questions` + `blocks` pair, and both arrays are plain jsonb arrays instead of jsonb[].
    welcome_card jsonb NOT NULL DEFAULT '{"enabled": false}',
    blocks jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(blocks) = 'array'),
    endings jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(endings) = 'array'),
    hidden_fields jsonb NOT NULL DEFAULT '{"enabled": false}',
    variables jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(variables) = 'array'),
    inline_triggers jsonb,
    survey_closed_message jsonb,
    workspace_overwrites jsonb,
    styling jsonb,
    single_use jsonb DEFAULT '{"enabled": false, "isEncrypted": true}',
    recaptcha jsonb DEFAULT '{"enabled": false, "threshold": 0.1}',
    metadata jsonb NOT NULL DEFAULT '{}',
    display_option text NOT NULL DEFAULT 'displayOnce' CHECK (display_option IN ('displayOnce', 'displayMultiple', 'displaySome', 'respondMultiple')),
    recontact_days integer CHECK (recontact_days >= 0),
    display_limit integer CHECK (display_limit >= 0),
    display_percentage numeric(5, 2) CHECK (display_percentage BETWEEN 0 AND 100),
    auto_close integer,
    auto_complete integer,
    delay integer NOT NULL DEFAULT 0 CHECK (delay >= 0),
    publish_on timestamptz,
    close_on timestamptz,
    archived_at timestamptz,
    is_verify_email_enabled boolean NOT NULL DEFAULT false,
    is_back_button_hidden boolean NOT NULL DEFAULT false,
    is_auto_progressing_enabled boolean NOT NULL DEFAULT false,
    is_capture_ip_enabled boolean NOT NULL DEFAULT false,
    is_anonymize_responses_enabled boolean NOT NULL DEFAULT false,
    show_language_switch boolean,
    pin text,
    -- Global, not per workspace: `/p/{slug}` carries no workspace.
    slug text UNIQUE,
    custom_head_scripts text,
    custom_head_scripts_mode text DEFAULT 'add' CHECK (custom_head_scripts_mode IN ('add', 'replace')),
    -- Target for the composite foreign keys that keep embedded data inside one workspace.
    UNIQUE (id, workspace_id)
);
CREATE INDEX surveys_workspace_id_updated_at_idx ON surveys (workspace_id, updated_at);
CREATE INDEX surveys_segment_id_idx ON surveys (segment_id);
CREATE INDEX surveys_status_publish_on_idx ON surveys (status, publish_on);
CREATE INDEX surveys_status_close_on_idx ON surveys (status, close_on);
CREATE INDEX surveys_archived_at_idx ON surveys (archived_at) WHERE archived_at IS NOT NULL;

CREATE TABLE survey_triggers (
    survey_id uuid NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    action_class_id uuid NOT NULL REFERENCES action_classes (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (survey_id, action_class_id)
);
CREATE INDEX survey_triggers_action_class_id_idx ON survey_triggers (action_class_id);

CREATE TABLE survey_attribute_filters (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    survey_id uuid NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    attribute_key_id uuid NOT NULL REFERENCES contact_attribute_keys (id) ON DELETE CASCADE,
    condition text NOT NULL CHECK (condition IN ('equals', 'notEquals')),
    value text NOT NULL,
    UNIQUE (survey_id, attribute_key_id)
);
CREATE INDEX survey_attribute_filters_attribute_key_id_idx ON survey_attribute_filters (attribute_key_id);

CREATE TABLE survey_quotas (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    survey_id uuid NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    name text NOT NULL,
    "limit" integer NOT NULL CHECK ("limit" > 0),
    logic jsonb NOT NULL DEFAULT '{}',
    action text NOT NULL CHECK (action IN ('endSurvey', 'continueSurvey')),
    -- Id of an entry in surveys.endings, which is JSON and cannot be a foreign key target.
    ending_card_id text,
    count_partial_submissions boolean NOT NULL DEFAULT false,
    UNIQUE (survey_id, name)
);

CREATE TABLE survey_follow_ups (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    survey_id uuid NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    name text NOT NULL,
    trigger jsonb NOT NULL,
    action jsonb NOT NULL
);
CREATE INDEX survey_follow_ups_survey_id_idx ON survey_follow_ups (survey_id);

CREATE TABLE survey_languages (
    survey_id uuid NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    language_id uuid NOT NULL REFERENCES languages (id) ON DELETE CASCADE,
    is_default boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (survey_id, language_id)
);
CREATE INDEX survey_languages_language_id_idx ON survey_languages (language_id);
CREATE UNIQUE INDEX survey_languages_one_default_idx ON survey_languages (survey_id) WHERE is_default;

-- A field is either local to one survey (survey_id set, key null) or part of the workspace library (key set, survey_id null).
CREATE TABLE embedded_data (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    survey_id uuid,
    key text,
    name text NOT NULL,
    description text,
    -- `reserved` fields are a static catalog in code and never stored.
    source text NOT NULL CHECK (source IN ('computed', 'ingested')),
    data_type text NOT NULL DEFAULT 'string' CHECK (data_type IN ('string', 'number', 'boolean', 'date')),
    default_value jsonb,
    locked boolean NOT NULL DEFAULT false,
    CHECK ((survey_id IS NULL) <> (key IS NULL)),
    FOREIGN KEY (survey_id, workspace_id) REFERENCES surveys (id, workspace_id) ON DELETE CASCADE,
    UNIQUE (workspace_id, key),
    UNIQUE (id, workspace_id)
);
CREATE INDEX embedded_data_survey_id_idx ON embedded_data (survey_id);

CREATE TABLE survey_embedded_data (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    workspace_id uuid NOT NULL,
    survey_id uuid NOT NULL,
    embedded_data_id uuid NOT NULL,
    -- The key the value is stored under in responses; migrated fields keep their original key so stored responses, recall tokens and logic keep resolving.
    storage_key text NOT NULL,
    position integer NOT NULL CHECK (position >= 0),
    FOREIGN KEY (survey_id, workspace_id) REFERENCES surveys (id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (embedded_data_id, workspace_id) REFERENCES embedded_data (id, workspace_id) ON DELETE CASCADE,
    UNIQUE (survey_id, embedded_data_id),
    UNIQUE (survey_id, storage_key)
);
CREATE INDEX survey_embedded_data_embedded_data_id_idx ON survey_embedded_data (embedded_data_id);
CREATE INDEX survey_embedded_data_survey_id_position_idx ON survey_embedded_data (survey_id, position);

-- ---- response collection ----

CREATE TABLE displays (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    survey_id uuid NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    contact_id uuid REFERENCES contacts (id) ON DELETE CASCADE
);
CREATE INDEX displays_survey_id_created_at_idx ON displays (survey_id, created_at);
CREATE INDEX displays_contact_id_created_at_idx ON displays (contact_id, created_at);

CREATE TABLE responses (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    survey_id uuid NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    contact_id uuid REFERENCES contacts (id) ON DELETE CASCADE,
    display_id uuid UNIQUE REFERENCES displays (id) ON DELETE SET NULL,
    finished boolean NOT NULL DEFAULT false,
    -- Id of an entry in surveys.endings.
    ending_id text,
    data jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object'),
    variables jsonb NOT NULL DEFAULT '{}',
    ttc jsonb NOT NULL DEFAULT '{}',
    meta jsonb NOT NULL DEFAULT '{}',
    -- Server-computed reasons a value in `data` does not read back as its declared type; null means no ingest wrote it.
    ingest_flags jsonb,
    -- Snapshot of the contact's attributes at submission.
    contact_attributes jsonb,
    single_use_id text,
    language text,
    UNIQUE (survey_id, single_use_id)
);
CREATE INDEX responses_created_at_idx ON responses (created_at);
CREATE INDEX responses_survey_id_created_at_idx ON responses (survey_id, created_at);
CREATE INDEX responses_contact_id_created_at_idx ON responses (contact_id, created_at);
CREATE INDEX responses_survey_id_finished_idx ON responses (survey_id, finished);
-- Answer filters in the responses view and the API are containment queries on `data`.
CREATE INDEX responses_data_idx ON responses USING gin (data jsonb_path_ops);

CREATE TABLE tags (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name text NOT NULL,
    UNIQUE (workspace_id, name)
);

CREATE TABLE response_tags (
    response_id uuid NOT NULL REFERENCES responses (id) ON DELETE CASCADE,
    tag_id uuid NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (response_id, tag_id)
);
CREATE INDEX response_tags_tag_id_idx ON response_tags (tag_id);

CREATE TABLE response_quota_links (
    response_id uuid NOT NULL REFERENCES responses (id) ON DELETE CASCADE,
    quota_id uuid NOT NULL REFERENCES survey_quotas (id) ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('screenedIn', 'screenedOut')),
    PRIMARY KEY (response_id, quota_id)
);
CREATE INDEX response_quota_links_quota_id_status_idx ON response_quota_links (quota_id, status);

-- ---- integrations ----

CREATE TABLE webhooks (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name text,
    url text NOT NULL,
    source text NOT NULL DEFAULT 'user' CHECK (source IN ('user', 'zapier', 'make', 'n8n', 'activepieces')),
    triggers text[] NOT NULL DEFAULT '{}' CHECK (triggers <@ ARRAY['responseCreated', 'responseUpdated', 'responseFinished']),
    -- Standard Webhooks signing secret (whsec_...).
    secret text,
    -- forma2 read an empty `surveyIds` as "every survey", so deleting the last listed survey silently widened a webhook to all of them. The scope is now explicit: when false, only the surveys in webhook_surveys fire it.
    all_surveys boolean NOT NULL DEFAULT true
);
CREATE INDEX webhooks_workspace_id_idx ON webhooks (workspace_id);

CREATE TABLE webhook_surveys (
    webhook_id uuid NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    survey_id uuid NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    PRIMARY KEY (webhook_id, survey_id)
);
CREATE INDEX webhook_surveys_survey_id_idx ON webhook_surveys (survey_id);

CREATE TABLE integrations (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    type text NOT NULL CHECK (type IN ('googleSheets', 'notion', 'airtable', 'slack')),
    -- Mapping configuration only (which survey goes to which sheet, base or channel). Credentials live in integration_credentials.
    config jsonb NOT NULL DEFAULT '{}',
    last_error_at timestamptz,
    last_error_message text,
    consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    CHECK ((last_error_at IS NULL) = (last_error_message IS NULL)),
    UNIQUE (workspace_id, type)
);

CREATE TABLE integration_credentials (
    integration_id uuid PRIMARY KEY REFERENCES integrations (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- AES-GCM ciphertext of the provider's OAuth token set, prefixed with the key version.
    ciphertext bytea NOT NULL
);

-- ---- workflows ----

CREATE TABLE workflows (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    name text NOT NULL,
    description text,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'enabled', 'disabled', 'archived')),
    definition jsonb NOT NULL,
    -- The survey the current published version's trigger targets, so a finished response can narrow candidate workflows in SQL. Null means "every survey", never "none".
    trigger_survey_id uuid,
    UNIQUE (id, workspace_id)
);
CREATE INDEX workflows_workspace_id_status_trigger_survey_id_idx ON workflows (workspace_id, status, trigger_survey_id);
CREATE INDEX workflows_workspace_id_updated_at_idx ON workflows (workspace_id, updated_at);

CREATE TABLE workflow_versions (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    workflow_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    definition jsonb NOT NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    published_by uuid REFERENCES users (id) ON DELETE SET NULL,
    FOREIGN KEY (workflow_id, workspace_id) REFERENCES workflows (id, workspace_id) ON DELETE CASCADE,
    UNIQUE (workflow_id, version)
);
CREATE INDEX workflow_versions_published_by_idx ON workflow_versions (published_by);

CREATE TABLE workflow_runs (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    workflow_id uuid NOT NULL,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    workflow_version_id uuid REFERENCES workflow_versions (id) ON DELETE SET NULL,
    response_id uuid REFERENCES responses (id) ON DELETE CASCADE,
    survey_id uuid,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'completed', 'failed', 'canceled')),
    trigger_type text NOT NULL CHECK (trigger_type IN ('response.completed')),
    is_dry_run boolean NOT NULL DEFAULT false,
    idempotency_key text,
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    next_attempt_at timestamptz,
    last_error_at timestamptz,
    -- The trigger's payload as it was when the run was created; steps read this snapshot, not the live response.
    trigger_payload jsonb NOT NULL,
    data jsonb NOT NULL DEFAULT '{}',
    error text,
    dispatched_at timestamptz,
    started_at timestamptz,
    finished_at timestamptz,
    FOREIGN KEY (workflow_id, workspace_id) REFERENCES workflows (id, workspace_id) ON DELETE CASCADE,
    UNIQUE (workflow_id, idempotency_key)
);
CREATE INDEX workflow_runs_workspace_id_created_at_idx ON workflow_runs (workspace_id, created_at);
CREATE INDEX workflow_runs_workflow_id_created_at_idx ON workflow_runs (workflow_id, created_at);
CREATE INDEX workflow_runs_status_created_at_idx ON workflow_runs (status, created_at);
CREATE INDEX workflow_runs_response_id_idx ON workflow_runs (response_id);
CREATE INDEX workflow_runs_next_attempt_at_idx ON workflow_runs (next_attempt_at) WHERE next_attempt_at IS NOT NULL;

CREATE TABLE workflow_run_logs (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    run_id uuid NOT NULL REFERENCES workflow_runs (id) ON DELETE CASCADE,
    sequence integer NOT NULL CHECK (sequence >= 0),
    step_id text NOT NULL,
    step_type text NOT NULL CHECK (step_type IN ('send_email')),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'skipped')),
    input jsonb NOT NULL DEFAULT '{}',
    output jsonb NOT NULL DEFAULT '{}',
    error text,
    started_at timestamptz,
    finished_at timestamptz,
    UNIQUE (run_id, step_id)
);
CREATE INDEX workflow_run_logs_run_id_sequence_idx ON workflow_run_logs (run_id, sequence);

-- ---- SpiceDB projection outbox ----
--
-- Same mechanism as forma2: triggers on every table that carries authorization data enqueue a row in the same transaction as the change, and the worker delivers it to SpiceDB. Identifiers are operationally sensitive and must never reach logs or metrics.

CREATE TABLE authz_projection_outbox (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    target_type text NOT NULL CHECK (target_type IN ('organization', 'membership', 'user', 'team', 'team_membership', 'workspace', 'workspace_team', 'api_key', 'api_key_workspace')),
    primary_id text NOT NULL,
    secondary_id text,
    is_revocation boolean NOT NULL DEFAULT false,
    -- Total delivery attempts; drives the retry backoff.
    attempts integer NOT NULL DEFAULT 0,
    -- Non-retryable failures attributable to this event alone, the only input to dead-lettering, so a SpiceDB outage cannot dead-letter healthy events.
    permanent_failures integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    leased_at timestamptz,
    lease_expires_at timestamptz,
    lease_owner text,
    processed_at timestamptz,
    dead_lettered_at timestamptz,
    last_attempt_at timestamptz,
    last_error_code text
);
-- The claim: its keys are the claim's ORDER BY, so LIMIT is an ordered index scan with no sort.
CREATE INDEX authz_projection_outbox_claim_idx ON authz_projection_outbox (is_revocation DESC, created_at ASC)
    WHERE processed_at IS NULL AND dead_lettered_at IS NULL;
-- Everything undelivered: the freshness guard, replay, and the status aggregate.
CREATE INDEX authz_projection_outbox_undelivered_idx ON authz_projection_outbox (is_revocation, dead_lettered_at, created_at)
    WHERE processed_at IS NULL;
-- History prune, the only index that carries delivered rows.
CREATE INDEX authz_projection_outbox_processed_idx ON authz_projection_outbox (processed_at)
    WHERE processed_at IS NOT NULL;

-- Does this UPDATE provably leave the projected relationships a superset of what they were? Deny by default: an unmapped target, column or role move is a revocation. Only the fail-closed freshness guard reads the answer.
-- +goose StatementBegin
CREATE FUNCTION authz_projection_is_grant(target_type text, previous_source jsonb, source jsonb) RETURNS boolean AS $$
  SELECT CASE target_type
    -- Deactivation deletes every relationship, so reactivation can only add them back.
    WHEN 'user' THEN
      (previous_source ->> 'is_active') IS DISTINCT FROM 'true'
      AND (source ->> 'is_active') = 'true'
    -- Memberships are projected regardless of `accepted`, so accepting an invite writes identical relationships. A role move deletes the old role's relation.
    WHEN 'membership' THEN
      previous_source ->> 'role' IS NOT DISTINCT FROM source ->> 'role'
    ELSE false
  END;
$$ LANGUAGE sql IMMUTABLE;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION enqueue_authz_projection() RETURNS trigger AS $$
DECLARE
  source jsonb;
  previous_source jsonb;
  is_revocation boolean;
  target_type text := TG_ARGV[0];
  primary_field text := TG_ARGV[1];
  secondary_field text := NULLIF(TG_ARGV[2], '');
BEGIN
  source := CASE WHEN TG_OP = 'DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;

  -- A row can move from one logical pair to another. Reconcile the old pair as a revocation first, or the old edge is no longer discoverable from PostgreSQL and could keep stale access.
  IF TG_OP = 'UPDATE' THEN
    previous_source := to_jsonb(OLD);
    IF previous_source ->> primary_field IS DISTINCT FROM source ->> primary_field
      OR (secondary_field IS NOT NULL AND previous_source ->> secondary_field IS DISTINCT FROM source ->> secondary_field)
    THEN
      INSERT INTO authz_projection_outbox (target_type, primary_id, secondary_id, is_revocation)
      VALUES (
        target_type,
        previous_source ->> primary_field,
        CASE WHEN secondary_field IS NULL THEN NULL ELSE previous_source ->> secondary_field END,
        true
      );
    END IF;
  END IF;

  is_revocation := CASE
    WHEN TG_OP = 'INSERT' THEN false
    WHEN TG_OP = 'DELETE' THEN true
    ELSE NOT authz_projection_is_grant(target_type, previous_source, source)
  END;

  INSERT INTO authz_projection_outbox (target_type, primary_id, secondary_id, is_revocation)
  VALUES (
    target_type,
    source ->> primary_field,
    CASE WHEN secondary_field IS NULL THEN NULL ELSE source ->> secondary_field END,
    is_revocation
  );

  RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER authz_projection_organization
    AFTER INSERT OR DELETE ON organizations
    FOR EACH ROW EXECUTE FUNCTION enqueue_authz_projection('organization', 'id', '');

CREATE TRIGGER authz_projection_membership
    AFTER INSERT OR DELETE OR UPDATE OF role, accepted, organization_id, user_id ON memberships
    FOR EACH ROW EXECUTE FUNCTION enqueue_authz_projection('membership', 'organization_id', 'user_id');

CREATE TRIGGER authz_projection_user
    AFTER INSERT OR DELETE OR UPDATE OF is_active ON users
    FOR EACH ROW EXECUTE FUNCTION enqueue_authz_projection('user', 'id', '');

CREATE TRIGGER authz_projection_team
    AFTER INSERT OR DELETE OR UPDATE OF organization_id ON teams
    FOR EACH ROW EXECUTE FUNCTION enqueue_authz_projection('team', 'id', '');

CREATE TRIGGER authz_projection_team_member
    AFTER INSERT OR DELETE OR UPDATE OF role, team_id, user_id ON team_members
    FOR EACH ROW EXECUTE FUNCTION enqueue_authz_projection('team_membership', 'team_id', 'user_id');

CREATE TRIGGER authz_projection_workspace
    AFTER INSERT OR DELETE OR UPDATE OF organization_id ON workspaces
    FOR EACH ROW EXECUTE FUNCTION enqueue_authz_projection('workspace', 'id', '');

CREATE TRIGGER authz_projection_workspace_team
    AFTER INSERT OR DELETE OR UPDATE OF permission, workspace_id, team_id ON workspace_teams
    FOR EACH ROW EXECUTE FUNCTION enqueue_authz_projection('workspace_team', 'workspace_id', 'team_id');

CREATE TRIGGER authz_projection_api_key
    AFTER INSERT OR DELETE OR UPDATE OF organization_id, organization_access ON api_keys
    FOR EACH ROW EXECUTE FUNCTION enqueue_authz_projection('api_key', 'id', '');

CREATE TRIGGER authz_projection_api_key_workspace
    AFTER INSERT OR DELETE OR UPDATE OF permission, api_key_id, workspace_id ON api_key_workspaces
    FOR EACH ROW EXECUTE FUNCTION enqueue_authz_projection('api_key_workspace', 'api_key_id', 'workspace_id');

-- +goose Down

DROP TABLE authz_projection_outbox, workflow_run_logs, workflow_runs, workflow_versions, workflows,
    integration_credentials, integrations, webhook_surveys, webhooks,
    response_quota_links, response_tags, tags, responses, displays,
    survey_embedded_data, embedded_data, survey_languages, survey_follow_ups, survey_quotas,
    survey_attribute_filters, survey_triggers, surveys, segments,
    contact_attributes, contacts, contact_attribute_keys, action_classes, languages,
    user_totp, verifications, sessions, user_identities,
    api_key_workspaces, api_keys, invite_teams, invites, workspace_teams, team_members, teams,
    memberships, workspaces, users, organization_billing, organizations CASCADE;
DROP FUNCTION enqueue_authz_projection();
DROP FUNCTION authz_projection_is_grant(text, jsonb, jsonb);
