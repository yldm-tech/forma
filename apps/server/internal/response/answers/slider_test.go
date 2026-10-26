package answers

import "testing"

func slider(extra string) string {
	return `{"id":"q","type":"slider","headline":{"default":"Q"},"required":false,"min":0,"max":1,"step":0.1` + extra + `}`
}

func TestSliderAnswerIsAPositionOfTheScale(t *testing.T) {
	t.Parallel()
	for _, data := range []string{`{"q":0}`, `{"q":0.3}`, `{"q":0.7000000000000001}`, `{"q":1}`} {
		expectValid(t, slider(""), data)
	}
	for _, data := range []string{`{"q":1.1}`, `{"q":-0.1}`, `{"q":0.35}`, `{"q":"0.5"}`, `{"q":[0.5]}`} {
		expectMessages(t, slider(""), data, invalid)
	}
	// An empty answer is the required check's: allowed on an optional slider, refused on a required one.
	expectValid(t, slider(""), `{"q":""}`)
	expectMessages(t, slider(`,"required":true`), `{"q":""}`, required)
}

func TestDateWithTimeTakesADateAndATime(t *testing.T) {
	t.Parallel()
	el := `{"id":"q","type":"date","headline":{"default":"Q"},"required":false,"format":"y-M-d","includeTime":true}`
	expectValid(t, el, `{"q":"2026-10-21T09:30"}`)
	expectValid(t, el, `{"q":""}`)
	expectMessages(t, el, `{"q":"2026-10-21"}`, invalid)
	expectMessages(t, el, `{"q":"2026-10-21T9:30"}`, invalid)
	// Without includeTime a day alone is the answer.
	expectValid(t, `{"id":"q","type":"date","headline":{"default":"Q"},"required":false,"format":"y-M-d"}`, `{"q":"2026-10-21"}`)
}

func TestDateRulesHoldADateAndTimeToItsDay(t *testing.T) {
	t.Parallel()
	el := func(rule string) string {
		return `{"id":"q","type":"date","headline":{"default":"Q"},"required":false,"format":"y-M-d","includeTime":true` + withRules("", rule) + `}`
	}
	later := el(`{"id":"r","type":"isLaterThan","params":{"date":"2026-10-21"}}`)
	// The bound is exclusive and names a day: any time on that day is not later than it.
	expectMessages(t, later, `{"q":"2026-10-21T23:59"}`, "Please select a date later than 2026-10-21")
	expectValid(t, later, `{"q":"2026-10-22T00:00"}`)
	earlier := el(`{"id":"r","type":"isEarlierThan","params":{"date":"2026-10-21"}}`)
	expectMessages(t, earlier, `{"q":"2026-10-21T00:00"}`, "Please select a date earlier than 2026-10-21")
	between := el(`{"id":"r","type":"isBetween","params":{"startDate":"2026-10-20","endDate":"2026-10-22"}}`)
	expectValid(t, between, `{"q":"2026-10-21T12:00"}`)
}
