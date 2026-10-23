package answers

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Date answers and bounds are YYYY-MM-DD strings compared as text. Relative bounds are resolved against the server's calendar day, which, like forma2's Node.js server, is UTC.

// maxRelativeDateAmount is forma2's MAX_RELATIVE_DATE_AMOUNT.
const maxRelativeDateAmount = 3650

// relative is forma2's TRelativeDateBound as stored; a missing or malformed part reads as the runtime read it.
type relative struct {
	amount    float64
	finite    bool
	workdays  bool
	backwards bool
}

// relativeBound reads params[key] as a relative bound; ok is false when params has no such key (a fixed bound).
func relativeBound(params map[string]any, key string) (relative, bool) {
	raw, ok := params[key]
	if !ok {
		return relative{}, false
	}
	m, _ := raw.(map[string]any)
	var r relative
	if a, ok := m["amount"].(float64); ok && !math.IsInf(a, 0) && !math.IsNaN(a) {
		r.amount, r.finite = a, true
	}
	r.workdays = m["unit"] == "workingDays"
	r.backwards = m["direction"] == "before"
	return r, true
}

func civilDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func isoDay(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}

func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}

// addWorkingDays is the runtime's addWorkingDays: weekends are stepped over, not counted, and 0 keeps the day as it is.
func addWorkingDays(t time.Time, days int) time.Time {
	result := civilDay(t)
	step := 1
	if days < 0 {
		step = -1
	}
	for remaining := days * step; remaining > 0; {
		result = result.AddDate(0, 0, step)
		if !isWeekend(result) {
			remaining--
		}
	}
	return result
}

// resolveRelativeDate is the runtime's resolveRelativeDate: the day the bound names from reference, its amount clamped to whole days within the schema's cap.
func resolveRelativeDate(r relative, reference time.Time) string {
	amount := 0
	if r.finite {
		amount = int(math.Min(math.Max(0, math.Trunc(r.amount)), maxRelativeDateAmount))
	}
	if r.backwards {
		amount = -amount
	}
	if r.workdays {
		return isoDay(addWorkingDays(reference, amount))
	}
	return isoDay(civilDay(reference).AddDate(0, 0, amount))
}

// timezoneGrace is the runtime's server-side applyTimezoneGrace: a respondent ahead of or behind the server's calendar may answer with their own today, so a lower bound resolves against yesterday (direction -1) and an upper bound against tomorrow (+1).
func timezoneGrace(now time.Time, direction int) time.Time {
	return civilDay(now).AddDate(0, 0, direction)
}

// shiftISODate is the runtime's shiftISODate: a YYYY-MM-DD day moved by whole calendar days, rolling over months as Date does.
func shiftISODate(day string, days int) string {
	parts := strings.Split(day, "-")
	if len(parts) != 3 {
		return "NaN-NaN-NaN"
	}
	y, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	d, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return "NaN-NaN-NaN"
	}
	return isoDay(time.Date(y, time.Month(m), d+days, 0, 0, 0, 0, time.UTC))
}

// Which way the timezone grace moves a relative range: for isBetween the answer sits inside the window, which therefore widens; for isNotBetween it sits outside, so the hole shrinks. Either way the server accepts at least what the respondent's browser did.
const (
	graceInside = iota
	graceOutside
)

// relativeRange resolves both ends of a relative range; ok is false for a fixed range.
func relativeRange(params map[string]any, now time.Time, grace int) (start, end string, ok bool) {
	_, hasStart := params["relativeStart"]
	_, hasEnd := params["relativeEnd"]
	if !hasStart || !hasEnd {
		return "", "", false
	}
	startBound, _ := relativeBound(params, "relativeStart")
	endBound, _ := relativeBound(params, "relativeEnd")
	startGrace, endGrace := -1, 1
	if grace == graceOutside {
		startGrace, endGrace = 1, -1
	}
	return resolveRelativeDate(startBound, timezoneGrace(now, startGrace)), resolveRelativeDate(endBound, timezoneGrace(now, endGrace)), true
}

// rangeArgs are a range rule's message parameters: a relative range resolved without the grace, so the respondent reads the days the picker offered.
func rangeArgs(params map[string]any, now time.Time) map[string]string {
	_, hasStart := params["relativeStart"]
	_, hasEnd := params["relativeEnd"]
	if hasStart && hasEnd {
		startBound, _ := relativeBound(params, "relativeStart")
		endBound, _ := relativeBound(params, "relativeEnd")
		return map[string]string{"startDate": resolveRelativeDate(startBound, now), "endDate": resolveRelativeDate(endBound, now)}
	}
	return map[string]string{"startDate": arg(params, "startDate"), "endDate": arg(params, "endDate")}
}
