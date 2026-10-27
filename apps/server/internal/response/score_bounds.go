package response

import (
	"encoding/json"
	"math"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// The total score a respondent's browser sends is kept only when the survey can produce it. The runtime starts the score variable at its declared value and raises it by the score of every choice picked in a block when the block is submitted (packages/types/surveys/scoring.ts applyBlockScore); the server does not recompute it, so a public POST could set any number and the admin's score summary would average it in (NF-04). This file is the minimal guard: the range of totals the survey's static choice scores can reach, checked on every client create and update.

// scoreTolerance absorbs the rounding the runtime applies after every block (roundScore, six decimals), so a total of many fractional scores is not refused for a rounding difference.
const scoreTolerance = 1e-6

// ScoreBounds is the range of totals a survey's score variable can reach: [Min, Max] around every value the runtime can hold at any point of a response.
type ScoreBounds struct {
	VariableID string
	Min, Max   float64
}

// ScoreBounds is the range the survey's total score can reach, with ok false when the survey has no total score variable or a block logic calculate action writes it, whose result the static choice scores do not bound (that case keeps the client's value).
//
// The range starts at the variable's seed: its declared value, and the default of the computed Embedded Data field linked to it when that differs (the runtime seeds from the field), both taken so neither seed is refused. Each scored element then widens it by what its answer can add, 0 included because an element can be skipped by logic, left unanswered or not yet reached: a single choice element adds one choice's score, a multiple choice or picture selection element any subset of its choices, so its negative scores together at the low end and its positive scores at the high end. Picture selection is read as multiple choice whether or not it allows several pictures, since surveydoc.Element does not read allowMulti and the wider range can only keep more. Blocks cannot loop (surveydoc refuses cyclic logic) and going back restores the score from before the block, so no block adds its points twice.
func (sv *Survey) ScoreBounds() (ScoreBounds, bool) {
	variable := surveydoc.ScoreVariable(sv.variables)
	if variable == nil {
		return ScoreBounds{}, false
	}
	for _, b := range sv.blocks {
		for _, l := range b.Logic {
			for _, a := range l.Actions {
				if a.Objective == "calculate" && a.VariableID == variable.ID {
					return ScoreBounds{}, false
				}
			}
		}
	}
	seeds := []float64{}
	var declared float64
	if json.Unmarshal(variable.Value, &declared) == nil && isFinite(declared) {
		seeds = append(seeds, declared)
	}
	if f := embeddeddata.FindComputed(sv.Fields, variable.ID); f != nil && f.Source == embeddeddata.SourceComputed {
		if v, ok := embeddeddata.Coerce(f.DefaultValue, embeddeddata.TypeNumber); ok {
			seeds = append(seeds, v.(float64))
		}
	}
	if len(seeds) == 0 {
		seeds = append(seeds, 0)
	}
	out := ScoreBounds{VariableID: variable.ID, Min: seeds[0], Max: seeds[0]}
	for _, s := range seeds[1:] {
		out.Min, out.Max = min(out.Min, s), max(out.Max, s)
	}
	for _, e := range sv.Elements() {
		lo, hi := elementScoreRange(e)
		out.Min += lo
		out.Max += hi
	}
	return out, true
}

// elementScoreRange is what an answer to the element can add to the total score, 0 to 0 for an element that is not scored (packages/types/surveys/scoring.ts isScoredElement).
func elementScoreRange(e surveydoc.Element) (lo, hi float64) {
	var single bool
	switch e.Type {
	case surveydoc.ElementMultipleChoiceSingle:
		single = true
	case surveydoc.ElementMultipleChoiceMulti, surveydoc.ElementPictureSelection:
	default:
		return 0, 0
	}
	for _, c := range e.Choices {
		// A choice without a finite score counts 0, as choiceScore reads it.
		if c.Score == nil || !isFinite(*c.Score) {
			continue
		}
		s := *c.Score
		if single {
			lo, hi = min(lo, s), max(hi, s)
		} else {
			lo, hi = lo+min(s, 0), hi+max(s, 0)
		}
	}
	return lo, hi
}

// Contains says whether the runtime could have produced v: a finite number within the bounds, give or take the runtime's rounding. Anything else (a string, a number out of range) could only have been written by hand.
func (b ScoreBounds) Contains(v any) bool {
	n, ok := v.(float64)
	if !ok || !isFinite(n) {
		return false
	}
	tolerance := scoreTolerance * max(1, math.Abs(b.Min), math.Abs(b.Max))
	return n >= b.Min-tolerance && n <= b.Max+tolerance
}

// DropUnreachableScore removes the survey's total score from variables when it holds a value the survey cannot produce, and returns the bounds it was checked against and the value removed; dropped is false when variables carry no score or one in range, and for a survey whose score is not bounded (ScoreBounds). Only the score is removed: the rest of the response is kept, so a respondent whose total a survey edit pushed out of range still has their answers recorded.
func (sv *Survey) DropUnreachableScore(variables map[string]any) (bounds ScoreBounds, value any, dropped bool) {
	bounds, ok := sv.ScoreBounds()
	if !ok {
		return ScoreBounds{}, nil, false
	}
	value, present := variables[bounds.VariableID]
	if !present || bounds.Contains(value) {
		return bounds, nil, false
	}
	delete(variables, bounds.VariableID)
	return bounds, value, true
}

func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
