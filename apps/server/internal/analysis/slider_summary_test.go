package analysis

import (
	"reflect"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

func sliderElement(minimum, maximum, step float64) *element {
	return &element{Element: surveydoc.Element{ID: "s", Type: surveydoc.ElementSlider, Min: &minimum, Max: &maximum, Step: &step}}
}

func sliderSummary(t *testing.T, el *element, answers ...any) SliderSummary {
	t.Helper()
	g := &sliderAggregate{el: el}
	g.scale, _ = el.SliderScale()
	for _, a := range answers {
		r := &summaryResponse{Data: map[string]any{}, TTC: map[string]any{"s": 1200.0}}
		if a != nil {
			r.Data["s"] = a
		}
		g.add(r)
	}
	out, ok := g.result(nil)
	if !ok {
		t.Fatal("no entry")
	}
	return out.(SliderSummary)
}

func TestSliderSummaryShortScaleHasABinPerPosition(t *testing.T) {
	t.Parallel()
	got := sliderSummary(t, sliderElement(0, 1, 0.25), 0.0, 0.25, 0.25, 1.0, nil)
	want := []SliderBin{
		{From: 0, To: 0, Count: 1, Percentage: 25},
		{From: 0.25, To: 0.25, Count: 2, Percentage: 50},
		{From: 0.5, To: 0.5},
		{From: 0.75, To: 0.75},
		{From: 1, To: 1, Count: 1, Percentage: 25},
	}
	if !reflect.DeepEqual(got.Bins, want) {
		t.Fatalf("bins %+v, want %+v", got.Bins, want)
	}
	if got.ResponseCount != 4 || got.Average != 0.38 || got.Median != 0.25 || got.Min != 0 || got.Max != 1 || got.Dismissed.Count != 1 {
		t.Fatalf("stats %+v", got)
	}
}

func TestSliderSummaryLongScaleHasTenBinsOfWholeSteps(t *testing.T) {
	t.Parallel()
	// 0 to 100 in steps of 1 is 101 positions: ten bins, the last one position wider.
	got := sliderSummary(t, sliderElement(0, 100, 1), 0.0, 9.0, 10.0, 55.0, 100.0, 120.0)
	if len(got.Bins) != 10 {
		t.Fatalf("%d bins", len(got.Bins))
	}
	if b := got.Bins[0]; b.From != 0 || b.To != 9 || b.Count != 2 {
		t.Fatalf("first bin %+v", b)
	}
	if b := got.Bins[1]; b.From != 10 || b.To != 19 || b.Count != 1 {
		t.Fatalf("second bin %+v", b)
	}
	// An answer off the scale (the scale changed after it was given) counts in the bin of the nearest position.
	if b := got.Bins[9]; b.From != 90 || b.To != 100 || b.Count != 2 {
		t.Fatalf("last bin %+v", b)
	}
	if got.Median != 32.5 || got.Max != 120 {
		t.Fatalf("stats %+v", got)
	}
}

func TestSliderSummaryWithoutAnswers(t *testing.T) {
	t.Parallel()
	got := sliderSummary(t, sliderElement(1, 5, 1))
	if got.ResponseCount != 0 || got.Average != 0 || len(got.Bins) != 5 || got.Bins[0].Percentage != 0 {
		t.Fatalf("%+v", got)
	}
}
