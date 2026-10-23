-- Retention windows for the two tables that grow forever.
--
-- Nothing in this product prunes anything except the AuthZed outbox. "WorkflowRun" stores response answers a second time — every completed response times every matched enabled workflow writes a `triggerPayload` holding the full `response.data`, plus a "WorkflowRunLog" row per step with its own input and output JSON — and "Display" gains a row per survey impression. Neither is ever deleted except by cascade. These two columns are what turns "we keep impression data for 90 days" into configuration instead of a fork of the sweep job.
--
-- **Null means keep forever, and disables that pass for the organization.** That is the value every existing row gets here, and it is the point of leaving the DEFAULT off: an additive migration must never arm an irreversible delete. Deleting product data is the sharpest thing in this area, no test in this repo would catch a window that is too short, and an operator who has not asked for retention has not consented to it. Turning it on is a deliberate write to these columns; a new organization starts at null like every existing one, and the long default the product suggests belongs in the settings surface that writes the value, not in the catalog.
--
-- No index is built here on purpose, and the sweep is expected to be written so that none is needed. "WorkflowRun" already has ("status", "created_at"), which serves one pass per terminal status keyed on age — `status = 'completed' AND "created_at" < cutoff` and the same for 'failed' and 'canceled' — as an ordered index scan that never touches a live row. "Display" already has ("surveyId", "created_at"), which serves a per-survey pass over impressions older than the cutoff. Both alternatives to that shape would mean building a fresh index on one of the largest tables an install has, which under this repo's migration convention is a plain CREATE INDEX holding SHARE on that table until it finishes — minutes of blocked display writes on a real instance, paid by every self-hoster, to support a feature that is off until someone sets a column here. A blocking build on the busiest write path is a bad trade for an opt-in sweep, so the sweep is asked to use the index paths that already exist.
--
-- Safety: one ALTER TABLE adding two nullable INTEGER columns with no default. That is a catalog-only change, no table rewrite on any PostgreSQL version, and the ACCESS EXCLUSIVE lock on "Organization" is held for the catalog update alone. "Organization" is one row per customer. Nothing is read, nothing is deleted, and no behaviour changes until an operator writes a value.

BEGIN;

SET LOCAL lock_timeout = '5s';

-- AlterTable
ALTER TABLE "Organization"
  ADD COLUMN "workflowRunRetentionDays" INTEGER,
  ADD COLUMN "displayRetentionDays" INTEGER;

COMMIT;
