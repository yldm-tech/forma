package surveydoc

import (
	"fmt"
	"math"
)

// SliderMaxSteps is the most steps a slider may have, packages/types/surveys/slider.ts's SLIDER_MAX_STEPS.
const SliderMaxSteps = 1000

// sliderStepTolerance is how far a computed step count may stray from a whole number and still count as one: binary fractions make 0.3 / 0.1 come out as 2.9999999999999996.
const sliderStepTolerance = 1e-9

func isWholeSteps(n float64) bool {
	return math.Abs(n-math.Round(n)) <= sliderStepTolerance*math.Max(1, math.Abs(n))
}

// SliderScale is a slider element's min, max and step, read once the document's structure is known to hold them.
type SliderScale struct {
	Min, Max, Step float64
}

// SliderScale is the element's slider scale; ok is false when the document lacks one of its numbers, which the JSON Schema already reports.
func (e *Element) SliderScale() (SliderScale, bool) {
	if e.Min == nil || e.Max == nil || e.Step == nil {
		return SliderScale{}, false
	}
	return SliderScale{Min: *e.Min, Max: *e.Max, Step: *e.Step}, true
}

// Steps is the number of steps from min to max, a whole number for a valid scale.
func (s SliderScale) Steps() float64 { return (s.Max - s.Min) / s.Step }

// Issue is getSliderScaleIssue: the first thing wrong with the scale and the field it is wrong in, or "" for a valid scale.
func (s SliderScale) Issue() (field, message string) {
	for _, n := range []float64{s.Min, s.Max, s.Step} {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return "min", "The minimum, maximum and step must be numbers"
		}
	}
	if s.Step <= 0 {
		return "step", "The step must be greater than 0"
	}
	if s.Max <= s.Min {
		return "max", "The maximum must be greater than the minimum"
	}
	steps := s.Steps()
	if !isWholeSteps(steps) {
		return "step", "The step must divide the range from the minimum to the maximum evenly"
	}
	if math.Round(steps) > SliderMaxSteps {
		return "step", fmt.Sprintf("A slider can have at most %d steps", SliderMaxSteps)
	}
	return "", ""
}

// Holds is isSliderValueOnScale: whether v is a position of the scale, from min to max and a whole number of steps from min.
func (s SliderScale) Holds(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	tolerance := sliderStepTolerance * math.Max(1, math.Abs(s.Max-s.Min))
	if v < s.Min-tolerance || v > s.Max+tolerance {
		return false
	}
	return isWholeSteps((v - s.Min) / s.Step)
}

// Position is the index of the scale position nearest v, clamped to the scale: 0 for min, Steps() for max.
func (s SliderScale) Position(v float64) int {
	last := int(math.Round(s.Steps()))
	p := int(math.Round((v - s.Min) / s.Step))
	return min(max(p, 0), last)
}

// Value is the number at a position of the scale, rounded to the decimals of the step and the minimum so 0.1 + 0.2 reads 0.3.
func (s SliderScale) Value(position int) float64 {
	v := s.Min + float64(position)*s.Step
	scale := math.Pow(10, float64(min(10, max(decimals(s.Step), decimals(s.Min)))))
	return math.Round(v*scale) / scale
}

// decimals is the number of decimal places a number is written with, as JavaScript's String(n) writes it.
func decimals(n float64) int {
	for d := 0; d < 10; d++ {
		p := math.Pow(10, float64(d))
		if math.Abs(n*p-math.Round(n*p)) <= sliderStepTolerance*math.Max(1, math.Abs(n*p)) {
			return d
		}
	}
	return 10
}
