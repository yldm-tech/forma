-- Whether a link survey keeps a respondent's progress in their browser.
--
-- The survey page keeps the answers given so far, the card the respondent is on and the ids of the response and display they belong to in the browser's IndexedDB for 24 hours, so a reload, a tab the phone discarded in the background or a PIN page refreshed after the PIN goes on where the respondent left off, in the same response, instead of starting over with a new partial one. It is on by default, existing surveys included; a creator turns it off for a kiosk or a shared device, where the next person must not see the previous one's answers.
--
-- A constant default makes ADD COLUMN a catalog change on PostgreSQL 11 and later: the existing rows are not rewritten.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The ACCESS EXCLUSIVE lock ADD COLUMN needs waits behind any open reader of surveys, and a migration should fail fast rather than queue every later query behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE surveys ADD COLUMN is_progress_saving_enabled boolean NOT NULL DEFAULT true;

-- +goose Down

SET LOCAL lock_timeout = '1s';

ALTER TABLE surveys DROP COLUMN is_progress_saving_enabled;
