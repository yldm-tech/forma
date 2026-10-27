package response

import (
	"math"
	"testing"
)

// The server recomputes the score of a survey with a score variable and no calculate action; a calculate action on any variable leaves the survey to the range check (score_bounds.go).
func TestRecomputesScoreOnlyWithoutCalculateLogic(t *testing.T) {
	calculate := func(variableID string) string {
		return `[{"id":"b1","name":"One","elements":[
			{"id":"single","type":"multipleChoiceSingle","headline":{"default":"S"},"required":true,"choices":[{"id":"a","label":{"default":"A"},"score":5}]}],
			"logic":[{"id":"l1","conditions":{"id":"c1","connector":"and","conditions":[]},"actions":[
				{"id":"a1","objective":"calculate","variableId":"` + variableID + `","operator":"add","value":{"type":"static","value":1}}]}]}]`
	}
	if !scoredSurvey(t, scoredBlocks, scoredVariables).RecomputesScore() {
		t.Error("a survey without calculate logic is not recomputed")
	}
	for _, id := range []string{"total", "other"} {
		sv := scoredSurvey(t, calculate(id), scoredVariables)
		if sv.RecomputesScore() {
			t.Errorf("calculate on %s: recomputed", id)
		}
		if _, _, _, ok := sv.RecomputeScore(ScoreInput{Data: map[string]any{"single": "A"}}); ok {
			t.Errorf("calculate on %s: RecomputeScore ran", id)
		}
	}
	if scoredSurvey(t, scoredBlocks, `[{"id":"total","name":"score","type":"number","value":10}]`).RecomputesScore() {
		t.Error("a survey without a score variable is recomputed")
	}
}

// A response without any answer or time has walked no block and holds the score's start.
func TestRecomputeScoreWithoutAnswers(t *testing.T) {
	sv := scoredSurvey(t, scoredBlocks, scoredVariables)
	id, score, path, ok := sv.RecomputeScore(ScoreInput{Data: map[string]any{"hidden": "x"}})
	if !ok || id != "total" || score != 10 || len(path) != 0 {
		t.Fatalf("%q %v %v %v", id, score, path, ok)
	}
}

// surveydoc refuses cyclic logic, but a stored document is not re-checked: a block reached twice ends the walk instead of looping, and is counted once.
func TestRecomputeScoreStopsOnACycle(t *testing.T) {
	sv := scoredSurvey(t, `[
		{"id":"b1","name":"One","elements":[{"id":"single","type":"multipleChoiceSingle","headline":{"default":"S"},"required":true,"choices":[{"id":"a","label":{"default":"A"},"score":5}]}],"logicFallback":"b2"},
		{"id":"b2","name":"Two","elements":[{"id":"multi","type":"multipleChoiceMulti","headline":{"default":"M"},"required":false,"choices":[{"id":"x","label":{"default":"X"},"score":1}]}],"logicFallback":"b1"}]`, scoredVariables)
	_, score, path, ok := sv.RecomputeScore(ScoreInput{Data: map[string]any{"single": "A", "multi": []any{"X"}}})
	if !ok || score != 16 || len(path) != 2 {
		t.Fatalf("%v %v %v", score, path, ok)
	}
}

// roundScore is Math.round(n * 1e6) / 1e6, rounding towards positive infinity on negatives too, and a zero is never -0 (Math.round(-0.1) is -0, which JSON writes as 0).
func TestRoundScore(t *testing.T) {
	negativeZero := math.Copysign(0, -1)
	// Not a constant expression, which Go would add exactly.
	tenth := 0.1
	cases := []struct{ in, want float64 }{
		{tenth + 0.2, 0.3},
		{15.8, 15.8},
		{1.23456789, 1.234568},
		{-3.0000004, -3},
		{-3.0000006, -3.000001},
		{-1e-7, 0},
		{negativeZero, 0},
	}
	for _, c := range cases {
		got := roundScore(c.in)
		if got != c.want || math.Signbit(got) != math.Signbit(c.want) {
			t.Errorf("roundScore(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
