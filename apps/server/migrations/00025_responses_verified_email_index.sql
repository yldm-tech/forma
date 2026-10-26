-- Serves the one-response-per-verified-email check of a survey (owner decision D-FG03, migration 00024): response.claimVerifiedEmail on every response created or finished for such a survey, under an advisory lock, and linksurvey.Repository.verifiedEmailResponded on every load of a verification link. Without it each check reads every response of the survey and detoasts its data; the GIN index on data cannot serve lower(data->>'verifiedEmail'). Both queries are written with this index's expression and predicate, so the planner matches them to it.
--
-- Partial: only responses to a survey with email verification carry verifiedEmail, so the index stays small and the other inserts never touch it.
--
-- CREATE INDEX CONCURRENTLY keeps reads and writes of responses going while it builds, and cannot run in a transaction. A build that fails leaves an invalid index behind; the DROP first makes a rerun of this migration start clean.
--
-- Outside a transaction SET LOCAL has no effect, so lock_timeout is set for the session (goose runs a migration's statements on one connection) and reset at the end, before goose hands the connection on. The SHARE UPDATE EXCLUSIVE lock the build takes, and its waits for transactions older than it, stop neither reads nor writes of responses, only schema changes and vacuum, so the timeout can be longer than the 1s of the transactional migrations: 30s lets the build outwait an ordinary long transaction and still fails rather than hanging behind a stuck one.

-- +goose NO TRANSACTION

-- +goose Up

SET lock_timeout = '30s';

DROP INDEX CONCURRENTLY IF EXISTS responses_survey_id_verified_email_idx;

CREATE INDEX CONCURRENTLY IF NOT EXISTS responses_survey_id_verified_email_idx ON responses (survey_id, lower(data->>'verifiedEmail'))
    WHERE data->>'verifiedEmail' IS NOT NULL;

RESET lock_timeout;

-- +goose Down

SET lock_timeout = '30s';

DROP INDEX CONCURRENTLY IF EXISTS responses_survey_id_verified_email_idx;

RESET lock_timeout;
