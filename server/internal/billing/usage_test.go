package billing

import (
	"testing"
	"time"
)

func TestMonthlyUsageCycle(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	anchor := func(s string) *time.Time { v := at(s); return &v }
	cases := []struct {
		name       string
		anchor     *time.Time
		now        string
		start, end string
	}{
		{"a regular anchor day", anchor("2026-01-15T10:30:00Z"), "2026-03-09T12:00:00Z", "2026-02-15T10:30:00Z", "2026-03-15T10:30:00Z"},
		{"yearly subscriptions keep monthly windows on the original day", anchor("2026-01-15T00:00:00Z"), "2026-11-20T12:00:00Z", "2026-11-15T00:00:00Z", "2026-12-15T00:00:00Z"},
		{"anchors on the 31st clamp to short months", anchor("2026-01-31T08:00:00Z"), "2026-02-28T12:00:00Z", "2026-02-28T08:00:00Z", "2026-03-31T08:00:00Z"},
		{"no anchor is the UTC calendar month", nil, "2026-03-09T12:00:00Z", "2026-03-01T00:00:00Z", "2026-04-01T00:00:00Z"},
		{"the billing anchor of the previous month", anchor("2026-02-10T00:00:00Z"), "2026-03-09T12:00:00Z", "2026-02-10T00:00:00Z", "2026-03-10T00:00:00Z"},
		{"the cycle starts exactly at the anchor time", anchor("2025-12-10T00:00:00Z"), "2026-03-10T00:00:00Z", "2026-03-10T00:00:00Z", "2026-04-10T00:00:00Z"},
		{"an anchor later than now in its own month", anchor("2026-05-20T00:00:00Z"), "2026-01-05T00:00:00Z", "2025-12-20T00:00:00Z", "2026-01-20T00:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MonthlyUsageCycle(tc.anchor, at(tc.now))
			if !got.Start.Equal(at(tc.start)) || !got.End.Equal(at(tc.end)) {
				t.Fatalf("got [%s, %s), want [%s, %s)", got.Start, got.End, tc.start, tc.end)
			}
		})
	}
}
