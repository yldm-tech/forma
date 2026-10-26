package retention

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
)

type env struct {
	t   *testing.T
	db  *gorm.DB
	now time.Time
}

func newEnv(t *testing.T) *env {
	return &env{t: t, db: pgtest.GORM(t), now: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
}

func (e *env) id(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query+" RETURNING id", args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return id
}

func (e *env) org(runDays, displayDays *int) uuid.UUID {
	return e.id(`INSERT INTO organizations (name, workflow_run_retention_days, display_retention_days) VALUES ('Org', ?, ?)`, runDays, displayDays)
}

func (e *env) workspace(org uuid.UUID) uuid.UUID {
	return e.id(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?)`, org, uuid.NewString())
}

func (e *env) survey(workspace uuid.UUID) uuid.UUID {
	return e.id(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Survey')`, workspace)
}

func (e *env) workflow(workspace uuid.UUID) uuid.UUID {
	return e.id(`INSERT INTO workflows (workspace_id, name, definition) VALUES (?, 'Flow', '{}')`, workspace)
}

func (e *env) run(workflow, workspace uuid.UUID, status string, age time.Duration) {
	e.id(`INSERT INTO workflow_runs (workflow_id, workspace_id, status, trigger_type, trigger_payload, created_at) VALUES (?, ?, ?, 'response.completed', '{}', ?)`, workflow, workspace, status, e.now.Add(-age))
}

func (e *env) display(survey uuid.UUID, age time.Duration) uuid.UUID {
	return e.id(`INSERT INTO displays (survey_id, created_at) VALUES (?, ?)`, survey, e.now.Add(-age))
}

func (e *env) count(query string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Raw(query, args...).Scan(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n
}

func days(n int) *int { return &n }

const day = 24 * time.Hour

func TestCutoffSubtractsWholeDays(t *testing.T) {
	now := time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC)
	if got := Cutoff(now, 30); !got.Equal(time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("Cutoff = %v", got)
	}
}

// TestSweepAppliesEachOrganizationsWindows: terminal runs and unconverted impressions older than the organization's own windows go, live runs, converted impressions and rows inside the window stay, and organizations without windows are not visited at all.
func TestSweepAppliesEachOrganizationsWindows(t *testing.T) {
	e := newEnv(t)
	org := e.org(days(30), days(7))
	ws := e.workspace(org)
	flow := e.workflow(ws)
	for _, status := range []string{"completed", "failed", "canceled", "queued", "running"} {
		e.run(flow, ws, status, 31*day)
		e.run(flow, ws, status, 29*day)
	}
	survey := e.survey(ws)
	e.display(survey, 8*day)
	converted := e.display(survey, 8*day)
	e.id(`INSERT INTO responses (survey_id, display_id) VALUES (?, ?)`, survey, converted)
	e.display(survey, 6*day)

	untouched := e.org(nil, nil)
	untouchedWS := e.workspace(untouched)
	e.run(e.workflow(untouchedWS), untouchedWS, "completed", 400*day)
	e.display(e.survey(untouchedWS), 400*day)

	onlyRuns := e.org(days(1), nil)
	onlyRunsWS := e.workspace(onlyRuns)
	e.display(e.survey(onlyRunsWS), 400*day)

	s := NewSweeper(e.db, slog.New(slog.DiscardHandler))
	dry, err := s.Sweep(t.Context(), e.now, true)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.Organizations != 2 || dry.WorkflowRuns.Matched != 3 || dry.Displays.Matched != 1 || dry.WorkflowRuns.Deleted != 0 || dry.Displays.Deleted != 0 {
		t.Fatalf("dry run = %+v, want 3 runs and 1 display matched in 2 organizations, nothing deleted", dry)
	}
	if n := e.count(`SELECT count(*) FROM workflow_runs`); n != 11 {
		t.Fatalf("a dry run deleted runs: %d left", n)
	}

	real, err := s.Sweep(t.Context(), e.now, false)
	if err != nil {
		t.Fatal(err)
	}
	if real.DryRun || real.WorkflowRuns.Deleted != 3 || real.Displays.Deleted != 1 || real.WorkflowRuns.Matched != 0 || real.WorkflowRuns.CappedUnits != 0 {
		t.Fatalf("sweep = %+v, want 3 runs and 1 display deleted", real)
	}
	if n := e.count(`SELECT count(*) FROM workflow_runs WHERE workspace_id = ? AND created_at < ?`, ws, Cutoff(e.now, 30)); n != 2 {
		t.Errorf("%d old runs left, want the queued and the running one", n)
	}
	if n := e.count(`SELECT count(*) FROM displays WHERE survey_id = ?`, survey); n != 2 {
		t.Errorf("%d displays left, want the converted and the recent one", n)
	}
	if n := e.count(`SELECT count(*) FROM responses WHERE display_id = ?`, converted); n != 1 {
		t.Error("the response lost its display")
	}
	if n := e.count(`SELECT count(*) FROM displays d JOIN surveys s ON s.id = d.survey_id JOIN workspaces w ON w.id = s.workspace_id WHERE w.organization_id IN (?, ?)`, untouched, onlyRuns); n != 2 {
		t.Errorf("%d displays left in organizations without a display window, want 2", n)
	}
}

