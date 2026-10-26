-- An organization's own survey templates: a survey saved as a template ("Save as template" in the survey list and the editor), offered in the template gallery of every workspace of the organization above the built-in catalog.
--
-- A template is a copy of the survey's document as it was saved, not a link to the survey: editing, archiving or deleting the survey later does not change it. It keeps what the respondent sees (the welcome card, blocks, endings, hidden fields, variables, link metadata, styling, the closed message, and the language switch, back button and auto-progress settings) and the languages it is translated into, as codes with their alias and default and enabled flags, since a workspace language belongs to one workspace. It keeps nothing that belongs to a workspace or to the survey's run: no triggers, segment, follow-ups, quotas, schedule, PIN, slug, single-use links or responses. Its other settings (email verification, progress saving, reCAPTCHA, IP capture, anonymized responses, display options) are not kept: a survey made from it starts with a new survey's defaults for them. The JSON columns are the surveys columns of the same names, copied from a stored (and so already validated) survey.
--
-- Templates belong to the organization (deleting it deletes them) and are authorized on it: owners and managers save, rename and delete them, every member except the billing role (kept out of product data) lists and reads them, and anyone who can create surveys in one of its workspaces uses them there. They carry no relationships of their own, so the table has no authorization projection. created_by is who saved it, kept for the gallery's "Saved by" and cleared when that account is deleted.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The foreign keys take a SHARE ROW EXCLUSIVE lock on organizations and users, and a migration should fail fast on a lock wait rather than queue every later write behind it.
SET LOCAL lock_timeout = '1s';

CREATE TABLE survey_templates (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    welcome_card jsonb NOT NULL,
    blocks jsonb NOT NULL CHECK (jsonb_typeof(blocks) = 'array'),
    endings jsonb NOT NULL CHECK (jsonb_typeof(endings) = 'array'),
    hidden_fields jsonb NOT NULL,
    variables jsonb NOT NULL CHECK (jsonb_typeof(variables) = 'array'),
    metadata jsonb NOT NULL DEFAULT '{}',
    styling jsonb,
    survey_closed_message jsonb,
    show_language_switch boolean,
    is_back_button_hidden boolean NOT NULL DEFAULT false,
    is_auto_progressing_enabled boolean NOT NULL DEFAULT false,
    languages jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(languages) = 'array')
);
CREATE INDEX survey_templates_organization_id_created_at_idx ON survey_templates (organization_id, created_at DESC, id DESC);
CREATE INDEX survey_templates_created_by_idx ON survey_templates (created_by);

-- +goose Down

SET LOCAL lock_timeout = '1s';

DROP TABLE survey_templates;
