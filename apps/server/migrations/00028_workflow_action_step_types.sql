-- The workflow actions after send_email (decision D-FG07): an HTTP request, Slack, Discord and Microsoft Teams messages, tagging the response and setting a contact attribute. Each step of a run records its action type in workflow_run_logs.step_type, whose check named send_email alone.
--
-- The check is replaced by one that also allows the new types. It is added NOT VALID, so adding it scans nothing, and 00029 validates it in a transaction of its own; it holds for every write from here on either way, and every existing row is a send_email step, which it allows.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. Replacing a constraint takes an ACCESS EXCLUSIVE lock on workflow_run_logs, which every running workflow step writes, and a migration should fail fast on a lock wait rather than queue those writes behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE workflow_run_logs DROP CONSTRAINT workflow_run_logs_step_type_check;
ALTER TABLE workflow_run_logs ADD CONSTRAINT workflow_run_logs_step_type_check
    CHECK (step_type IN ('send_email', 'http_request', 'slack_message', 'discord_message', 'teams_message', 'tag_response', 'set_contact_attribute')) NOT VALID;

-- +goose Down

SET LOCAL lock_timeout = '1s';

-- A release before this one runs only send_email steps; the logs of the other steps go, and their runs keep their status and error.
DELETE FROM workflow_run_logs WHERE step_type <> 'send_email';
ALTER TABLE workflow_run_logs DROP CONSTRAINT workflow_run_logs_step_type_check;
ALTER TABLE workflow_run_logs ADD CONSTRAINT workflow_run_logs_step_type_check CHECK (step_type IN ('send_email')) NOT VALID;
ALTER TABLE workflow_run_logs VALIDATE CONSTRAINT workflow_run_logs_step_type_check;
