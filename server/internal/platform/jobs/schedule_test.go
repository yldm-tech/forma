package jobs

import (
	"testing"
	"time"
)

func TestDailyUTC(t *testing.T) {
	s := DailyUTC{Hour: 2, Minute: 15}
	tokyo := time.FixedZone("JST", 9*3600)
	for _, tc := range []struct{ now, want time.Time }{
		{time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC), time.Date(2026, 10, 23, 2, 15, 0, 0, time.UTC)},
		{time.Date(2026, 10, 23, 2, 15, 0, 0, time.UTC), time.Date(2026, 10, 24, 2, 15, 0, 0, time.UTC)},
		{time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 2, 15, 0, 0, time.UTC)},
		{time.Date(2026, 10, 23, 10, 0, 0, 0, tokyo), time.Date(2026, 10, 23, 2, 15, 0, 0, time.UTC)},
		{time.Date(2026, 10, 23, 12, 0, 0, 0, tokyo), time.Date(2026, 10, 24, 2, 15, 0, 0, time.UTC)},
	} {
		if got := s.Next(tc.now); !got.Equal(tc.want) {
			t.Errorf("Next(%v) = %v, want %v", tc.now, got, tc.want)
		}
	}
}

// berlinDaily is a daily schedule at a local time across a DST change, as the survey module's dailyAt is.
type berlinDaily struct{ loc *time.Location }

func (b berlinDaily) Next(current time.Time) time.Time {
	now := current.In(b.loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), 12, 30, 0, 0, b.loc)
	if !next.After(current) {
		next = time.Date(now.Year(), now.Month(), now.Day()+1, 12, 30, 0, 0, b.loc)
	}
	return next
}

func TestLatestSlot(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	daily := DailyUTC{Hour: 2, Minute: 15}
	for _, tc := range []struct {
		name     string
		schedule interface{ Next(time.Time) time.Time }
		now      time.Time
		want     time.Time
	}{
		{"before today's slot", daily, time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC), time.Date(2026, 10, 22, 2, 15, 0, 0, time.UTC)},
		{"at the slot", daily, time.Date(2026, 10, 23, 2, 15, 0, 0, time.UTC), time.Date(2026, 10, 23, 2, 15, 0, 0, time.UTC)},
		{"after the slot", daily, time.Date(2026, 10, 23, 23, 0, 0, 0, time.UTC), time.Date(2026, 10, 23, 2, 15, 0, 0, time.UTC)},
		// The day the clocks go back has 25 hours: 24.5 hours after a slot is still before the next one.
		{"across the autumn change", berlinDaily{berlin}, time.Date(2026, 10, 25, 11, 0, 0, 0, time.UTC), time.Date(2026, 10, 24, 12, 30, 0, 0, berlin)},
		{"after the autumn change's slot", berlinDaily{berlin}, time.Date(2026, 10, 25, 11, 30, 0, 0, time.UTC), time.Date(2026, 10, 25, 12, 30, 0, 0, berlin)},
	} {
		if got := latestSlot(tc.schedule, tc.now); !got.Equal(tc.want) {
			t.Errorf("%s: latestSlot(%v) = %v, want %v", tc.name, tc.now, got, tc.want)
		}
	}
}
