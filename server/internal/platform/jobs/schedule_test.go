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
