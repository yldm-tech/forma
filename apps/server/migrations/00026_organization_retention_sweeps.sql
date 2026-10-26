-- The last retention sweep of each organization: what the daily sweep (internal/retention) counted, or deleted, under the organization's retention windows (organizations.workflow_run_retention_days and display_retention_days), which the owner's Data retention card shows.
--
-- One row per organization, replaced by every sweep that visits it, one that failed partway included; only organizations with at least one window are visited. dry_run says whether the instance only counted (RETENTION_SWEEP_DRY_RUN=1, the default; the *_matched columns hold the counts) or deleted (the *_deleted columns). stopped_early marks a sweep in which a pass failed: its counts are what it got through before the failure, which on an instance that deletes is data already gone. The windows the sweep applied are kept with the counts, so the card shows them only while the organization's windows are still the same. *_capped_units counts the units (a workspace and status for runs, a survey for impressions) that reached the sweep's batch limit and left the rest for the next day.
--
-- The rows carry no relationships of their own, so the table has no authorization projection; they go with their organization.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The foreign key takes a SHARE ROW EXCLUSIVE lock on organizations, which most authorization decisions read, and a migration should fail fast on a lock wait rather than queue every later write behind it.
SET LOCAL lock_timeout = '1s';

CREATE TABLE organization_retention_sweeps (
    organization_id uuid PRIMARY KEY REFERENCES organizations (id) ON DELETE CASCADE,
    swept_at timestamptz NOT NULL,
    dry_run boolean NOT NULL,
    stopped_early boolean NOT NULL DEFAULT false,
    workflow_run_retention_days integer CHECK (workflow_run_retention_days > 0),
    display_retention_days integer CHECK (display_retention_days > 0),
    workflow_runs_matched bigint NOT NULL DEFAULT 0 CHECK (workflow_runs_matched >= 0),
    workflow_runs_deleted bigint NOT NULL DEFAULT 0 CHECK (workflow_runs_deleted >= 0),
    workflow_runs_capped_units bigint NOT NULL DEFAULT 0 CHECK (workflow_runs_capped_units >= 0),
    displays_matched bigint NOT NULL DEFAULT 0 CHECK (displays_matched >= 0),
    displays_deleted bigint NOT NULL DEFAULT 0 CHECK (displays_deleted >= 0),
    displays_capped_units bigint NOT NULL DEFAULT 0 CHECK (displays_capped_units >= 0)
);

-- +goose Down

SET LOCAL lock_timeout = '1s';

DROP TABLE organization_retention_sweeps;
