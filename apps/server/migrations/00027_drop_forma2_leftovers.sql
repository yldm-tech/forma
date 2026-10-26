-- Drops the last objects this schema kept for the retired Next.js release (forma2): the forma2_imports table (00008, 00009), the record of a `forma import-forma2` run, and workspaces.legacy_environment_id (00001), the forma2 environment id a workspace once answered to.
--
-- Nothing reads or writes either any more. The import, its CLI and the startup guard that read forma2_imports were removed in v0.2.49 together with 00011, which kept the table only so that an image rollback to a release with that guard would still start on a database that had imported forma2, and such a rollback would now cross every release from v0.2.49 on and every migration since. No code path has read legacy_environment_id since v0.2.49 either, when the client and management APIs stopped resolving forma2 environment ids: the server's workspace model, its queries and the API responses never carried it, and the last mention, an optional field of the SPA's workspace type, goes with this change.
--
-- Only this server's own objects are touched: forma2's tables ("User", "Survey", ...) that a self-hosted database may still hold next to this schema are left as they are, and the startup refusal that reads forma2's "User" table (database.RefuseUnreadForma2Data) is unaffected.
--
-- The Down section recreates the table and the column as they were, empty: the import records and the legacy ids are not restored.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. Both statements are catalog changes, but each takes an ACCESS EXCLUSIVE lock: on forma2_imports, which nothing opens any more, and on workspaces, which most requests read. A migration should fail fast on a lock wait rather than queue every later query on workspaces behind it.
SET LOCAL lock_timeout = '1s';

-- No running release reads forma2_imports: the last one that did is v0.2.48.
-- squawk-ignore ban-drop-table
DROP TABLE forma2_imports;

-- No running release reads legacy_environment_id: the server's workspace model has no field for it and no query names it, so a release still running during the rollout neither reads nor writes it. Its UNIQUE constraint goes with it.
-- squawk-ignore ban-drop-column
ALTER TABLE workspaces DROP COLUMN legacy_environment_id;

-- +goose Down

SET LOCAL lock_timeout = '1s';

ALTER TABLE workspaces ADD COLUMN legacy_environment_id text UNIQUE;

CREATE TABLE forma2_imports (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    started_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL DEFAULT now(),
    outcome text NOT NULL CHECK (outcome IN ('imported', 'discarded')),
    legacy_namespace uuid,
    source_counts jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(source_counts) = 'object'),
    target_counts jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(target_counts) = 'object'),
    reason text,
    target_digests jsonb CHECK (jsonb_typeof(target_digests) = 'object'),
    source_marks jsonb CHECK (jsonb_typeof(source_marks) = 'object'),
    CHECK ((outcome = 'imported') = (legacy_namespace IS NOT NULL)),
    CHECK ((outcome = 'discarded') = (reason IS NOT NULL AND reason <> ''))
);
