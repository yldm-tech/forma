package survey

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
)

func mustTime(t *testing.T, s string) *time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

func TestScheduleSnapsToTheLocalRunTime(t *testing.T) {
	berlin := Schedule{loc: mustLoad(t, "Europe/Berlin"), hour: 9, minute: 30}
	cases := []struct{ in, want string }{
		// Noon UTC, the editor's date-only selection, is that calendar day at 09:30 Berlin.
		{"2026-03-10T12:00:00Z", "2026-03-10T08:30:00Z"},
		// Late evening UTC is already the next day in Berlin.
		{"2026-03-10T23:30:00Z", "2026-03-11T08:30:00Z"},
		// Across the spring-forward change Berlin is UTC+2.
		{"2026-03-29T12:00:00Z", "2026-03-29T07:30:00Z"},
	}
	for _, tc := range cases {
		if got := berlin.snap(mustTime(t, tc.in)); !got.Equal(*mustTime(t, tc.want)) {
			t.Errorf("snap(%s) = %s, want %s", tc.in, got.UTC().Format(time.RFC3339), tc.want)
		}
	}
	if berlin.snap(nil) != nil {
		t.Error("snap(nil) is not nil")
	}
	midnight := Schedule{loc: mustLoad(t, "Europe/Berlin")}
	if got := midnight.next(*mustTime(t, "2026-03-10T22:59:00Z"), 0, 0); !got.Equal(*mustTime(t, "2026-03-10T23:00:00Z")) {
		t.Errorf("next run %s", got)
	}
	if got := midnight.next(*mustTime(t, "2026-03-10T23:00:00Z"), 0, 0); !got.Equal(*mustTime(t, "2026-03-11T23:00:00Z")) {
		t.Errorf("next run after the run %s", got)
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

// The cases of forma2's normalizeSurveyScheduling tests (lib/survey/service.scheduling.test.ts).
func TestNormalizeScheduling(t *testing.T) {
	s := Schedule{loc: time.UTC}
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
		p, c, err := s.normalize(tc.current, tc.status, pub, cls)
		if err != nil || (p != nil) != tc.wantPub || (c != nil) != tc.wantCls {
			t.Errorf("%s: %v %v %v", tc.name, p, c, err)
		}
	}
	if p, c, _ := s.normalize(str(StatusInProgress), StatusPaused, nil, cls); p != nil || c != nil {
		t.Error("pausing by hand without a publish date kept the close date")
	}
	if _, _, err := s.normalize(nil, StatusPaused, cls, pub); err == nil {
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
