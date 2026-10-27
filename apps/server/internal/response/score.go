package response

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveylogic"
)

// The server's own total score (NF-04, decision D3). For a survey whose block logic has no calculate action, the score is a function of the answers alone, so the client API recomputes it from the response's data on every create and update, finished or partial, and stores that instead of the number the browser sent, unless it is out of the range ScoreBounds allows, which only answers the runtime never sends can reach and which drops it. A survey with a calculate action keeps the client's value within the range ScoreBounds allows (score_bounds.go): reproducing calculations would need a second calculation engine.
//
// The recomputation is the runtime's, step for step: packages/types/surveys/scoring.ts scores the answers and packages/surveys/src/components/general/survey.tsx (evaluateLogicAndGetNextBlockId) walks the blocks. testdata/score_parity.json is the table both are held to; packages/surveys/src/lib/score.parity.test.ts runs the runtime's functions over the same cases.

// RecomputesScore says whether the server recomputes the survey's total score (RecomputeScore): the survey has a total score variable and no block logic calculate action, on any variable. A calculate action on another variable counts too, since a condition on that variable can change which blocks a respondent is routed through.
func (sv *Survey) RecomputesScore() bool {
	if surveydoc.ScoreVariable(sv.variables) == nil {
		return false
	}
	for _, b := range sv.blocks {
		for _, l := range b.Logic {
			for _, a := range l.Actions {
				if a.Objective == "calculate" {
					return false
				}
			}
		}
	}
	return true
}

// ScoreInput is what RecomputeScore reads of a response: its merged data and ttc, the language as quotas read it ("default" for the survey's default language) and the reserved values its logic conditions read: those the runtime reads mid-survey (embeddeddata.ClientValues), as recomputedScore passes them.
type ScoreInput struct {
	Data     map[string]any
	TTC      map[string]float64
	Language string
	Reserved map[string]any
}

// RecomputeScore replays the response through the survey the way the runtime walked it and returns the total score the runtime held after the last block, with the path of block ids it walked; ok is false when the survey does not recompute its score (RecomputesScore).
//
// The variables start as the runtime seeds them (seedVariables). The walk starts at the first block, in document order, that the response holds an answer or a time to complete for: the runtime starts at the first block unless the link carried startAt, which opens a later block and never shows the ones before it, and a block shown and submitted always leaves a time to complete for its elements. At each block the score is raised by what the block's answers earn (blockScore), then the block's logic is evaluated over the data and the variables with the score already raised: the first jump of the first rule whose conditions hold, the block's fallback when none does, and otherwise the next block. A jump to an ending, to an id that is no block, or past the last block ends the walk, and so does a block reached twice, which surveydoc refuses to save but a stored document could hold.
//
// Walking the logic rather than adding up every answer matters for a respondent who went back and took another branch: the answers of the block they left stay in the response's data, but the runtime restored the score from before that block and never counted them. The walk does not stop at the block a partial response is waiting on, since the server does not know which one it is; its unanswered blocks earn nothing, so the total is the runtime's unless a block further down the path already holds an answer from an earlier visit, which the runtime only counts once the respondent submits it again.
func (sv *Survey) RecomputeScore(in ScoreInput) (variableID string, score float64, path []string, ok bool) {
	if !sv.RecomputesScore() {
		return "", 0, nil, false
	}
	variable := surveydoc.ScoreVariable(sv.variables)
	var declared float64
	_ = json.Unmarshal(variable.Value, &declared)
	variables := sv.seedVariables()
	engine := surveylogic.NewSurvey(sv.blocks, sv.Fields)
	input := surveylogic.Input{Data: in.Data, Variables: variables, Language: in.Language, Reserved: in.Reserved}

	index := map[string]int{}
	for i, b := range sv.blocks {
		index[b.ID] = i
	}
	visited := map[int]bool{}
	for i := sv.firstSubmittedBlock(in); i >= 0 && i < len(sv.blocks) && !visited[i]; {
		visited[i] = true
		b := sv.blocks[i]
		path = append(path, b.ID)
		if earned, scored := blockScore(b, in.Data); scored {
			base := declared
			if before, isNumber := variables[variable.ID].(float64); isNumber && isFinite(before) {
				base = before
			}
			variables[variable.ID] = roundScore(base + earned)
		}
		next := ""
		for _, l := range b.Logic {
			if next != "" {
				break
			}
			if !surveylogic.Evaluate(engine, input, l.Conditions) {
				continue
			}
			for _, a := range l.Actions {
				if a.Objective == "jumpToBlock" && a.Target != "" {
					next = a.Target
					break
				}
			}
		}
		if next == "" {
			next = b.LogicFallback
		}
		if next == "" {
			i++
			continue
		}
		j, isBlock := index[next]
		if !isBlock {
			break
		}
		i = j
	}
	score = declared
	if v, isNumber := variables[variable.ID].(float64); isNumber && isFinite(v) {
		score = v
	}
	return variable.ID, score, path, true
}

