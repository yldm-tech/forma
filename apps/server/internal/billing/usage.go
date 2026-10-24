package billing

import "time"

// UsageCycle is the window monthly usage is counted in: [Start, End).
type UsageCycle struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// MonthlyUsageCycle is forma2's getMonthlyUsageCycleWindow. With an anchor (the subscription's billing cycle anchor), cycles start every month on the anchor's UTC day and time, clamped to the last day of shorter months; this holds for yearly subscriptions too, since usage is billed monthly. Without one the cycle is the UTC calendar month.
func MonthlyUsageCycle(anchor *time.Time, now time.Time) UsageCycle {
	now = now.UTC()
	if anchor == nil {
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return UsageCycle{Start: start, End: start.AddDate(0, 1, 0)}
	}
	a := anchor.UTC()
	offset := (now.Year()-a.Year())*12 + int(now.Month()) - int(a.Month())
	start := addAnchoredMonths(a, offset)
	for start.After(now) {
		offset--
		start = addAnchoredMonths(a, offset)
	}
	end := addAnchoredMonths(a, offset+1)
	for !end.After(now) {
		offset++
		start = addAnchoredMonths(a, offset)
		end = addAnchoredMonths(a, offset+1)
	}
	return UsageCycle{Start: start, End: end}
}

// addAnchoredMonths is forma2's addUtcMonthsFromAnchor: the anchor moved by months, its day clamped to the target month's length instead of overflowing into the next month as time.AddDate would.
func addAnchoredMonths(anchor time.Time, months int) time.Time {
	index := int(anchor.Month()) - 1 + months
	year := anchor.Year() + floorDiv(index, 12)
	month := time.Month(index - floorDiv(index, 12)*12 + 1)
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(year, month, min(anchor.Day(), lastDay), anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC)
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}
