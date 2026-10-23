package jobs

import "time"

// DailyUTC is a River schedule that fires once a day at hour:minute UTC, forma2's "<minute> <hour> * * *" cron in UTC. Like a cron, it does not drift with the process's start time: a restart keeps the same slot.
type DailyUTC struct {
	Hour, Minute int
}

func (d DailyUTC) Next(current time.Time) time.Time {
	now := current.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), d.Hour, d.Minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
