package scheduling

import (
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func load(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func zone(name string) *string { return &name }

func TestSnapToTheLocalRunTime(t *testing.T) {
	t.Parallel()
	berlin := New(load(t, "Europe/Berlin"), 9, 30)
	cases := []struct{ in, want string }{
		// Noon UTC, the editor's date-only selection, is that calendar day at 09:30 Berlin.
		{"2026-03-10T12:00:00Z", "2026-03-10T08:30:00Z"},
		// Late evening UTC is already the next day in Berlin.
		{"2026-03-10T23:30:00Z", "2026-03-11T08:30:00Z"},
		// Across the spring-forward change Berlin is UTC+2.
		{"2026-03-29T12:00:00Z", "2026-03-29T07:30:00Z"},
	}
	for _, tc := range cases {
		in := at(t, tc.in)
		if got := berlin.Snap(&in); !got.Equal(at(t, tc.want)) {
			t.Errorf("Snap(%s) = %s, want %s", tc.in, got.Format(time.RFC3339), tc.want)
		}
	}
	if berlin.Snap(nil) != nil {
		t.Error("Snap(nil) is not nil")
	}
	midnight := New(load(t, "Europe/Berlin"), 0, 0)
	if got := midnight.NextAt(at(t, "2026-03-10T22:59:00Z"), 0, 0); !got.Equal(at(t, "2026-03-10T23:00:00Z")) {
		t.Errorf("next run %s", got)
	}
	if got := midnight.NextAt(at(t, "2026-03-10T23:00:00Z"), 0, 0); !got.Equal(at(t, "2026-03-11T23:00:00Z")) {
		t.Errorf("next run after the run %s", got)
	}
}

// An organization's own zone wins; without one, or with a name this build cannot load, the instance's zone is used, always at the instance's local time.
func TestZoneResolution(t *testing.T) {
	t.Parallel()
	instance := Instance(config.Config{SurveySchedulingTimeZone: "Europe/Berlin", SurveySchedulingLocalHour: 6, SurveySchedulingLocalMinute: 15})
	cases := []struct {
		name string
		zone *string
		want string
	}{
		{"no zone of its own", nil, "Europe/Berlin"},
		{"its own zone", zone("Asia/Tokyo"), "Asia/Tokyo"},
		{"an empty name", zone(""), "Europe/Berlin"},
		{"the host's zone", zone("Local"), "Europe/Berlin"},
		{"an unknown name", zone("Mars/Olympus_Mons"), "Europe/Berlin"},
	}
	for _, tc := range cases {
		got := instance.ForOrganization(tc.zone).Settings()
		want := Settings{TimeZone: tc.want, LocalHour: 6, LocalMinute: 15, InstanceTimeZone: "Europe/Berlin"}
		if got != want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, want)
		}
	}
	// A schedule resolved for one organization resolves again from the instance, not from that organization's zone.
	tokyo := instance.ForOrganization(zone("Asia/Tokyo"))
	if got := tokyo.ForOrganization(nil).Location().String(); got != "Europe/Berlin" {
		t.Errorf("an organization without a zone after Tokyo got %s", got)
	}
	in := at(t, "2026-05-01T12:00:00Z")
	if got := tokyo.Snap(&in); !got.Equal(at(t, "2026-04-30T21:15:00Z")) {
		t.Errorf("Tokyo snap %s, want 06:15 JST on May 1", got)
	}
	if !ValidZone("Asia/Tokyo") || ValidZone("") || ValidZone("Local") || ValidZone("Mars/Olympus_Mons") {
		t.Error("ValidZone")
	}
}

// Re-snapping keeps the calendar day a date was chosen for, also when the new zone is behind the old one, where snapping the stored instant again would move it to the day before.
func TestResnapKeepsTheDay(t *testing.T) {
	t.Parallel()
	tokyo, berlin := New(load(t, "Asia/Tokyo"), 0, 0), New(load(t, "Europe/Berlin"), 0, 0)
	in := at(t, "2026-10-30T12:00:00Z")
	tokyoMidnight := tokyo.Snap(&in) // 2026-10-29T15:00Z
	if !tokyoMidnight.Equal(at(t, "2026-10-29T15:00:00Z")) {
		t.Fatalf("Tokyo snap %s", tokyoMidnight)
	}
	if again := berlin.Snap(tokyoMidnight); !again.Equal(at(t, "2026-10-28T23:00:00Z")) {
		t.Fatalf("a plain snap in Berlin %s, expected the day before", again)
	}
	if got := berlin.Resnap(tokyo, tokyoMidnight); !got.Equal(at(t, "2026-10-29T23:00:00Z")) {
		t.Errorf("Resnap = %s, want midnight of October 30 in Berlin", got)
	}
	if berlin.Resnap(tokyo, nil) != nil {
		t.Error("Resnap(nil) is not nil")
	}
}

