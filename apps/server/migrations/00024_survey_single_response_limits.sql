-- Duplicate-submission limits of a link survey (owner decision D-FG03 of 2026-10-25), both off by default and for every existing survey.
--
-- is_single_response_per_browser_enabled: once a respondent's final answer reaches the server, the survey page remembers it in the browser (localStorage and a cookie under the survey's path) and shows "already responded" on the next visit from that browser. Best effort only: private browsing, cleared site data, another browser or device answer again, and on a shared device the next person cannot answer.
--
-- is_single_response_per_email_enabled: with email verification on (is_verify_email_enabled), one response per verified address, enforced by the server. A verification link whose address already finished a response shows "already responded", and creating or finishing a response for an address that already finished one is refused with 409 (response.Create, response.Update). Without email verification it has no effect.
--
-- Constant defaults make ADD COLUMN a catalog change on PostgreSQL 11 and later: the existing rows are not rewritten.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The ACCESS EXCLUSIVE lock ADD COLUMN needs waits behind any open reader of surveys, and a migration should fail fast rather than queue every later query behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE surveys
    ADD COLUMN is_single_response_per_browser_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN is_single_response_per_email_enabled boolean NOT NULL DEFAULT false;

-- +goose Down

SET LOCAL lock_timeout = '1s';

ALTER TABLE surveys
    DROP COLUMN is_single_response_per_email_enabled,
    DROP COLUMN is_single_response_per_browser_enabled;
