package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/billing"
	"github.com/yldm-tech/forma/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// forma2's snapshot events and window (apps/web/modules/workflows/lib/analytics).
const (
	usageSnapshotEvent    = "workflow_usage_snapshot"
	nodeTypeSnapshotEvent = "workflow_node_type_snapshot"
	snapshotRunWindow     = 24 * time.Hour
	definitionPageSize    = 500
	// selfHostedPlan is the plan a self-hosted organization reports: its license grants every feature.
	selfHostedPlan = "self_hosted_enterprise"
	// licenseStatus is the license every instance runs under since the license check grants everything.
	licenseStatus = "active"
)

// statusCounts are workflows by status.
type statusCounts struct{ draft, enabled, disabled, archived int64 }

func (s *statusCounts) add(status string, n int64) bool {
	switch status {
	case StatusDraft:
		s.draft += n
	case StatusEnabled:
		s.enabled += n
	case StatusDisabled:
		s.disabled += n
	case StatusArchived:
		s.archived += n
	default:
		return false
	}
	return true
}

func (s statusCounts) total() int64 { return s.draft + s.enabled + s.disabled + s.archived }
func (s statusCounts) live() int64  { return s.draft + s.enabled + s.disabled }

type nodeTypeUsage struct {
	kind           string
	total, enabled int64
}

// organizationContext is the organization's plan facts: Stripe's snapshot on Forma Cloud, the license tier self-hosted.
type organizationContext struct {
	createdAt                time.Time
	plan                     *string
	billingInterval          *string
	subscriptionStatus       *string
	monthlyWorkflowRunsLimit *int64
	hasWorkflowsEntitlement  bool
}

// organizationUsage is one organization's workflow usage, forma2's OrganizationWorkflowUsage.
type organizationUsage struct {
	organizationID uuid.UUID
	context        organizationContext
	statuses       statusCounts
	// workspaces are those with at least one workflow, archived included.
	workspaces map[uuid.UUID]*statusCounts
	nodeTypes  map[string]*nodeTypeUsage
	// runs settled in the window, so completed + failed <= total.
	runsTotal, runsCompleted, runsFailed int64
}

// UsageSnapshot is forma2's daily workflow usage snapshot for PostHog (ENG-2851): what each organization's workflows are, by status and node type, and how its last day of runs settled. Lifecycle events say what changed; this says what is.
type UsageSnapshot struct {
	cfg config.Config
	db  *gorm.DB
	log *slog.Logger
}

func NewUsageSnapshot(cfg config.Config, db *gorm.DB, log *slog.Logger) *UsageSnapshot {
	return &UsageSnapshot{cfg: cfg, db: db, log: log}
}

// SnapshotSummary is what one snapshot emitted.
type SnapshotSummary struct {
	Organizations int `json:"organizations"`
	Workspaces    int `json:"workspaces"`
	Events        int `json:"events"`
}

type countRow struct {
	WorkspaceID uuid.UUID
	Status      string
	N           int64
}

type workspaceRow struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	CreatedAt      time.Time
	Limits         []byte
	Stripe         []byte
}

type definitionRow struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Status      string
	Definition  json.RawMessage
}

