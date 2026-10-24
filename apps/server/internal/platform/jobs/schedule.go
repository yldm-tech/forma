package jobs

import (
	"time"

	"github.com/riverqueue/river"
)

// DailyUTC is a River schedule that fires once a day at hour:minute UTC, forma2's "<minute> <hour> * * *" cron in UTC. Like a cron, it does not drift with the process's start time. A slot that passes while no worker runs is caught up at the next start when the job is registered with Daily.
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

// slotMargin covers River firing a scheduled run up to 100 ms before its slot, so the run is counted to the slot it was scheduled for.
const slotMargin = time.Second

// Daily is a periodic job that runs once per slot of a daily schedule, and catches up a slot that passed while no worker led. River keeps periodic schedules only in memory, so a slot passing during a deploy, or before an instance that is started each morning comes up, would otherwise be skipped; forma2's BullMQ schedulers instead re-added the overdue iteration and ran it at the next boot. Each run carries its slot (args builds the job's arguments from it) and is unique by its arguments: every start enqueues the latest slot, which does nothing when that slot already has a job, and each scheduled run enqueues its own. A completed job is kept for 24 hours (CompletedJobRetentionPeriod), so the latest slot's job is still there to deduplicate against until the next slot has its own. A schedule that fires more often than daily works the same way.
func Daily(id string, schedule river.PeriodicSchedule, args func(slot time.Time) river.JobArgs) *river.PeriodicJob {
	return river.NewPeriodicJob(schedule, func() (river.JobArgs, *river.InsertOpts) {
		slot := latestSlot(schedule, time.Now().Add(slotMargin))
		if slot.IsZero() {
			return nil, nil
		}
		return args(slot), &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
	}, &river.PeriodicJobOpts{ID: id, RunOnStart: true})
}

// latestSlot is the last time at or before now that a daily schedule fires, zero if it did not fire in the last two days. Slots of a local-time schedule are 23 to 25 hours apart, so it walks forward from two days back rather than stepping back a day.
func latestSlot(schedule river.PeriodicSchedule, now time.Time) time.Time {
	var latest time.Time
	for next := schedule.Next(now.Add(-49 * time.Hour)); !next.After(now); next = schedule.Next(next) {
		latest = next
	}
	return latest
}
