package workflow

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics/analyticstest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
)

func insertID(t *testing.T, db *gorm.DB, query string, args ...any) string {
	t.Helper()
	var id string
	if err := db.Raw(query+" RETURNING id::text", args...).Row().Scan(&id); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return id
}

const emailFlow = `{"trigger":{"type":"trigger","triggerType":"response.completed"},"nodes":[{"type":"action","actionType":"send_email"},{"type":"action","actionType":"send_email"},{"type":"if_else"}]}`

// TestUsageSnapshotEmitsForma2sEvents runs the snapshot over two organizations on Forma Cloud: one with live workflows, a run history and a Pro subscription, and one that archived everything and is left out.
func TestUsageSnapshotEmitsForma2sEvents(t *testing.T) {
	db := pgtest.GORM(t)
	now := time.Now()
	org := insertID(t, db, `INSERT INTO organizations (name, created_at) VALUES ('Acme', '2025-01-02T03:04:05Z')`)
	db.Exec(`INSERT INTO organization_billing (organization_id, limits, stripe) VALUES (?, '{"workspaces":null,"monthly":{"responses":5000,"workflowRuns":1000}}', '{"plan":"pro","interval":"monthly","subscriptionStatus":"active","features":["workflows"]}')`, org)
	ws1 := insertID(t, db, `INSERT INTO workspaces (organization_id, name) VALUES (?, 'Website')`, org)
	ws2 := insertID(t, db, `INSERT INTO workspaces (organization_id, name) VALUES (?, 'App')`, org)
	enabled := insertID(t, db, `INSERT INTO workflows (workspace_id, name, status, definition) VALUES (?, 'A', 'enabled', ?)`, ws1, emailFlow)
	insertID(t, db, `INSERT INTO workflows (workspace_id, name, status, definition) VALUES (?, 'B', 'draft', '{"trigger":null,"nodes":[{"type":"action","actionType":"send_email"}]}')`, ws1)
	insertID(t, db, `INSERT INTO workflows (workspace_id, name, status, definition) VALUES (?, 'C', 'archived', ?)`, ws2, emailFlow)
	for _, run := range []struct {
		status   string
		finished time.Time
		dryRun   bool
	}{
		{"completed", now.Add(-time.Hour), false},
		{"failed", now.Add(-2 * time.Hour), false},
		{"canceled", now.Add(-3 * time.Hour), false},
		{"completed", now.Add(-25 * time.Hour), false},
		{"completed", now.Add(-time.Hour), true},
	} {
		insertID(t, db, `INSERT INTO workflow_runs (workflow_id, workspace_id, status, trigger_type, trigger_payload, finished_at, is_dry_run) VALUES (?, ?, ?, 'response.completed', '{}', ?, ?)`, enabled, ws1, run.status, run.finished, run.dryRun)
	}
	archivedOnly := insertID(t, db, `INSERT INTO organizations (name) VALUES ('Gone')`)
	archivedWS := insertID(t, db, `INSERT INTO workspaces (organization_id, name) VALUES (?, 'Old')`, archivedOnly)
	insertID(t, db, `INSERT INTO workflows (workspace_id, name, status, definition) VALUES (?, 'Old', 'archived', ?)`, archivedWS, emailFlow)

	rec := analyticstest.Start(t)
	summary, err := NewUsageSnapshot(config.Config{IsFormaCloud: true}, db, slog.New(slog.DiscardHandler)).Emit(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	if summary != (SnapshotSummary{Organizations: 1, Workspaces: 2, Events: 4}) {
		t.Fatalf("summary = %+v, want one organization, two workspaces, four events", summary)
	}

	usage := rec.Events(t, "workflow_usage_snapshot", 1)[0]
	want := map[string]any{
		"organization_id": org, "plan": "pro", "has_workflows_entitlement": true,
		"workflows_total": float64(3), "workflows_draft": float64(1), "workflows_enabled": float64(1), "workflows_disabled": float64(0), "workflows_archived": float64(1),
		"workspaces_with_workflows": float64(2), "runs_24h_total": float64(3), "runs_24h_completed": float64(1), "runs_24h_failed": float64(1),
	}
	if usage.DistinctID != org {
		t.Errorf("distinct id = %s, want the organization", usage.DistinctID)
	}
	for k, v := range want {
		if usage.Properties[k] != v {
			t.Errorf("workflow_usage_snapshot %s = %v, want %v", k, usage.Properties[k], v)
		}
	}
	nodeTypes := map[string]map[string]any{}
	for _, e := range rec.Events(t, "workflow_node_type_snapshot", 3) {
		nodeTypes[e.Properties["node_type"].(string)] = e.Properties
	}
	for nodeType, w := range map[string][3]any{
		"response.completed": {"trigger", float64(1), float64(1)},
		"send_email":         {"action", float64(2), float64(1)},
		"if_else":            {"action", float64(1), float64(1)},
	} {
		p := nodeTypes[nodeType]
		if p["node_kind"] != w[0] || p["workflows_total"] != w[1] || p["workflows_enabled"] != w[2] || p["plan"] != "pro" {
			t.Errorf("%s = %v, want kind %v, %v workflows, %v enabled", nodeType, p, w[0], w[1], w[2])
		}
	}

	groups := rec.Events(t, "$groupidentify", 3)
	var organization map[string]any
	workspaces := map[string]map[string]any{}
	for _, g := range groups {
		set, _ := g.Properties["$group_set"].(map[string]any)
		switch g.Properties["$group_type"] {
		case "organization":
			organization = set
		case "workspace":
			workspaces[g.Properties["$group_key"].(string)] = set
		}
	}
	for k, v := range map[string]any{
		"plan": "pro", "billing_interval": "monthly", "subscription_status": "active", "deployment": "cloud", "license_status": "active",
		"has_workflows_entitlement": true, "monthly_workflow_runs_limit": float64(1000), "workflows_total": float64(3), "workflows_enabled": float64(1),
		"organization_created_at": "2025-01-02T03:04:05.000Z",
	} {
		if organization[k] != v {
			t.Errorf("organization group %s = %v, want %v", k, organization[k], v)
		}
	}
	if workspaces[ws1]["workflows_total"] != float64(2) || workspaces[ws1]["workflows_enabled"] != float64(1) || workspaces[ws2]["workflows_total"] != float64(1) {
		t.Errorf("workspace groups = %v", workspaces)
	}
}

// TestUsageSnapshotSelfHosted: self-hosted organizations report the enterprise tier their license grants, and a group property the pass could not resolve is left out rather than sent empty.
func TestUsageSnapshotSelfHosted(t *testing.T) {
	db := pgtest.GORM(t)
	org := insertID(t, db, `INSERT INTO organizations (name) VALUES ('Acme')`)
	ws := insertID(t, db, `INSERT INTO workspaces (organization_id, name) VALUES (?, 'Website')`, org)
	insertID(t, db, `INSERT INTO workflows (workspace_id, name, status, definition) VALUES (?, 'A', 'disabled', ?)`, ws, emailFlow)

	rec := analyticstest.Start(t)
	if _, err := NewUsageSnapshot(config.Config{}, db, slog.New(slog.DiscardHandler)).Emit(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	usage := rec.Events(t, "workflow_usage_snapshot", 1)[0]
	if usage.Properties["plan"] != "self_hosted_enterprise" || usage.Properties["has_workflows_entitlement"] != true || usage.Properties["workflows_disabled"] != float64(1) {
		t.Errorf("snapshot = %v", usage.Properties)
	}
	var set map[string]any
	for _, g := range rec.Events(t, "$groupidentify", 2) {
		if g.Properties["$group_type"] == "organization" {
			set, _ = g.Properties["$group_set"].(map[string]any)
		}
	}
	if set["deployment"] != "self_hosted" || set["plan"] != "self_hosted_enterprise" {
		t.Errorf("organization group = %v", set)
	}
	for _, unresolved := range []string{"billing_interval", "subscription_status", "monthly_workflow_runs_limit"} {
		if _, ok := set[unresolved]; ok {
			t.Errorf("organization group carries the unresolved %s", unresolved)
		}
	}
}

func TestUsageSnapshotJobSkipsWithoutPostHog(t *testing.T) {
	db := pgtest.GORM(t)
	w := &usageSnapshotWorker{snapshot: NewUsageSnapshot(config.Config{}, db, slog.New(slog.DiscardHandler)), log: slog.New(slog.DiscardHandler)}
	// The database is closed: the pass would fail if it ran.
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	if err := w.Work(context.Background(), &river.Job[UsageSnapshotArgs]{JobRow: &rivertype.JobRow{Kind: UsageSnapshotArgs{}.Kind()}}); err != nil {
		t.Fatalf("the snapshot ran without PostHog: %v", err)
	}
}
