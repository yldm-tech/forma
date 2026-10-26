-- Read-only results links: a survey's summary published to people without an account, at /app/results?token=<share_key> ("Share results" in the summary's toolbar).
--
-- A link shows the summary under the filter it was created with (filter_criteria, the response module's criteria as JSON), computed from the survey's responses whenever it is opened, so later responses appear while the filter stays as it was. Cards that list individual answers (free text, dates, addresses, contact details, hidden fields and the values typed into an Other option) show only their counts unless the link reveals them: revealed lists those cards as the survey offered them when someone chose to reveal them, each an object {id, kind, type} (kind is answers, otherValues or hiddenField; type is the element's type, absent for a hidden field). A card is revealed only while the survey still offers one with the same id, kind and type, and the server drops entries the survey no longer offers whenever it reads or saves the link: a card removed from the survey and added again later under the same id is withheld until someone reveals it again, unless it came back before anyone read the link. Uploaded files are never shown. Contacts, contact attributes and response ids never leave the server through a link.
--
-- share_key is the link's secret: 256 random bits, base64url without padding (43 characters). It is kept in plain text, unlike session and API key tokens, so the people who manage the link can copy it again: it grants read access to an aggregate of rows this same database holds, so a reader of the table learns nothing the responses table does not already tell them. A link stops working when it expires (expires_at, null for never) or is deleted (revoked); either way it answers like a link that never existed.
--
-- Links belong to their survey (deleting it deletes them) and are authorized on it: whoever may publish the survey lists, creates, changes and revokes them. They carry no relationships of their own, so the table has no authorization projection. created_by is who created the link, cleared when that account is deleted.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The foreign keys take a SHARE ROW EXCLUSIVE lock on surveys and users, and a migration should fail fast on a lock wait rather than queue every later write behind it.
SET LOCAL lock_timeout = '1s';

CREATE TABLE survey_result_shares (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    survey_id uuid NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    share_key text NOT NULL UNIQUE CHECK (share_key ~ '^[A-Za-z0-9_-]{43}$'),
    name text NOT NULL DEFAULT '' CHECK (char_length(name) <= 100),
    filter_criteria jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(filter_criteria) = 'object'),
    revealed jsonb NOT NULL DEFAULT '[]' CHECK (CASE WHEN jsonb_typeof(revealed) = 'array' THEN jsonb_array_length(revealed) <= 500 ELSE false END),
    expires_at timestamptz
);
CREATE INDEX survey_result_shares_survey_id_created_at_idx ON survey_result_shares (survey_id, created_at DESC, id DESC);
CREATE INDEX survey_result_shares_created_by_idx ON survey_result_shares (created_by);

-- +goose Down

SET LOCAL lock_timeout = '1s';

DROP TABLE survey_result_shares;
