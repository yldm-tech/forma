-- Validates workflow_run_logs_step_type_check, which 00028 added NOT VALID: VALIDATE CONSTRAINT takes only a SHARE UPDATE EXCLUSIVE lock, so running workflows go on writing their steps while it scans, and it runs in its own transaction, as an online migration should. Every row passes: before 00028 every step was a send_email step, and the constraint has held for every write since.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE workflow_run_logs VALIDATE CONSTRAINT workflow_run_logs_step_type_check;

-- +goose Down

-- A validated constraint cannot be marked not valid again; 00028's Down replaces it.