// seedVariables are the variables the runtime starts a response with: every computed Embedded Data field's default, coerced to the field's type, kept when it is a string or a number (survey.tsx, the effect that seeds currentVariables). The total score starts from its field's default when that is a number and from the variable's declared value otherwise (applyBlockScore's base).
func (sv *Survey) seedVariables() map[string]any {
	out := map[string]any{}
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceComputed) {
		v, ok := embeddeddata.Coerce(f.DefaultValue, f.DataType)
		if !ok {
			continue
		}
		switch v.(type) {
		case string, float64:
			out[f.StorageKey] = v
		}
	}
	return out
}

// firstSubmittedBlock is the index of the first block holding an answer or a time to complete for one of its elements, -1 when there is none.
func (sv *Survey) firstSubmittedBlock(in ScoreInput) int {
	for i, b := range sv.blocks {
		for _, e := range b.Elements {
			if _, answered := in.Data[e.ID]; answered {
				return i
			}
			if _, timed := in.TTC[e.ID]; timed {
				return i
			}
		}
	}
	return -1
}

// blockScore is what a block's answers add to the total score, applyBlockScore's earned: the sum, in element order, of every scored element's elementScore; scored is false when no element of the block earned a score (none is scored, or none was answered), and the runtime then leaves the total as it is, unrounded.
func blockScore(b surveydoc.Block, data map[string]any) (earned float64, scored bool) {
	for _, e := range b.Elements {
		if s, ok := elementScore(e, data[e.ID]); ok {
			earned += s
			scored = true
		}
	}
	return earned, scored
}

// elementScore is getElementScore: the points an answer to a scored element earns, the sum of the scores of the choices it picked, each choice counted once in the order first picked, a choice without a score counting 0; ok is false when the element is not scored or the answer holds no non-empty string. An answer that names no choice still earns 0 with ok true, as the runtime counts it. Single and multiple choice answers are labels, matched against a choice's label in any language, with the "other" choice taking any text no label matches; picture selection answers are choice ids.
func elementScore(e surveydoc.Element, answer any) (float64, bool) {
	if !isScoredElement(e) {
		return 0, false
	}
	var values []string
	switch a := answer.(type) {
	case string:
		if a != "" {
			values = append(values, a)
		}
	case []any:
		for _, v := range a {
			if s, isString := v.(string); isString && s != "" {
				values = append(values, s)
			}
		}
	}
	if len(values) == 0 {
		return 0, false
	}
	var picked []*surveydoc.Choice
	for _, v := range values {
		var choice *surveydoc.Choice
		if e.Type == surveydoc.ElementPictureSelection {
			choice = findChoice(e.Choices, func(c surveydoc.Choice) bool { return c.ID == v })
		} else {
			choice = answeredChoice(e.Choices, v)
		}
		if choice != nil && !slices.ContainsFunc(picked, func(p *surveydoc.Choice) bool { return p.ID == choice.ID }) {
			picked = append(picked, choice)
		}
	}
	total := 0.0
	for _, c := range picked {
		if c.Score != nil && isFinite(*c.Score) {
			total += *c.Score
		}
	}
	return total, true
}

// answeredChoice is findAnsweredChoice: the choice other than "other" one of whose labels is the answer, else the "other" choice, nil when the element has neither.
func answeredChoice(choices []surveydoc.Choice, answer string) *surveydoc.Choice {
	labelled := findChoice(choices, func(c surveydoc.Choice) bool {
		if c.ID == "other" {
			return false
		}
		for _, label := range c.Label {
			if label == answer {
				return true
			}
		}
		return false
	})
	if labelled != nil {
		return labelled
	}
	return findChoice(choices, func(c surveydoc.Choice) bool { return c.ID == "other" })
}

