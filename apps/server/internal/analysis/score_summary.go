package analysis

import (
	"math"
	"slices"
)

// ScoreBin is one bar of the total score's spread: the responses whose score is from From to To.
type ScoreBin struct {
	From       float64 `json:"from"`
	To         float64 `json:"to"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// ScoreSummary is the total score over the completed responses that carry one (ZSurveyScoreSummary): the value of the survey's total score variable in each response, its mean, median and extremes, and its spread.
type ScoreSummary struct {
	VariableID    string     `json:"variableId"`
	Name          string     `json:"name"`
	ResponseCount int        `json:"responseCount"`
	Average       float64    `json:"average"`
	Median        float64    `json:"median"`
	Min           float64    `json:"min"`
	Max           float64    `json:"max"`
	Bins          []ScoreBin `json:"bins"`
}

// scoreBinLimit is the most distinct scores that get a bin each; more are grouped into scoreBinCount bins of equal width.
const (
	scoreBinLimit = 11
	scoreBinCount = 10
)

// scoreAggregate collects the total score of every completed response that has one. The runtime writes the score into the response's variables after every block, so a response the respondent has not finished holds only the points of the blocks answered so far; counting it would pull the average and the lowest score down as if it were final, so it is left out. A response the score variable did not exist for yet, or one created through the API without it, has none and is left out too.
type scoreAggregate struct {
	variableID, name string
	values           []float64
}

func (g *scoreAggregate) add(r *summaryResponse) {
	if !r.Finished {
		return
	}
	if v, ok := r.Variables[g.variableID].(float64); ok && !math.IsNaN(v) && !math.IsInf(v, 0) {
		g.values = append(g.values, v)
	}
}

func (g *scoreAggregate) result() *ScoreSummary {
	out := &ScoreSummary{VariableID: g.variableID, Name: g.name, ResponseCount: len(g.values), Bins: []ScoreBin{}}
	total := len(g.values)
	if total == 0 {
		return out
	}
	sorted := slices.Clone(g.values)
	slices.Sort(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	median := sorted[total/2]
	if total%2 == 0 {
		median = (sorted[total/2-1] + sorted[total/2]) / 2
	}
	out.Average, out.Median, out.Min, out.Max = round2(sum/float64(total)), round2(median), sorted[0], sorted[total-1]

	distinct := slices.Compact(slices.Clone(sorted))
	if len(distinct) <= scoreBinLimit {
		for _, v := range distinct {
			count := 0
			for _, s := range sorted {
				if s == v {
					count++
				}
			}
			out.Bins = append(out.Bins, ScoreBin{From: v, To: v, Count: count, Percentage: percent(float64(count), float64(total))})
		}
		return out
	}
	width := (out.Max - out.Min) / scoreBinCount
	counts := make([]int, scoreBinCount)
	for _, v := range sorted {
		i := int((v - out.Min) / width)
		counts[min(i, scoreBinCount-1)]++
	}
	for i, count := range counts {
		from, to := out.Min+float64(i)*width, out.Min+float64(i+1)*width
		if i == scoreBinCount-1 {
			to = out.Max
		}
		out.Bins = append(out.Bins, ScoreBin{From: round2(from), To: round2(to), Count: count, Percentage: percent(float64(count), float64(total))})
	}
	return out
}
