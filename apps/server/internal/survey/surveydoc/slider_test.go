package surveydoc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSliderScaleIssue(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		scale SliderScale
		field string
	}{
		{SliderScale{Min: 0, Max: 10, Step: 1}, ""},
		{SliderScale{Min: 0, Max: 1, Step: 0.1}, ""},
		{SliderScale{Min: -5, Max: 5, Step: 2.5}, ""},
		{SliderScale{Min: 0, Max: 1000, Step: 1}, ""},
		{SliderScale{Min: 0, Max: 10, Step: 0}, "step"},
		{SliderScale{Min: 10, Max: 10, Step: 1}, "max"},
		{SliderScale{Min: 0, Max: 10, Step: 3}, "step"},
		{SliderScale{Min: 0, Max: 1001, Step: 1}, "step"},
	} {
		if field, _ := c.scale.Issue(); field != c.field {
			t.Errorf("%+v: issue in %q, want %q", c.scale, field, c.field)
		}
	}
}

func TestSliderScaleHoldsAndPositions(t *testing.T) {
	t.Parallel()
	s := SliderScale{Min: 0, Max: 1, Step: 0.1}
	tenth, fifth := 0.1, 0.2 // variables, so the sum is the float64 0.30000000000000004 rather than the exact constant 0.3
	for _, c := range []struct {
		v    float64
		want bool
	}{{0, true}, {0.3, true}, {tenth + fifth, true}, {1, true}, {0.35, false}, {1.1, false}, {-0.1, false}} {
		if got := s.Holds(c.v); got != c.want {
			t.Errorf("Holds(%v) = %v, want %v", c.v, got, c.want)
		}
	}
	if s.Value(3) != 0.3 || s.Position(0.31) != 3 || s.Position(7) != 10 || s.Position(-2) != 0 {
		t.Errorf("Value(3) = %v, Position(0.31) = %d", s.Value(3), s.Position(0.31))
	}
}

func TestComparableDateAnswers(t *testing.T) {
	t.Parallel()
	if l, r := ComparableDateAnswers("2026-03-01T09:30", "2026-03-01"); l != "2026-03-01" || r != "2026-03-01" {
		t.Errorf("mixed: %q %q", l, r)
	}
	if l, r := ComparableDateAnswers("2026-03-01T09:30", "2026-03-01T10:00"); l != "2026-03-01T09:30" || r != "2026-03-01T10:00" {
		t.Errorf("both with time: %q %q", l, r)
	}
}

func TestCheckValidatesASliderDocument(t *testing.T) {
	v := newValidator(t)
	d := validDocument()
	d.Blocks = json.RawMessage(`[{"id": "blocka", "name": "Block 1", "elements": [
		{"id": "s1", "type": "slider", "headline": {"default": "How many?"}, "required": true, "min": 0, "max": 100, "step": 5, "lowerLabel": {"default": "None"}}
	], "logic": [{"id": "l1", "conditions": {"id": "g1", "connector": "and", "conditions": [
		{"id": "c1", "leftOperand": {"type": "element", "value": "s1"}, "operator": "isGreaterThan", "rightOperand": {"type": "static", "value": 50}}
	]}, "actions": [{"id": "a1", "objective": "requireAnswer", "target": "d1"}]}]},
	{"id": "blockb", "name": "Block 2", "elements": [
		{"id": "d1", "type": "date", "headline": {"default": "When?"}, "required": false, "format": "y-M-d", "includeTime": true}
	]}]`)
	is, s, err := v.Check(d, nil, nil)
	if err != nil || len(is) > 0 || s == nil {
		t.Fatalf("Check = %v, %v, %v", is, s, err)
	}
	slider, date := s.Blocks[0].Elements[0], s.Blocks[1].Elements[0]
	if slider.Min == nil || *slider.Max != 100 || *slider.Step != 5 || date.IncludeTime == nil || !*date.IncludeTime {
		t.Fatalf("decoded %+v / %+v", slider, date)
	}

	d.Blocks = json.RawMessage(`[{"id": "blocka", "name": "Block 1", "elements": [
		{"id": "s1", "type": "slider", "headline": {"default": "How many?"}, "required": true, "min": 0, "max": 100, "step": 7}
	], "logic": [{"id": "l1", "conditions": {"id": "g1", "connector": "and", "conditions": [
		{"id": "c1", "leftOperand": {"type": "element", "value": "s1"}, "operator": "isGreaterThan", "rightOperand": {"type": "static", "value": 150}}
	]}, "actions": [{"id": "a1", "objective": "jumpToBlock", "target": "blocka"}]}]}]`)
	is, _, err = v.Check(d, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	for _, i := range is {
		messages = append(messages, i.Name()+": "+i.Message)
	}
	want := map[string]bool{
		"blocks.0.elements.0.step: The step must divide the range from the minimum to the maximum evenly": false,
	}
	for _, m := range messages {
		if _, ok := want[m]; ok {
			want[m] = true
		}
	}
	for m, seen := range want {
		if !seen {
			t.Errorf("missing %q among %q", m, messages)
		}
	}
	found := false
	for _, m := range messages {
		if strings.Contains(m, "Slider value should be between 0 and 100") {
			found = true
		}
	}
	if !found {
		t.Errorf("no slider range issue among %q", messages)
	}
}