// At UTC+12 and beyond noon UTC is already the next day, so the editor sends a picked day as that day's run time in the zone; the snap keeps it there.
func TestSnapKeepsTheEditorsDayEastOfUTCPlus12(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ zone, sent string }{
		{"Pacific/Auckland", "2026-04-30T12:00:00Z"},
		{"Pacific/Chatham", "2026-04-30T11:15:00Z"},
		{"Pacific/Kiritimati", "2026-04-30T10:00:00Z"},
	} {
		s := New(load(t, tc.zone), 0, 0)
		sent := at(t, tc.sent)
		if got := s.Snap(&sent); !got.Equal(sent) {
			t.Errorf("%s: Snap(%s) = %s, want it unchanged", tc.zone, tc.sent, got.Format(time.RFC3339))
		}
		if local := sent.In(s.Location()); local.Day() != 1 || local.Month() != time.May {
			t.Errorf("%s: %s is %s there, not May 1", tc.zone, tc.sent, local)
		}
		// Noon UTC, what the editor used to send, is May 2 there.
		noon := at(t, "2026-05-01T12:00:00Z")
		if local := s.Snap(&noon).In(s.Location()); local.Day() != 2 {
			t.Errorf("%s: noon UTC snapped to %s, expected May 2", tc.zone, local)
		}
	}
}

// A zone a client names is a schedule at the same local time; anything that is not a zone an organization may choose is not.
func TestInZone(t *testing.T) {
	t.Parallel()
	base := New(load(t, "Europe/Berlin"), 6, 30)
	in, ok := base.InZone("Pacific/Auckland")
	if !ok || in.Location().String() != "Pacific/Auckland" || in.Settings().LocalHour != 6 || in.Settings().LocalMinute != 30 || in.Settings().InstanceTimeZone != "Europe/Berlin" {
		t.Fatalf("InZone(Pacific/Auckland) = %+v, %v", in.Settings(), ok)
	}
	for _, name := range []string{"", "Local", "Mars/Olympus"} {
		if got, ok := base.InZone(name); ok || got.Location() != base.Location() {
			t.Errorf("InZone(%q) = %s, %v", name, got.Location(), ok)
		}
	}
}

// The quarter-hour runs reach every zone's local run time at its instant, including zones a whole hour does not fit (India, Nepal, Chatham) and on both sides of a daylight saving change.
func TestRunsHitEveryZonesLocalTime(t *testing.T) {
	t.Parallel()
	zones := []string{"UTC", "Europe/Berlin", "America/New_York", "America/St_Johns", "Asia/Kolkata", "Asia/Kathmandu", "Australia/Eucla", "Australia/Lord_Howe", "Pacific/Chatham", "Asia/Tokyo"}
	days := []string{"2026-01-15T12:00:00Z", "2026-03-08T12:00:00Z", "2026-03-09T12:00:00Z", "2026-03-29T12:00:00Z", "2026-03-30T12:00:00Z", "2026-07-01T12:00:00Z", "2026-11-01T12:00:00Z", "2026-11-02T12:00:00Z"}
	for _, minute := range []int{0, 7, 30, 59} {
		runs := RunsFor(New(time.UTC, 0, minute))
		for _, name := range zones {
			s := New(time.UTC, 0, minute).ForOrganization(zone(name))
			for _, day := range days {
				d := at(t, day)
				due := *s.Snap(&d)
				if got := runs.Next(due.Add(-time.Second)); !got.Equal(due) {
					t.Errorf("%s at 00:%02d on %s: due %s, next run %s", name, minute, day[:10], due.Format(time.RFC3339), got.Format(time.RFC3339))
				}
			}
		}
	}
	runs := RunsFor(New(time.UTC, 0, 20))
	if got := runs.Next(at(t, "2026-03-09T02:05:00Z")); !got.Equal(at(t, "2026-03-09T02:05:00Z").Add(15 * time.Minute)) {
		t.Errorf("a run exactly on the grid is followed by the next one, got %s", got)
	}
	if got := runs.Next(at(t, "2026-03-09T02:06:00Z")); !got.Equal(at(t, "2026-03-09T02:20:00Z")) {
		t.Errorf("next run %s", got)
	}
}