// collect is one read pass over the workflow tables, aggregated per organization. Only organizations with a live (non-archived) workflow come back: one that archived everything no longer uses workflows.
func (s *UsageSnapshot) collect(ctx context.Context, now time.Time) ([]*organizationUsage, error) {
	db := s.db.WithContext(ctx)
	var statusRows []countRow
	if err := db.Raw(`SELECT workspace_id, status, count(*) AS n FROM workflows GROUP BY workspace_id, status`).Scan(&statusRows).Error; err != nil {
		return nil, fmt.Errorf("count workflows: %w", err)
	}
	if len(statusRows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(statusRows))
	for _, r := range statusRows {
		if !slices.Contains(ids, r.WorkspaceID) {
			ids = append(ids, r.WorkspaceID)
		}
	}
	var workspaces []workspaceRow
	err := db.Raw(`SELECT w.id, w.organization_id, o.created_at, b.limits, b.stripe FROM workspaces w
		JOIN organizations o ON o.id = w.organization_id LEFT JOIN organization_billing b ON b.organization_id = o.id
		WHERE w.id = ANY(?::uuid[])`, uuidArray(ids)).Scan(&workspaces).Error
	if err != nil {
		return nil, fmt.Errorf("read workflow workspaces: %w", err)
	}
	// Runs settled in the window, not created in it: a run's status changes, and every terminal write sets finished_at, so each run counts once, in the window it settled in.
	var runRows []countRow
	err = db.Raw(`SELECT workspace_id, status, count(*) AS n FROM workflow_runs
		WHERE workspace_id = ANY(?::uuid[]) AND NOT is_dry_run AND finished_at >= ? GROUP BY workspace_id, status`, uuidArray(ids), now.Add(-snapshotRunWindow)).Scan(&runRows).Error
	if err != nil {
		return nil, fmt.Errorf("count workflow runs: %w", err)
	}

	workspaceByID := map[uuid.UUID]workspaceRow{}
	for _, w := range workspaces {
		workspaceByID[w.ID] = w
	}
	byOrganization := map[uuid.UUID]*organizationUsage{}
	var order []uuid.UUID
	forWorkspace := func(id uuid.UUID) *organizationUsage {
		w, ok := workspaceByID[id]
		if !ok {
			// Deleted between the reads.
			return nil
		}
		u := byOrganization[w.OrganizationID]
		if u == nil {
			u = &organizationUsage{organizationID: w.OrganizationID, context: s.organizationContext(w), workspaces: map[uuid.UUID]*statusCounts{}, nodeTypes: map[string]*nodeTypeUsage{}}
			byOrganization[w.OrganizationID] = u
			order = append(order, w.OrganizationID)
		}
		return u
	}
	for _, r := range statusRows {
		u := forWorkspace(r.WorkspaceID)
		if u == nil || !u.statuses.add(r.Status, r.N) {
			continue
		}
		if u.workspaces[r.WorkspaceID] == nil {
			u.workspaces[r.WorkspaceID] = &statusCounts{}
		}
		u.workspaces[r.WorkspaceID].add(r.Status, r.N)
	}
	for _, r := range runRows {
		u := forWorkspace(r.WorkspaceID)
		if u == nil {
			continue
		}
		u.runsTotal += r.N
		switch r.Status {
		case RunCompleted:
			u.runsCompleted += r.N
		case RunFailed:
			u.runsFailed += r.N
		}
	}
	// Definitions are paged, so the pass never holds more than a page of JSON. The mix counts workflows, not nodes: a workflow with two emails is one send_email user.
	after := uuid.Nil
	for {
		var page []definitionRow
		err := db.Raw(`SELECT id, workspace_id, status, definition FROM workflows WHERE status <> 'archived' AND id > ? ORDER BY id LIMIT ?`, after, definitionPageSize).Scan(&page).Error
		if err != nil {
			return nil, fmt.Errorf("read workflow definitions: %w", err)
		}
		for _, d := range page {
			u := forWorkspace(d.WorkspaceID)
			if u == nil {
				continue
			}
			summary := SummarizeDefinition(d.Definition)
			seen := make([][2]string, 0, len(summary.ActionTypes)+1)
			if summary.TriggerType != "" {
				seen = append(seen, [2]string{summary.TriggerType, "trigger"})
			}
			for _, t := range summary.ActionTypes {
				seen = append(seen, [2]string{t, "action"})
			}
			for _, s := range seen {
				entry := u.nodeTypes[s[0]]
				if entry == nil {
					entry = &nodeTypeUsage{kind: s[1]}
					u.nodeTypes[s[0]] = entry
				}
				entry.total++
				if d.Status == StatusEnabled {
					entry.enabled++
				}
			}
		}
		if len(page) < definitionPageSize {
			break
		}
		after = page[len(page)-1].ID
	}

	usages := make([]*organizationUsage, 0, len(order))
	for _, id := range order {
		if u := byOrganization[id]; u.statuses.live() > 0 {
			usages = append(usages, u)
		}
	}
	return usages, nil
}

// organizationContext reads the plan facts off the billing row the pass already holds, mirroring the workflows entitlement check: on Forma Cloud the Stripe product must carry the workflows feature, and the license grants it everywhere.
func (s *UsageSnapshot) organizationContext(w workspaceRow) organizationContext {
	c := organizationContext{createdAt: w.CreatedAt}
	var snapshot billing.Snapshot
	parsed := len(w.Stripe) > 0 && json.Unmarshal(w.Stripe, &snapshot) == nil
	var limits billing.Limits
	if len(w.Limits) > 0 && json.Unmarshal(w.Limits, &limits) == nil {
		c.monthlyWorkflowRunsLimit = limits.Monthly.WorkflowRuns
	}
	if parsed {
		if snapshot.Plan != "" {
			c.plan = &snapshot.Plan
		}
		c.billingInterval = snapshot.Interval
		c.subscriptionStatus = snapshot.SubscriptionStatus
	}
	if !s.cfg.IsFormaCloud {
		plan := selfHostedPlan
		c.plan = &plan
		c.hasWorkflowsEntitlement = true
		return c
	}
	c.hasWorkflowsEntitlement = parsed && slices.Contains(snapshot.Features, billing.FeatureWorkflows)
	return c
}

