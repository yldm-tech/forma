-- Serves the retention sweep of survey impressions (internal/retention sweepDisplays): its count, its preview in the owner's request and every delete batch read a survey's displays older than the window with contact_id IS NULL, as impressions of identified users are kept forever as their targeting history (owner decision D2). On displays_survey_id_created_at_idx those kept rows sit at the old end of every survey's range and are read again from the heap by each batch and each daily count, a cost that grows with the identified history; this index holds only the rows the sweep can delete, so its statements stay proportional to them. The queries are written with this index's predicate, so the planner matches them to it.
--
-- CREATE INDEX CONCURRENTLY keeps reads and writes of displays going while it builds, and cannot run in a transaction. A build that fails leaves an invalid index behind; the DROP first makes a rerun of this migration start clean.
--
-- Outside a transaction SET LOCAL has no effect, so lock_timeout is set for the session (goose runs a migration's statements on one connection) and reset at the end, before goose hands the connection on. The SHARE UPDATE EXCLUSIVE lock the build takes stops neither reads nor writes of displays, only schema changes and vacuum, so 30s lets the build outwait an ordinary long transaction and still fails rather than hanging behind a stuck one.

-- +goose NO TRANSACTION

-- +goose Up

SET lock_timeout = '30s';

DROP INDEX CONCURRENTLY IF EXISTS displays_anonymous_survey_id_created_at_idx;

CREATE INDEX CONCURRENTLY IF NOT EXISTS displays_anonymous_survey_id_created_at_idx ON displays (survey_id, created_at)
    WHERE contact_id IS NULL;

RESET lock_timeout;

-- +goose Down

SET lock_timeout = '30s';

DROP INDEX CONCURRENTLY IF EXISTS displays_anonymous_survey_id_created_at_idx;

RESET lock_timeout;
