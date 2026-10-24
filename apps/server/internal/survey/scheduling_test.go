package survey

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/survey/scheduling"
)

func mustTime(t *testing.T, s string) *time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

// The cases of forma2's normalizeSurveyScheduling tests (lib/survey/service.scheduling.test.ts).
func TestNormalizeScheduling(t *testing.T) {
	s := scheduling.New(time.UTC, 0, 0)
	pub, cls := mustTime(t, "2026-05-01T00:00:00Z"), mustTime(t, "2026-05-03T00:00:00Z")
	str := func(v string) *string { return &v }
	cases := []struct {
		name             string
		current          *string
		status           string
		wantPub, wantCls bool
	}{
		{"a create publishing at once drops the publish date", nil, StatusInProgress, false, true},
		{"a create scheduling keeps both", nil, StatusPaused, true, true},
		{"an unchanged running survey keeps both", str(StatusInProgress), StatusInProgress, true, true},
		{"publishing by hand drops the publish date", str(StatusPaused), StatusInProgress, false, true},
		{"completing by hand drops both", str(StatusInProgress), StatusCompleted, false, false},
		{"a draft keeps both", str(StatusDraft), StatusDraft, true, true},
	}
	for _, tc := range cases {
		p, c, err := normalizeDates(s, tc.current, tc.status, pub, cls)
		if err != nil || (p != nil) != tc.wantPub || (c != nil) != tc.wantCls {
			t.Errorf("%s: %v %v %v", tc.name, p, c, err)
		}
	}
	if p, c, _ := normalizeDates(s, str(StatusInProgress), StatusPaused, nil, cls); p != nil || c != nil {
		t.Error("pausing by hand without a publish date kept the close date")
	}
	if _, _, err := normalizeDates(s, nil, StatusPaused, cls, pub); err == nil {
		t.Error("a close date before the publish date was accepted")
	} else if e, ok := apperr.As(err); !ok || e.Message != "Close date must be after publish date" {
		t.Errorf("error %v", err)
	}
}

func TestReconcileSchedules(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(48 * time.Hour)
	mk := func(status string, publishOn, closeOn *time.Time, archived bool) uuid.UUID {
		id := e.insert(`INSERT INTO surveys (workspace_id, name, status, publish_on, close_on) VALUES (?, 'S', ?, ?, ?) RETURNING id`, ws, status, publishOn, closeOn)
		if archived {
			e.exec(`UPDATE surveys SET archived_at = now() WHERE id = ?`, id)
		}
		return id
	}
	due := mk(StatusPaused, &past, &future, false)
	dueAndClosing := mk(StatusPaused, &past, &past, false)
	draft := mk(StatusDraft, &past, nil, false)
	notYet := mk(StatusPaused, &future, nil, false)
	archived := mk(StatusPaused, &past, nil, true)
	closing := mk(StatusInProgress, nil, &past, false)

	published, closed, err := e.svc.scheduler.Reconcile(context.Background(), time.Now(), nil)
	if err != nil || published != 2 || closed != 2 {
		t.Fatalf("Reconcile = %d published, %d closed, %v", published, closed, err)
	}
	status := func(id uuid.UUID) string { return e.text(`SELECT status FROM surveys WHERE id = ?`, id) }
	if status(due) != StatusInProgress || e.count(`SELECT count(*) FROM surveys WHERE id = ? AND publish_on IS NULL AND close_on IS NOT NULL`, due) != 1 {
		t.Error("the due survey was not published")
	}
	if status(dueAndClosing) != StatusCompleted || status(closing) != StatusCompleted {
		t.Error("a due close was not applied, or not after the publish in the same run")
	}
	for _, id := range []uuid.UUID{draft, notYet, archived} {
		if status(id) == StatusInProgress {
			t.Errorf("survey %s was published", id)
		}
	}
	if published, closed, _ = e.svc.scheduler.Reconcile(context.Background(), time.Now(), nil); published+closed != 0 {
		t.Error("a second run changed something")
	}
}

func TestSaveAppliesADueSchedule(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	id := e.createLink(owner, ws)
	sv := e.editorSurvey(owner, id)
	sv["status"] = StatusInProgress
	sv["closeOn"] = time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	var saved Survey
	e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, &saved)
	if saved.Status != StatusCompleted || saved.CloseOn != nil {
		t.Fatalf("a save with a passed close date left %s %v", saved.Status, saved.CloseOn)
	}
}