// TestSweepDefersTheRemainderOfALargeUnit: a unit stops after maxDeleteBatches full batches and is counted as capped; the next sweep continues.
func TestSweepDefersTheRemainderOfALargeUnit(t *testing.T) {
	e := newEnv(t)
	calls := 0
	deleted, capped, err := drain(func() (int64, error) {
		calls++
		return deleteBatchSize, nil
	})
	if err != nil || !capped || calls != maxDeleteBatches || deleted != maxDeleteBatches*deleteBatchSize {
		t.Fatalf("drain = %d, %v, %v after %d calls", deleted, capped, err, calls)
	}
	calls = 0
	if _, capped, _ := drain(func() (int64, error) {
		calls++
		if calls == 3 {
			return 10, nil
		}
		return deleteBatchSize, nil
	}); capped || calls != 3 {
		t.Fatalf("a short batch did not end the unit: capped %v after %d calls", capped, calls)
	}

	// Surveys are walked in pages: displays of a survey beyond the first page are swept too.
	org := e.org(nil, days(1))
	ws := e.workspace(org)
	var last uuid.UUID
	for range surveyPageSize + 1 {
		last = e.survey(ws)
	}
	e.display(last, 2*day)
	res, err := NewSweeper(e.db, slog.New(slog.DiscardHandler)).Sweep(t.Context(), e.now, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Displays.Matched != 1 {
		t.Errorf("matched %d displays, want the one on the second page", res.Displays.Matched)
	}
}

func TestJobLogsTheDryRunResult(t *testing.T) {
	e := newEnv(t)
	org := e.org(days(1), nil)
	ws := e.workspace(org)
	e.run(e.workflow(ws), ws, "failed", 2*day)

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	w := &sweepWorker{sweeper: NewSweeper(e.db, log), dryRun: true, log: log}
	if err := w.Work(context.Background(), &river.Job[SweepArgs]{JobRow: &rivertype.JobRow{ID: 1, Kind: SweepArgs{}.Kind(), Attempt: 1, MaxAttempts: 3, Queue: "default"}}); err != nil {
		t.Fatal(err)
	}
	var lines []map[string]any
	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, m)
	}
	if len(lines) != 2 || lines[0]["msg"] != "Retention sweep job started" || lines[1]["msg"] != "Retention sweep job completed (dry run, nothing deleted)" {
		t.Fatalf("log = %v", lines)
	}
	runs, _ := lines[1]["workflowRuns"].(map[string]any)
	if lines[1]["dryRun"] != true || lines[1]["organizations"] != float64(1) || runs["matched"] != float64(1) || runs["deleted"] != float64(0) {
		t.Errorf("completion line = %v", lines[1])
	}
	if n := e.count(`SELECT count(*) FROM workflow_runs`); n != 1 {
		t.Error("the job deleted while the sweep is a dry run")
	}
}