func findChoice(choices []surveydoc.Choice, match func(surveydoc.Choice) bool) *surveydoc.Choice {
	for i := range choices {
		if match(choices[i]) {
			return &choices[i]
		}
	}
	return nil
}

// roundScore is scoring.ts roundScore, Math.round(n * 1e6) / 1e6, with Math.round's halves towards positive infinity. A zero is written as 0, never -0, which JSON.stringify would print as 0 too.
func roundScore(n float64) float64 {
	scaled := float64(n * 1e6)
	r := math.Floor(scaled)
	if scaled-r >= 0.5 {
		r++
	}
	out := float64(r / 1e6)
	if out == 0 {
		return 0
	}
	return out
}

// recomputedScore is RecomputeScore over a response as a create or an update is about to store it: the language as quotas read it and, for the logic conditions, the reserved values the runtime can read mid-survey (embeddeddata.ClientValues), never the ones only the server knows (the geo-IP country, the parsed user agent, the response row's columns), which the runtime reads as unset.
func (sv *Survey) recomputedScore(r embeddeddata.ValueResponse, ttc map[string]float64) (variableID string, score float64, ok bool) {
	language := "default"
	if r.Language != nil && *r.Language != "" && *r.Language != sv.DefaultLanguage {
		language = *r.Language
	}
	variableID, score, _, ok = sv.RecomputeScore(ScoreInput{
		Data: r.Data, TTC: ttc, Language: language,
		Reserved: embeddeddata.ClientValues(r, sv.Fields, sv.ElementIDs()),
	})
	return variableID, score, ok
}

// recomputeScore writes the survey's recomputed total score (recomputedScore) into variables, the variables about to be stored, for a survey that recomputes it; sent are the variables the client sent with this create or update. A sent score the answers do not add up to is logged with the value (a bounded description of it, ScoreValueAttrs), so a forged submission, a survey edited while a respondent answered it, or a runtime that drifted from this replay can be found. The range check (ScoreBounds) stays behind the recomputation: answers the runtime never sends, such as several labels for a single choice element, which answer validation takes as the runtime's validateChoiceMembership does, add up to a total no respondent can reach, and that total is removed from the variables written and logged instead of stored. responseID is nil on a create.
func (s *Service) recomputeScore(ctx context.Context, sv *Survey, responseID *uuid.UUID, r embeddeddata.ValueResponse, ttc map[string]float64, variables, sent map[string]any) {
	id, score, ok := sv.recomputedScore(r, ttc)
	if !ok {
		return
	}
	// Read before the write: on a create, variables can be the very map the client sent.
	value, present := sent[id]
	if bounds, bounded := sv.ScoreBounds(); bounded && !bounds.Contains(score) {
		delete(variables, id)
		attrs := []any{"surveyId", sv.ID, "variableId", id, "recomputed", score, "min", bounds.Min, "max", bounds.Max}
		if present {
			attrs = append(attrs, ScoreValueAttrs(value)...)
		}
		if responseID != nil {
			attrs = append(attrs, "responseId", *responseID)
		}
		s.log.WarnContext(ctx, "dropped a recomputed total score the survey cannot produce", attrs...)
		return
	}
	variables[id] = score
	if !present || value == score {
		return
	}
	attrs := append([]any{"surveyId", sv.ID, "variableId", id}, ScoreValueAttrs(value)...)
	attrs = append(attrs, "recomputed", score)
	if responseID != nil {
		attrs = append(attrs, "responseId", *responseID)
	}
	s.log.WarnContext(ctx, "replaced a total score the answers do not add up to", attrs...)
}

// ScoreValueAttrs describes a total score a client sent for the log without copying an unbounded client value into it: a number is logged as is, a string by its length and anything else (a boolean, null, an array or an object) by its type only, since an anonymous request can send a value up to the body limit and every forged create or update would otherwise write all of it as one log line.
func ScoreValueAttrs(value any) []any {
	switch v := value.(type) {
	case float64:
		return []any{"value", v}
	case string:
		return []any{"valueType", "string", "valueLength", len(v)}
	case bool:
		return []any{"valueType", "boolean"}
	case nil:
		return []any{"valueType", "null"}
	case []any:
		return []any{"valueType", "array", "valueLength", len(v)}
	case map[string]any:
		return []any{"valueType", "object", "valueLength", len(v)}
	default:
		return []any{"valueType", fmt.Sprintf("%T", v)}
	}
}
