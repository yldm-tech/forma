package analysis

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// PeriodStats is a survey's responses created in one period: how many, how many of them are completed, and each NPS, CSAT, rating or CES element's metric over the numeric answers, computed as the trend computes a bucket's.
type PeriodStats struct {
	Responses int
	Completed int
	Elements  []PeriodScore
}

// PeriodScore is one score element's metric in a period; Value is nil when nobody answered it with a number.
type PeriodScore struct {
	ElementID string
	Type      string
	Headline  string
	Metric    string
	Answered  int
	Value     *float64
}

// Score is the period's score of the first element of the given type, nil when the survey has none.
func (p *PeriodStats) Score(elementType string) *PeriodScore {
	for i := range p.Elements {
		if p.Elements[i].Type == elementType {
			return &p.Elements[i]
		}
	}
	return nil
}

// ComparePeriods is the survey's PeriodStats over two consecutive periods, [previousStart, start) and [start, end), whose bounds are midnights in loc: the weekly summary's week and the week before it. It authorizes nothing; the caller has decided who may see the numbers. Both are nil when the survey does not exist.
func ComparePeriods(ctx context.Context, db *gorm.DB, surveyID uuid.UUID, loc *time.Location, previousStart, start, end time.Time) (current, previous *PeriodStats, err error) {
	sv, err := loadSurveyDoc(ctx, db, surveyID)
	if err != nil || sv == nil {
		return nil, nil, err
	}
	elements := trendElements(sv)
	ids := make([]string, len(elements))
	for i, el := range elements {
		ids[i] = el.ID
	}
	clause := response.Clause{SQL: "r.created_at >= ? AND r.created_at < ?", Args: []any{previousStart, end}}
	var counts []dayCounts
	var scores []dayScores
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		counts, scores, err = readTrendDays(ctx, tx, sv.ID, clause, loc.String(), ids)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	// The rows are local days; a day before the current period's first belongs to the previous period.
	boundary := localDate(start, loc)
	current, previous = periodStats(elements), periodStats(elements)
	pick := func(day time.Time) *PeriodStats {
		if dateOnly(day).Before(boundary) {
			return previous
		}
		return current
	}
	for _, c := range counts {
		p := pick(c.Day)
		p.Responses += c.Responses
		p.Completed += c.Completed
	}
	sums := map[*PeriodStats]map[string]*dayScores{current: {}, previous: {}}
	for _, s := range scores {
		bucket := sums[pick(s.Day)]
		if bucket[s.ElementID] == nil {
			bucket[s.ElementID] = &dayScores{}
		}
		bucket[s.ElementID].add(s)
	}
	for p, bucket := range sums {
		for i := range p.Elements {
			if s := bucket[p.Elements[i].ElementID]; s != nil {
				p.Elements[i].Answered = s.Answered
				p.Elements[i].Value = s.value(p.Elements[i].Metric)
			}
		}
	}
	return current, previous, nil
}

// periodStats is an empty period with a score for each element.
func periodStats(elements []*element) *PeriodStats {
	p := &PeriodStats{Elements: make([]PeriodScore, len(elements))}
	for i, el := range elements {
		p.Elements[i] = PeriodScore{ElementID: el.ID, Type: el.Type, Headline: surveydoc.TextContent(el.Headline.Default()), Metric: trendMetric(el.Type)}
	}
	return p
}
