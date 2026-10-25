package webhook

import (
	"testing"
	"time"
)

// A failed delivery waits seconds, then minutes, then hours before its next attempt, and gives up after about a day.
func TestRetrySchedule(t *testing.T) {
	t.Parallel()
	for attempt, want := range map[int]time.Duration{1: 5 * time.Second, 2: 5 * time.Minute, 3: 30 * time.Minute, 4: 2 * time.Hour, 5: 5 * time.Hour, 7: 10 * time.Hour, 8: 10 * time.Hour} {
		if got := retryDelay(attempt); got != want {
			t.Errorf("retryDelay(%d) = %s, want %s", attempt, got, want)
		}
	}
	var total time.Duration
	for _, d := range retrySchedule {
		total += d
	}
	if opts := (DeliverArgs{}).InsertOpts(); opts.MaxAttempts != 8 || !opts.UniqueOpts.ByArgs || total < 24*time.Hour || total > 30*time.Hour {
		t.Errorf("%d attempts over %s, unique by args %v", opts.MaxAttempts, total, opts.UniqueOpts.ByArgs)
	}
}
