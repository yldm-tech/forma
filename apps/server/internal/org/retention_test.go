package org

import (
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/retention"
)

func windows(runs, displays any) map[string]any {
	return map[string]any{"workflowRunRetentionDays": runs, "displayRetentionDays": displays}
}

// TestRetention: owners read and set the organization's retention windows; the card says whether the instance's sweep only counts, when it runs next, and what its last run counted, but only while the windows are the ones that run applied.
func TestRetention(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RetentionSweepDryRun = true })
	owner, manager, member := e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, manager, RoleManager)
	e.member(c.ID, member, RoleMember)
	path := orgPath(c.ID, "/retention")

	var r Retention
	e.ok(e.do(owner, http.MethodGet, path, nil), http.StatusOK, &r)
	if r.WorkflowRunRetentionDays != nil || r.DisplayRetentionDays != nil || !r.DryRun || r.LastSweep != nil {
		t.Fatalf("a new organization's retention = %+v, want every window off, a dry run and no sweep", r)
	}
	if next := r.NextSweepAt.UTC(); !next.After(time.Now()) || time.Until(next) > 24*time.Hour || next.Hour() != 0 || next.Minute() != 0 {
		t.Fatalf("next sweep at %v, want the coming midnight UTC", r.NextSweepAt)
	}

	// Owners only: the windows decide what is deleted for good.
	e.fails(e.do(manager, http.MethodGet, path, nil), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(member, http.MethodGet, path, nil), http.StatusForbidden, apperr.CodeForbidden, "")
	e.fails(e.do(manager, http.MethodPut, path, windows(30, nil)), http.StatusForbidden, apperr.CodeForbidden, "")

	for _, body := range []map[string]any{
		{"workflowRunRetentionDays": 30},
		{"displayRetentionDays": 30},
		windows(0, nil),
		windows(nil, -1),
		windows(MaxRetentionDays+1, nil),
		windows("30", nil),
		windows(1.5, nil),
	} {
		e.fails(e.do(owner, http.MethodPut, path, body), http.StatusBadRequest, apperr.CodeValidation, "")
	}
	if e.count(`SELECT count(*) FROM organizations WHERE id = ? AND (workflow_run_retention_days IS NOT NULL OR display_retention_days IS NOT NULL)`, c.ID) != 0 {
		t.Fatal("a refused request set a window")
	}

	e.ok(e.do(owner, http.MethodPut, path, windows(30, 90)), http.StatusOK, &r)
	if r.WorkflowRunRetentionDays == nil || *r.WorkflowRunRetentionDays != 30 || r.DisplayRetentionDays == nil || *r.DisplayRetentionDays != 90 || r.LastSweep != nil {
		t.Fatalf("after setting 30 and 90 days: %+v", r)
	}
	if e.count(`SELECT count(*) FROM organizations WHERE id = ? AND workflow_run_retention_days = 30 AND display_retention_days = 90`, c.ID) != 1 {
		t.Fatal("the windows were not stored")
	}
	events := e.auditEvents()
	last := events[len(events)-1]
	if last["action"] != "updated" || last["status"] != "success" || last["target"].(map[string]any)["type"] != "organization" || last["changes"] == nil {
		t.Fatalf("audit event %v, want the organization's update with its changes", last)
	}

	// A sweep with these windows is what the card reports.
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Old') RETURNING id`, c.ID)
	flow := e.insert(`INSERT INTO workflows (workspace_id, name, definition) VALUES (?, 'Flow', '{}') RETURNING id`, ws)
	e.exec(`INSERT INTO workflow_runs (workflow_id, workspace_id, status, trigger_type, trigger_payload, created_at) VALUES (?, ?, 'completed', 'response.completed', '{}', now() - interval '31 days')`, flow, ws)
	if _, err := retention.NewSweeper(e.db, slog.New(slog.DiscardHandler)).Sweep(t.Context(), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	e.ok(e.do(owner, http.MethodGet, path, nil), http.StatusOK, &r)
	if r.LastSweep == nil || !r.LastSweep.DryRun || r.LastSweep.WorkflowRuns.Matched != 1 || r.LastSweep.WorkflowRuns.Deleted != 0 || r.LastSweep.Displays.Matched != 0 {
		t.Fatalf("last sweep %+v, want a dry run that matched the one old run", r.LastSweep)
	}

	// Other windows make that report meaningless, and keeping everything forever leaves nothing to report.
	e.ok(e.do(owner, http.MethodPut, path, windows(60, 90)), http.StatusOK, &r)
	if r.LastSweep != nil {
		t.Fatalf("last sweep %+v under other windows, want none", r.LastSweep)
	}
	e.ok(e.do(owner, http.MethodPut, path, windows(nil, nil)), http.StatusOK, &r)
	if r.WorkflowRunRetentionDays != nil || r.DisplayRetentionDays != nil || r.LastSweep != nil {
		t.Fatalf("after turning every window off: %+v", r)
	}
	if e.count(`SELECT count(*) FROM workflow_runs WHERE workspace_id = ?`, ws) != 1 {
		t.Fatal("a dry run deleted the run")
	}
}

// The card tells an owner when the instance's sweep deletes.
func TestRetentionReportsDeletingInstance(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	c := e.organization(owner, "Acme")
	var r Retention
	e.ok(e.do(owner, http.MethodGet, orgPath(c.ID, "/retention"), nil), http.StatusOK, &r)
	if r.DryRun {
		t.Fatal("dryRun = true on an instance with RETENTION_SWEEP_DRY_RUN=0")
	}
}
