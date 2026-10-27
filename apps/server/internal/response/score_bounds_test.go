package response

import (
	"encoding/json"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// scoredSurvey is a survey with the blocks and variables given as stored JSON.
func scoredSurvey(t *testing.T, blocks, variables string) *Survey {
	t.Helper()
	sv := &Survey{}
	if err := json.Unmarshal([]byte(blocks), &sv.blocks); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(variables), &sv.variables); err != nil {
		t.Fatal(err)
	}
	return sv
}

// Two blocks: a single choice worth -2 to 5 (an unscored choice counts 0), a multiple choice whose choices are worth 1, 2 and -1, and a picture selection worth 0.5 and 0.25; the score starts at 10.
const scoredBlocks = `[
	{"id":"b1","name":"One","elements":[
		{"id":"single","type":"multipleChoiceSingle","headline":{"default":"S"},"required":true,"choices":[
			{"id":"a","label":{"default":"A"},"score":5},{"id":"b","label":{"default":"B"},"score":-2},{"id":"c","label":{"default":"C"}}]},
		{"id":"open","type":"openText","headline":{"default":"O"},"required":false,"inputType":"text"}]},
	{"id":"b2","name":"Two","elements":[
		{"id":"multi","type":"multipleChoiceMulti","headline":{"default":"M"},"required":false,"choices":[
			{"id":"x","label":{"default":"X"},"score":1},{"id":"y","label":{"default":"Y"},"score":2},{"id":"z","label":{"default":"Z"},"score":-1}]},
		{"id":"pic","type":"pictureSelection","headline":{"default":"P"},"required":false,"allowMulti":false,"choices":[
			{"id":"p1","imageUrl":"/a.png","score":0.5},{"id":"p2","imageUrl":"/b.png","score":0.25}]}]}]`

const scoredVariables = `[{"id":"total","name":"score","type":"number","value":10,"isScore":true},{"id":"other","name":"other","type":"number","value":0}]`

func TestScoreBoundsSumTheStaticChoiceScores(t *testing.T) {
	sv := scoredSurvey(t, scoredBlocks, scoredVariables)
	b, ok := sv.ScoreBounds()
	if !ok {
		t.Fatal("no bounds")
	}
	// 10 + [-2, 5] + [-1, 3] + [0, 0.75]
	if b.VariableID != "total" || b.Min != 7 || b.Max != 18.75 {
		t.Fatalf("bounds %+v", b)
	}
}

func TestScoreBoundsKeepEveryValueTheRuntimeCanProduce(t *testing.T) {
	sv := scoredSurvey(t, scoredBlocks, scoredVariables)
	for _, v := range []float64{10, 7, 18.75, 15, 8, 18.75 + 1e-7, 0.1 + 0.2 + 10} {
		vars := map[string]any{"total": v, "other": 9999.0}
		if _, _, dropped := sv.DropUnreachableScore(vars); dropped || vars["total"] != v || vars["other"] != 9999.0 {
			t.Errorf("%v: dropped=%v vars=%v", v, dropped, vars)
		}
	}
	// No score sent: nothing to check.
	vars := map[string]any{"other": 1.0}
	if _, _, dropped := sv.DropUnreachableScore(vars); dropped || len(vars) != 1 {
		t.Errorf("no score: %v", vars)
	}
}

func TestScoreBoundsDropOnlyTheUnreachableScore(t *testing.T) {
	sv := scoredSurvey(t, scoredBlocks, scoredVariables)
	for _, v := range []any{9999.0, 18.8, 6.9, -9999.0, "12", true} {
		vars := map[string]any{"total": v, "other": 3.0}
		b, value, dropped := sv.DropUnreachableScore(vars)
		if !dropped || value != v || b.VariableID != "total" {
			t.Errorf("%v: dropped=%v value=%v bounds=%+v", v, dropped, value, b)
		}
		if _, still := vars["total"]; still || vars["other"] != 3.0 {
			t.Errorf("%v: vars %v", v, vars)
		}
	}
}