// Emit sends the snapshot: per organization one workflow_usage_snapshot, one workflow_node_type_snapshot per node type in use, and a refresh of the organization's and its workspaces' group properties. The organization is the distinct id: a scheduled job has no acting user.
func (s *UsageSnapshot) Emit(ctx context.Context, now time.Time) (SnapshotSummary, error) {
	usages, err := s.collect(ctx, now)
	if err != nil {
		return SnapshotSummary{}, err
	}
	deployment := "self_hosted"
	if s.cfg.IsFormaCloud {
		deployment = "cloud"
	}
	summary := SnapshotSummary{Organizations: len(usages)}
	for _, u := range usages {
		org := u.organizationID.String()
		groups := analytics.Groups{OrganizationID: org}
		analytics.Capture(org, usageSnapshotEvent, analytics.Properties{
			"organization_id":           org,
			"plan":                      u.context.plan,
			"has_workflows_entitlement": u.context.hasWorkflowsEntitlement,
			"workflows_total":           u.statuses.total(),
			"workflows_draft":           u.statuses.draft,
			"workflows_enabled":         u.statuses.enabled,
			"workflows_disabled":        u.statuses.disabled,
			"workflows_archived":        u.statuses.archived,
			"workspaces_with_workflows": len(u.workspaces),
			"runs_24h_total":            u.runsTotal,
			"runs_24h_completed":        u.runsCompleted,
			"runs_24h_failed":           u.runsFailed,
		}, groups)
		summary.Events++

		nodeTypes := make([]string, 0, len(u.nodeTypes))
		for t := range u.nodeTypes {
			nodeTypes = append(nodeTypes, t)
		}
		slices.Sort(nodeTypes)
		for _, t := range nodeTypes {
			n := u.nodeTypes[t]
			analytics.Capture(org, nodeTypeSnapshotEvent, analytics.Properties{
				"organization_id":   org,
				"plan":              u.context.plan,
				"node_kind":         n.kind,
				"node_type":         t,
				"workflows_total":   n.total,
				"workflows_enabled": n.enabled,
			}, groups)
			summary.Events++
		}

		// Only what this pass resolved: $group_set overwrites, so an unknown plan must be left out rather than sent as null over what the billing sync wrote.
		props := analytics.Properties{
			"deployment":                deployment,
			"license_status":            licenseStatus,
			"has_workflows_entitlement": u.context.hasWorkflowsEntitlement,
			"workflows_total":           u.statuses.total(),
			"workflows_enabled":         u.statuses.enabled,
			"organization_created_at":   u.context.createdAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		}
		for key, v := range map[string]*string{"plan": u.context.plan, "billing_interval": u.context.billingInterval, "subscription_status": u.context.subscriptionStatus} {
			if v != nil {
				props[key] = *v
			}
		}
		if u.context.monthlyWorkflowRunsLimit != nil {
			props["monthly_workflow_runs_limit"] = *u.context.monthlyWorkflowRunsLimit
		}
		analytics.GroupIdentify(analytics.GroupOrganization, org, props)

		for id, counts := range u.workspaces {
			analytics.GroupIdentify(analytics.GroupWorkspace, id.String(), analytics.Properties{"workflows_total": counts.total(), "workflows_enabled": counts.enabled})
			summary.Workspaces++
		}
	}
	return summary, nil
}

// UsageSnapshotArgs is forma2's workflows-usage.snapshot job.
type UsageSnapshotArgs struct {
	// Slot is the daily slot the run belongs to (jobs.Daily), which makes a caught-up slot run once.
	Slot time.Time `json:"slot"`
}

func (UsageSnapshotArgs) Kind() string { return "workflows-usage.snapshot" }

type usageSnapshotWorker struct {
	river.WorkerDefaults[UsageSnapshotArgs]
	snapshot *UsageSnapshot
	log      *slog.Logger
}

func (w *usageSnapshotWorker) Work(ctx context.Context, job *river.Job[UsageSnapshotArgs]) error {
	logContext := []any{"attempt", job.Attempt, "jobId", job.ID, "jobName", job.Kind, "maxAttempts", job.MaxAttempts, "queueName", job.Queue}
	// Without PostHog there is no point paying for the read pass.
	if !analytics.Enabled() {
		w.log.InfoContext(ctx, "Workflows usage snapshot skipped: PostHog is not configured", logContext...)
		return nil
	}
	w.log.InfoContext(ctx, "Workflows usage snapshot job started", logContext...)
	summary, err := w.snapshot.Emit(ctx, time.Now())
	if err != nil {
		return err
	}
	w.log.InfoContext(ctx, "Workflows usage snapshot job completed", append(logContext, "organizations", summary.Organizations, "workspaces", summary.Workspaces, "events", summary.Events)...)
	return nil
}

// RegisterUsageSnapshot adds the daily snapshot at 02:45 UTC, after the 02:15 usage report. A second run in a day emits the same facts again, which the dashboards deduplicate per organization and day.
func RegisterUsageSnapshot(reg *jobs.Registry, snapshot *UsageSnapshot, log *slog.Logger) {
	jobs.Add(reg, &usageSnapshotWorker{snapshot: snapshot, log: log})
	reg.AddPeriodic(jobs.Daily("workflows-usage-snapshot", jobs.DailyUTC{Hour: 2, Minute: 45}, func(slot time.Time) river.JobArgs { return UsageSnapshotArgs{Slot: slot} }))
}