// A survey of an organization with its own time zone is snapped to that zone's local run time, not the instance's (Europe/Berlin here), and the scheduler applies it at that instant: St. John's is UTC-3:30 until its spring-forward change on 2026-03-08 and UTC-2:30 after it, offsets no whole hour fits.
func TestScheduledTransitionInTheOrganizationZoneAcrossDST(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	e.exec(`UPDATE organizations SET survey_scheduling_time_zone = 'America/St_Johns' WHERE id = ?`, org)
	ctx := context.Background()
	schedule, err := e.svc.scheduleOf(ctx, e.repo, ws)
	if err != nil {
		t.Fatal(err)
	}
	if got := schedule.Settings(); got.TimeZone != "America/St_Johns" || got.InstanceTimeZone != "Europe/Berlin" {
		t.Fatalf("schedule %+v", got)
	}
	before, publishDay, closeDay := mustTime(t, "2026-03-08T12:00:00Z"), mustTime(t, "2026-03-09T12:00:00Z"), mustTime(t, "2026-03-10T12:00:00Z")
	if got := schedule.Snap(before); !got.Equal(*mustTime(t, "2026-03-08T03:30:00Z")) {
		t.Fatalf("midnight of March 8 in St. John's is %s, want 03:30Z (NST)", got)
	}
	publishOn, closeOn := schedule.Snap(publishDay), schedule.Snap(closeDay)
	if !publishOn.Equal(*mustTime(t, "2026-03-09T02:30:00Z")) || !closeOn.Equal(*mustTime(t, "2026-03-10T02:30:00Z")) {
		t.Fatalf("midnights after the change %s %s, want 02:30Z (NDT)", publishOn, closeOn)
	}
	id := e.insert(`INSERT INTO surveys (workspace_id, name, status, publish_on, close_on) VALUES (?, 'S', ?, ?, ?) RETURNING id`, ws, StatusPaused, publishOn, closeOn)
	runs := scheduling.RunsFor(e.svc.schedule)
	for _, due := range []time.Time{*publishOn, *closeOn} {
		if next := runs.Next(due.Add(-10 * time.Minute)); !next.Equal(due) {
			t.Fatalf("the scheduler runs at %s, not at %s", next, due)
		}
	}
	status := func() string { return e.text(`SELECT status FROM surveys WHERE id = ?`, id) }
	if published, _, err := e.svc.scheduler.Reconcile(ctx, publishOn.Add(-time.Second), &id); err != nil || published != 0 || status() != StatusPaused {
		t.Fatalf("a second before its publish date: %d published, %v, status %s", published, err, status())
	}
	if published, _, err := e.svc.scheduler.Reconcile(ctx, *publishOn, &id); err != nil || published != 1 || status() != StatusInProgress {
		t.Fatalf("at its publish date: %d published, %v, status %s", published, err, status())
	}
	if _, closed, err := e.svc.scheduler.Reconcile(ctx, closeOn.Add(-time.Second), &id); err != nil || closed != 0 || status() != StatusInProgress {
		t.Fatalf("a second before its close date: %d closed, %v, status %s", closed, err, status())
	}
	if _, closed, err := e.svc.scheduler.Reconcile(ctx, *closeOn, &id); err != nil || closed != 1 || status() != StatusCompleted {
		t.Fatalf("at its close date: %d closed, %v, status %s", closed, err, status())
	}
}

// A save snaps the dates to the organization's zone; without one, to the instance's.
func TestSaveSnapsToTheOrganizationZone(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	picked := time.Now().AddDate(0, 0, 30).UTC()
	picked = time.Date(picked.Year(), picked.Month(), picked.Day(), 12, 0, 0, 0, time.UTC)
	for _, zone := range []string{"", "Asia/Tokyo"} {
		want := "Europe/Berlin"
		if zone != "" {
			e.exec(`UPDATE organizations SET survey_scheduling_time_zone = ? WHERE id = ?`, zone, org)
			want = zone
		}
		loc := mustLoad(t, want)
		id := e.createLink(owner, ws)
		sv := e.editorSurvey(owner, id)
		sv["status"] = StatusInProgress
		sv["closeOn"] = picked.Format(time.RFC3339)
		var saved Survey
		e.ok(e.do(owner, http.MethodPut, appPath(id, ""), sv), http.StatusOK, &saved)
		if expect := time.Date(picked.Year(), picked.Month(), picked.Day(), 0, 0, 0, 0, loc); saved.CloseOn == nil || !saved.CloseOn.Equal(expect) {
			t.Errorf("zone %q: close date %v, want midnight in %s (%s)", zone, saved.CloseOn, want, expect.UTC())
		}
	}
}

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestArchivePurge(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	old := e.createLink(owner, ws)
	recent := e.createLink(owner, ws)
	active := e.createLink(owner, ws)
	e.exec(`UPDATE surveys SET archived_at = now() - interval '31 days' WHERE id = ?`, old)
	e.exec(`UPDATE surveys SET archived_at = now() - interval '29 days' WHERE id = ?`, recent)

	purger := NewPurger(e.repo, e.svc.audit, e.svc.log)
	result, err := purger.Purge(context.Background(), time.Now())
	if err != nil || result != (PurgeResult{Deleted: 1}) {
		t.Fatalf("Purge = %+v, %v", result, err)
	}
	if e.count(`SELECT count(*) FROM surveys WHERE id = ?`, old) != 0 || e.count(`SELECT count(*) FROM surveys WHERE id IN (?, ?)`, recent, active) != 2 {
		t.Fatal("the purge deleted the wrong surveys")
	}
	// A survey restored between selection and delete is skipped by the re-check.
	cutoff := time.Now().Add(-archiveRetention)
	deleted, err := e.repo.deleteSurvey(context.Background(), uuid.MustParse(recent), &cutoff)
	if err != nil || deleted != nil {
		t.Fatalf("a survey archived after the cutoff was deleted: %v %v", deleted, err)
	}
}

// The purge is bounded by its batches, not by River's one-minute default job timeout, which a slow cascade delete would hit on every attempt; a run stopped from outside stops without counting the surveys it did not reach as failed.
func TestArchivePurgeIsNotCutShort(t *testing.T) {
	if got := (&purgeWorker{}).Timeout(nil); got != -1 {
		t.Fatalf("purge timeout %v, want none", got)
	}
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	old := e.createLink(owner, ws)
	e.exec(`UPDATE surveys SET archived_at = now() - interval '31 days' WHERE id = ?`, old)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := NewPurger(e.repo, e.svc.audit, e.svc.log).Purge(ctx, time.Now())
	if err == nil || result.Failed != 0 || strings.Contains(e.logs.String(), "Archived survey purge failed") {
		t.Fatalf("Purge = %+v, %v", result, err)
	}
}
