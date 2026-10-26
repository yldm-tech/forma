package analysis

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// The weekly summary's two weeks are split at a local midnight: the first fixture response, 23:30 in Tokyo on 1 March, belongs to the week before, the next, 00:30 on 2 March, to the week.
func TestComparePeriodsSplitsAtTheLocalMidnight(t *testing.T) {
	e, _, _, _, sv := trendFixture(t, ptr("Asia/Tokyo"))
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	monday := func(day int) time.Time { return time.Date(2026, 3, day, 0, 0, 0, 0, tokyo) }
	current, previous, err := ComparePeriods(t.Context(), e.db, sv, tokyo, monday(-5), monday(2), monday(9))
	if err != nil {
		t.Fatal(err)
	}
	if previous.Responses != 1 || previous.Completed != 1 || current.Responses != 3 || current.Completed != 2 {
		t.Fatalf("previous %+v, current %+v", previous, current)
	}
	// The week before: one promoter, one satisfied answer. The week: a detractor and a promoter, and one of two numeric CSAT answers satisfied ("n/a" is not a number).
	for _, c := range []struct {
		stats    *PeriodStats
		element  string
		answered int
		value    float64
	}{
		{previous, "nps", 1, 100}, {previous, "csat", 1, 100}, {previous, "rating", 1, 4},
		{current, "nps", 2, 0}, {current, "csat", 2, 50}, {current, "rating", 1, 2},
	} {
		s := c.stats.Score(c.element)
		if s == nil || s.Answered != c.answered || s.Value == nil || *s.Value != c.value {
			t.Errorf("%s: %+v, want %d answered, %v", c.element, s, c.answered, c.value)
		}
	}
	if s := current.Score("nps"); s.Headline != "Recommend?" || s.Metric != TrendMetricNPS {
		t.Errorf("nps %+v", s)
	}
	if current.Score("ces") != nil {
		t.Error("a type the survey has no element of has no score")
	}

	// A week nobody answered in has the elements with no value.
	current, _, err = ComparePeriods(t.Context(), e.db, sv, tokyo, monday(9), monday(16), monday(23))
	if err != nil || current.Responses != 0 || current.Score("nps") == nil || current.Score("nps").Value != nil {
		t.Errorf("empty week %+v, %v", current, err)
	}
	// A survey that does not exist has no periods.
	if current, previous, err := ComparePeriods(t.Context(), e.db, uuid.New(), tokyo, monday(-5), monday(2), monday(9)); err != nil || current != nil || previous != nil {
		t.Errorf("missing survey %v %v %v", current, previous, err)
	}
}