// TestJobDeletesWhenTheInstanceTurnsDryRunOff: RETENTION_SWEEP_DRY_RUN=0 makes the job delete.
func TestJobDeletesWhenTheInstanceTurnsDryRunOff(t *testing.T) {
	e := newEnv(t)
	org := e.org(days(1), nil)
	ws := e.workspace(org)
	e.run(e.workflow(ws), ws, "failed", 2*day)

	log := slog.New(slog.DiscardHandler)
	w := &sweepWorker{sweeper: NewSweeper(e.db, log), dryRun: false, log: log}
	if err := w.Work(context.Background(), &river.Job[SweepArgs]{JobRow: &rivertype.JobRow{ID: 1, Kind: SweepArgs{}.Kind(), Attempt: 1, MaxAttempts: 3, Queue: "default"}}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM workflow_runs`); n != 0 {
		t.Errorf("%d runs left, want the expired one deleted", n)
	}
	r, err := LastReport(t.Context(), e.db, org)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.DryRun || r.WorkflowRuns.Deleted != 1 || r.WorkflowRuns.Matched != 0 {
		t.Errorf("report %+v, want the one run deleted", r)
	}
}

// TestSweepRecordsEachOrganizationsLastSweep: every organization the sweep visits gets its own counts and the windows they were taken under, replaced by the next sweep; an organization without windows gets none.
func TestSweepRecordsEachOrganizationsLastSweep(t *testing.T) {
	e := newEnv(t)
	a := e.org(days(30), days(7))
	aWS := e.workspace(a)
	aFlow := e.workflow(aWS)
	e.run(aFlow, aWS, "completed", 31*day)
	e.run(aFlow, aWS, "completed", 40*day)
	e.display(e.survey(aWS), 8*day)
	b := e.org(nil, days(1))
	bWS := e.workspace(b)
	e.run(e.workflow(bWS), bWS, "completed", 400*day)
	none := e.org(nil, nil)

	s := NewSweeper(e.db, slog.New(slog.DiscardHandler))
	if _, err := s.Sweep(t.Context(), e.now, true); err != nil {
		t.Fatal(err)
	}
	ra, err := LastReport(t.Context(), e.db, a)
	if err != nil {
		t.Fatal(err)
	}
	if ra == nil || !ra.DryRun || !ra.SweptAt.Equal(e.now) || *ra.WorkflowRunRetentionDays != 30 || *ra.DisplayRetentionDays != 7 ||
		ra.WorkflowRuns != (PassResult{Matched: 2}) || ra.Displays != (PassResult{Matched: 1}) {
		t.Fatalf("report of the first organization = %+v", ra)
	}
	rb, err := LastReport(t.Context(), e.db, b)
	if err != nil {
		t.Fatal(err)
	}
	if rb == nil || rb.WorkflowRunRetentionDays != nil || *rb.DisplayRetentionDays != 1 || rb.WorkflowRuns != (PassResult{}) || rb.Displays != (PassResult{}) {
		t.Fatalf("report of the organization with only a display window = %+v, want its runs left out", rb)
	}
	if r, err := LastReport(t.Context(), e.db, none); err != nil || r != nil {
		t.Fatalf("report of an organization without windows = %+v, %v; want none", r, err)
	}

	later := e.now.Add(day)
	if _, err := s.Sweep(t.Context(), later, false); err != nil {
		t.Fatal(err)
	}
	ra, err = LastReport(t.Context(), e.db, a)
	if err != nil {
		t.Fatal(err)
	}
	if ra.DryRun || !ra.SweptAt.Equal(later) || ra.WorkflowRuns != (PassResult{Deleted: 2}) || ra.Displays != (PassResult{Deleted: 1}) {
		t.Fatalf("report after a real sweep = %+v, want it replaced", ra)
	}
	if n := e.count(`SELECT count(*) FROM organization_retention_sweeps`); n != 2 {
		t.Errorf("%d reports, want one per organization with a window", n)
	}
}

// TestSweepRecordsWhatAFailedPassDeleted: when a pass fails partway, the rows it already deleted are on the organization's report, marked as stopped early, and the other pass still runs; the next complete sweep clears the mark.
func TestSweepRecordsWhatAFailedPassDeleted(t *testing.T) {
	e := newEnv(t)
	org := e.org(days(1), days(1))
	first, second := e.workspace(org), e.workspace(org)
	if second.String() < first.String() {
		first, second = second, first
	}
	e.run(e.workflow(first), first, "completed", 2*day)
	e.run(e.workflow(second), second, "completed", 2*day)
	e.display(e.survey(first), 2*day)

	s := NewSweeper(e.db, slog.New(slog.DiscardHandler))
	if _, err := s.Sweep(t.Context(), e.now, true); err != nil {
		t.Fatal(err)
	}
	// The runs pass deletes in the first workspace, then fails in the second.
	for _, statement := range []string{
		`CREATE FUNCTION refuse_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$`,
		`CREATE TRIGGER refuse_delete BEFORE DELETE ON workflow_runs FOR EACH ROW WHEN (OLD.workspace_id = '` + second.String() + `') EXECUTE FUNCTION refuse_delete()`,
	} {
		if err := e.db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.Sweep(t.Context(), e.now, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkflowRuns.Deleted != 1 || result.Displays.Deleted != 1 {
		t.Fatalf("result = %+v, want the first workspace's run and the impression deleted", result)
	}
	r, err := LastReport(t.Context(), e.db, org)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.DryRun || !r.StoppedEarly || r.WorkflowRuns != (PassResult{Deleted: 1}) || r.Displays != (PassResult{Deleted: 1}) {
		t.Fatalf("report = %+v, want a deleting sweep that stopped early after one run and one impression", r)
	}
	if n := e.count(`SELECT count(*) FROM workflow_runs`); n != 1 {
		t.Fatalf("%d runs left, want the refused one", n)
	}

	if err := e.db.Exec(`DROP TRIGGER refuse_delete ON workflow_runs`).Error; err != nil {
		t.Fatal(err)
	}
	later := e.now.Add(day)
	if _, err := s.Sweep(t.Context(), later, false); err != nil {
		t.Fatal(err)
	}
	if r, err = LastReport(t.Context(), e.db, org); err != nil {
		t.Fatal(err)
	}
	if r.StoppedEarly || r.WorkflowRuns != (PassResult{Deleted: 1}) || r.Displays != (PassResult{}) {
		t.Fatalf("report after a complete sweep = %+v, want the remaining run deleted and no stopped-early mark", r)
	}
}

// TestSweepStopsWhenItsContextEnds: a canceled sweep returns the context's error and leaves the organizations it did not reach with their last report.
func TestSweepStopsWhenItsContextEnds(t *testing.T) {
	e := newEnv(t)
	org := e.org(days(1), nil)
	ws := e.workspace(org)
	e.run(e.workflow(ws), ws, "completed", 2*day)
	s := NewSweeper(e.db, slog.New(slog.DiscardHandler))
	if _, err := s.Sweep(t.Context(), e.now, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Sweep(ctx, e.now.Add(day), false); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	r, err := LastReport(t.Context(), e.db, org)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || !r.DryRun || r.StoppedEarly || !r.SweptAt.Equal(e.now) || r.WorkflowRuns != (PassResult{Matched: 1}) {
		t.Fatalf("report = %+v, want the earlier dry run kept", r)
	}
}
