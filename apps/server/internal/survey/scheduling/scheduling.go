// Package scheduling is when scheduled surveys open and close: a publish or close date is a calendar day, snapped to the configured local time of that day (SURVEY_SCHEDULING_LOCAL_HOUR and _MINUTE) in the time zone of the survey's organization, or in SURVEY_SCHEDULING_TIME_ZONE when the organization has none. The survey module snaps the dates and runs the scheduler, the organization module re-snaps them when the zone changes, and the workspace and organization payloads tell the SPA the zone in effect.
package scheduling

import (
	"time"

	// Organization zones are validated and loaded from the IANA database; embedding it keeps both independent of the host's zoneinfo.
	_ "time/tzdata"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Schedule is a time zone and the local time of day scheduled surveys open and close in it.
type Schedule struct {
	loc          *time.Location
	hour, minute int
	// instance is SURVEY_SCHEDULING_TIME_ZONE, the zone an organization without its own uses.
	instance *time.Location
}

// Instance is the instance's schedule, from the configuration, which validated the zone and the time at startup.
func Instance(cfg config.Config) Schedule {
	loc, err := time.LoadLocation(cfg.SurveySchedulingTimeZone)
	if err != nil {
		loc = time.UTC
	}
	return Schedule{loc: loc, hour: cfg.SurveySchedulingLocalHour, minute: cfg.SurveySchedulingLocalMinute, instance: loc}
}

// New is a schedule at hour:minute in loc, which is also the instance's zone.
func New(loc *time.Location, hour, minute int) Schedule {
	return Schedule{loc: loc, hour: hour, minute: minute, instance: loc}
}

// ValidZone is whether name is an IANA time zone an organization may choose: forma2's isValidIanaTimeZone, which also refuses "Local", the host's zone.
func ValidZone(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// ForOrganization is the schedule of an organization whose own zone is zone: that zone at the same local time, or the instance's zone when zone is nil. A stored name this build cannot load (it was validated when it was saved) falls back to the instance's zone too, so a survey always has a schedule.
func (s Schedule) ForOrganization(zone *string) Schedule {
	out := s
	out.loc = s.instance
	if zone == nil || *zone == "" || *zone == "Local" {
		return out
	}
	if loc, err := time.LoadLocation(*zone); err == nil {
		out.loc = loc
	}
	return out
}

// Location is the schedule's time zone.
func (s Schedule) Location() *time.Location { return s.loc }

// Snap is forma2's normalizeDateOnlySelectionToSurveySchedulingDateTime: the instant's calendar day in the schedule's zone, at the schedule's local time.
func (s Schedule) Snap(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	local := t.In(s.loc)
	out := time.Date(local.Year(), local.Month(), local.Day(), s.hour, s.minute, 0, 0, s.loc).UTC()
	return &out
}

// Resnap moves a date snapped in from to the same calendar day in s: what an organization's scheduled dates become when its zone changes, so a survey still opens or closes on the day that was chosen.
func (s Schedule) Resnap(from Schedule, t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	local := t.In(from.loc)
	out := time.Date(local.Year(), local.Month(), local.Day(), s.hour, s.minute, 0, 0, s.loc).UTC()
	return &out
}

// NextAt is the first time strictly after t that the schedule's zone shows hour:minute.
func (s Schedule) NextAt(t time.Time, hour, minute int) time.Time {
	local := t.In(s.loc)
	run := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, s.loc)
	if !run.After(local) {
		run = time.Date(local.Year(), local.Month(), local.Day()+1, hour, minute, 0, 0, s.loc)
	}
	return run
}

// Settings is a schedule as the SPA reads it.
type Settings struct {
	// TimeZone is the zone in effect: the organization's own, or the instance's.
	TimeZone    string `json:"timeZone"`
	LocalHour   int    `json:"localHour"`
	LocalMinute int    `json:"localMinute"`
	// InstanceTimeZone is SURVEY_SCHEDULING_TIME_ZONE, what an organization without its own zone uses.
	InstanceTimeZone string `json:"instanceTimeZone"`
}

// Settings is the schedule for the SPA.
func (s Schedule) Settings() Settings {
	return Settings{TimeZone: s.loc.String(), LocalHour: s.hour, LocalMinute: s.minute, InstanceTimeZone: s.instance.String()}
}

// runInterval is how often the scheduler runs: every UTC offset in use is a whole number of quarter hours, so every zone's local hour:minute falls on the same quarter-hour grid, offset by the configured minute.
const runInterval = 15 * time.Minute

// Runs is the scheduler's River schedule: every quarter hour at the configured local minute, which is the instant every organization's local hour:minute arrives, whatever its zone, so a survey opens or closes at its snapped instant rather than at the next run in the instance's zone.
type Runs struct {
	offset time.Duration
}

// RunsFor is the run schedule of the instance's local time.
func RunsFor(s Schedule) Runs {
	return Runs{offset: time.Duration(s.minute%int(runInterval/time.Minute)) * time.Minute}
}

func (r Runs) Next(current time.Time) time.Time {
	next := current.UTC().Truncate(runInterval).Add(r.offset)
	for !next.After(current) {
		next = next.Add(runInterval)
	}
	return next
}
