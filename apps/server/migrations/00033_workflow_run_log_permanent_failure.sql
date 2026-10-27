-- Whether a failed workflow step failed in a way no retry can change (a refused URL, a 4xx answer, a tag that does not exist). A step set to continue on failure lets its run go on past it; when another such step failed transiently, the run is retried, and the retry resumes a step that failed for good as failed instead of performing it again, so an endpoint that answered 4xx after acting on the request is not sent it once per attempt.
--
-- Existing rows start false, which is how every failed step was treated before: a permanent failure without continue on failure fails its run at once, so no retry ever reached it.
--
-- Adding a column with a constant default is a catalog change: no existing row is rewritten.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The ACCESS EXCLUSIVE lock ADD COLUMN needs waits behind any open reader of workflow_run_logs, which every running workflow step writes, and a migration should fail fast rather than queue those writes behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE workflow_run_logs ADD COLUMN permanent_failure boolean NOT NULL DEFAULT false;

-- +goose Down

SET LOCAL lock_timeout = '1s';

-- A release before this one does not read the column; dropping it only means a retried run performs a step that failed for good once more.
-- squawk-ignore ban-drop-column
ALTER TABLE workflow_run_logs DROP COLUMN permanent_failure;
