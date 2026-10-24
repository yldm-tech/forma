-- The record that forma2's data has been dealt with, for a database that holds forma2's tables next to these.
--
-- `forma import-forma2` copies forma2's rows into this schema in one transaction and writes a row here, outcome 'imported', in that same transaction, so the row exists exactly when a complete import has been committed. `forma import-forma2 --discard-forma2-data --reason=...` writes outcome 'discarded' instead, the deliberate and recorded decision to start empty on such a database.
--
-- The server refuses to start while forma2's "User" table holds rows and this table holds none, because a server started on an empty schema would make its first sign-up the instance's owner. docker/forma.sh checks the same predicate before it starts the stack.

-- +goose Up

CREATE TABLE forma2_imports (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    started_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL DEFAULT now(),
    outcome text NOT NULL CHECK (outcome IN ('imported', 'discarded')),
    -- The legacyid namespace the imported ids were derived with; a different value would mean every migrated id is orphaned.
    legacy_namespace uuid,
    -- Rows per forma2 table as the import read them, and what it wrote per table of this schema.
    source_counts jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(source_counts) = 'object'),
    target_counts jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(target_counts) = 'object'),
    reason text,
    CHECK ((outcome = 'imported') = (legacy_namespace IS NOT NULL)),
    CHECK ((outcome = 'discarded') = (reason IS NOT NULL AND reason <> ''))
);

-- +goose Down

DROP TABLE forma2_imports;