// A calculate action on the score makes the result depend on logic the static choice scores do not bound, so the score is not checked (D3 covers those surveys); a calculate action on another variable changes nothing.
func TestScoreBoundsLeaveScoresWrittenByLogicAlone(t *testing.T) {
	logic := func(variableID string) string {
		return `[{"id":"b1","name":"One","elements":[
			{"id":"single","type":"multipleChoiceSingle","headline":{"default":"S"},"required":true,"choices":[{"id":"a","label":{"default":"A"},"score":5}]}],
			"logic":[{"id":"l1","conditions":{"id":"c1","connector":"and","conditions":[]},"actions":[
				{"id":"a1","objective":"calculate","variableId":"` + variableID + `","operator":"multiply","value":{"type":"static","value":100}}]}]}]`
	}
	sv := scoredSurvey(t, logic("total"), scoredVariables)
	if _, ok := sv.ScoreBounds(); ok {
		t.Fatal("bounded a score that logic calculates")
	}
	vars := map[string]any{"total": 9999.0}
	if _, _, dropped := sv.DropUnreachableScore(vars); dropped || vars["total"] != 9999.0 {
		t.Fatalf("vars %v", vars)
	}

	sv = scoredSurvey(t, logic("other"), scoredVariables)
	if b, ok := sv.ScoreBounds(); !ok || b.Min != 10 || b.Max != 15 {
		t.Fatalf("bounds %+v %v", b, ok)
	}
}

func TestScoreBoundsWithoutAScoreVariable(t *testing.T) {
	sv := scoredSurvey(t, scoredBlocks, `[{"id":"total","name":"score","type":"number","value":10}]`)
	if _, ok := sv.ScoreBounds(); ok {
		t.Fatal("bounds without a score variable")
	}
	vars := map[string]any{"total": 9999.0}
	if _, _, dropped := sv.DropUnreachableScore(vars); dropped {
		t.Fatal("dropped a variable that is not the score")
	}
}

// The runtime seeds the score from the computed Embedded Data field linked to it, so both its default and the variable's declared value are within range.
func TestScoreBoundsTakeBothSeeds(t *testing.T) {
	sv := scoredSurvey(t, scoredBlocks, scoredVariables)
	sv.Fields = []embeddeddata.Linked{{Field: embeddeddata.Field{StorageKey: "total", Name: "score", Source: embeddeddata.SourceComputed, DataType: embeddeddata.TypeNumber, DefaultValue: 20.0}}}
	if b, _ := sv.ScoreBounds(); b.Min != 7 || b.Max != 28.75 {
		t.Fatalf("bounds %+v", b)
	}
}

func TestElementScoreRange(t *testing.T) {
	score := func(f float64) *float64 { return &f }
	cases := []struct {
		element surveydoc.Element
		lo, hi  float64
	}{
		{surveydoc.Element{Type: surveydoc.ElementMultipleChoiceSingle, Choices: []surveydoc.Choice{{Score: score(3)}, {Score: score(4)}}}, 0, 4},
		{surveydoc.Element{Type: surveydoc.ElementMultipleChoiceSingle, Choices: []surveydoc.Choice{{Score: score(-3)}, {Score: score(-4)}}}, -4, 0},
		{surveydoc.Element{Type: surveydoc.ElementMultipleChoiceMulti, Choices: []surveydoc.Choice{{Score: score(3)}, {Score: score(4)}, {Score: score(-1)}, {}}}, -1, 7},
		{surveydoc.Element{Type: surveydoc.ElementPictureSelection, Choices: []surveydoc.Choice{{Score: score(1)}, {Score: score(1)}}}, 0, 2},
		// Only choice elements are scored.
		{surveydoc.Element{Type: surveydoc.ElementRanking, Choices: []surveydoc.Choice{{Score: score(5)}}}, 0, 0},
	}
	for i, c := range cases {
		if lo, hi := elementScoreRange(c.element); lo != c.lo || hi != c.hi {
			t.Errorf("case %d: [%v, %v], want [%v, %v]", i, lo, hi, c.lo, c.hi)
		}
	}
}
