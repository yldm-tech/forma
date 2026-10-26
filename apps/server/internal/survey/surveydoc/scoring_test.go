package surveydoc

import (
	"encoding/json"
	"testing"
)

func TestScoreVariableIsUnique(t *testing.T) {
	variables := func(second string) string {
		return `{"variables": [{"id": "a", "name": "score", "type": "number", "value": 0, "isScore": true}, {"id": "b", "name": "bonus", "type": "number", "value": 0` + second + `}, {"id": "c", "name": "note", "type": "text", "value": "", "isScore": true}]}`
	}
	if hasMessage(FieldIssues(load(t, variables(""))), ScoreVariableUniqueMessage) != nil {
		t.Fatal("one number variable marked isScore (a text variable cannot hold the score) was refused")
	}
	expectIssue(t, FieldIssues(load(t, variables(`, "isScore": true`))), ScoreVariableUniqueMessage, "variables")
}

func TestScoreVariable(t *testing.T) {
	s := load(t, `{"variables": [{"id": "t", "name": "note", "type": "text", "isScore": true}, {"id": "a", "name": "bonus", "type": "number"}, {"id": "b", "name": "score", "type": "number", "isScore": true}]}`)
	if v := ScoreVariable(s.Variables); v == nil || v.ID != "b" {
		t.Fatalf("ScoreVariable = %+v, want the number variable b", v)
	}
	if v := ScoreVariable(s.Variables[:2]); v != nil {
		t.Fatalf("ScoreVariable without a marked number variable = %+v, want nil", v)
	}
}

func TestChoiceScoresAreReadWhereScoringReadsThem(t *testing.T) {
	var elements []Element
	err := json.Unmarshal([]byte(`[
		{"id": "single", "type": "multipleChoiceSingle", "choices": [{"id": "a", "label": {"default": "A"}, "score": 2}]},
		{"id": "multi", "type": "multipleChoiceMulti", "choices": [{"id": "a", "label": {"default": "A"}, "score": -1.5}]},
		{"id": "picture", "type": "pictureSelection", "choices": [{"id": "a", "imageUrl": "/storage/x/public/a.png", "score": 3}]},
		{"id": "ranking", "type": "ranking", "choices": [{"id": "a", "label": {"default": "A"}, "score": 4}]}
	]`), &elements)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []*float64{new(2.0), new(-1.5), new(3.0), nil} {
		got := elements[i].Choices[0].Score
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("%s: score %v, want %v", elements[i].ID, got, want)
		}
	}
}
