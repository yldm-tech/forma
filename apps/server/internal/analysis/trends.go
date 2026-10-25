package analysis

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// Trend intervals: the length of one bucket. An empty interval asks the server to pick one from the span the buckets cover (autoInterval).
const (
	IntervalDay   = "day"
	IntervalWeek  = "week"
	IntervalMonth = "month"
)

// Trend metrics, what an element trend's values are: the NPS score (-100 to 100), the share of satisfied answers (4 or 5) in percent, or the average answer.
const (
	TrendMetricNPS       = "nps"
	TrendMetricSatisfied = "satisfied"
	TrendMetricAverage   = "average"
)

// maxTrendBuckets bounds the buckets one trend returns; a range that needs more at the interval asked for is refused with CodeTrendTooManyBuckets, so the client offers a longer interval.
const maxTrendBuckets = 1000

// CodeTrendTooManyBuckets is the error code of a trend whose range needs more than maxTrendBuckets buckets at the interval asked for.
const CodeTrendTooManyBuckets = "trend_too_many_buckets"

// The longest span, in days, that autoInterval shows by day, and then by week; anything longer is shown by month.
const (
	autoDayMaxDays  = 92
	autoWeekMaxDays = 731
)

// Trends is the survey's responses over time: the matching responses grouped by the day, ISO week (from Monday) or month they were created in, in the organization's display time zone, with each score element's metric per bucket. Buckets run without gaps from the first to the last day of the range, a bucket without responses counting zero.
type Trends struct {
	Interval string `json:"interval"`
	// TimeZone is the zone the buckets' days are in: the organization's display time zone, UTC when it has none.
	TimeZone string         `json:"timeZone"`
	Buckets  []TrendBucket  `json:"buckets"`
	Elements []ElementTrend `json:"elements"`
}

// TrendBucket is one interval: its first and last day (both inclusive, local dates, cut to the range at the two ends), the responses started in it and how many of them are completed, with that share.
type TrendBucket struct {
	Start                string  `json:"start"`
	End                  string  `json:"end"`
	Responses            int     `json:"responses"`
	Completed            int     `json:"completed"`
	CompletionPercentage float64 `json:"completionPercentage"`
}

// ElementTrend is one NPS, CSAT, rating or CES element's metric per bucket; Points line up with the trend's Buckets.
type ElementTrend struct {
	ElementID string       `json:"elementId"`
	Type      string       `json:"type"`
	Headline  string       `json:"headline"`
	Metric    string       `json:"metric"`
	Points    []TrendPoint `json:"points"`
}

// TrendPoint is an element's metric in one bucket over the responses that answered it with a number; Value is null when none did.
type TrendPoint struct {
	Answered int      `json:"answered"`
	Value    *float64 `json:"value"`
}

// trendMetric is the metric the trend shows for an element type, "" for a type without one.
func trendMetric(elementType string) string {
	switch elementType {
	case surveydoc.ElementNPS:
		return TrendMetricNPS
	case surveydoc.ElementCSAT:
		return TrendMetricSatisfied
	case surveydoc.ElementRating, surveydoc.ElementCES:
		return TrendMetricAverage
	}
	return ""
}

// trendZone is the organization's display time zone, UTC when it has none or it does not load, as FormatTimestamp reads it.
func trendZone(sv *surveyDoc) *time.Location {
	if sv.DisplayTimeZone == nil || *sv.DisplayTimeZone == "" || *sv.DisplayTimeZone == "Local" {
		return time.UTC
	}
	loc, err := time.LoadLocation(*sv.DisplayTimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// dayCounts is one local day's responses.
type dayCounts struct {
	Day       time.Time
	Responses int
	Completed int
}

// dayScores is one local day's numeric answers to one element: how many, how many promote (9 and above) and detract (below 7) as NPS reads them, how many are satisfied (4 or 5) as CSAT reads them, and their sum. The groups match the summary's npsAggregate and scaleAggregate.
type dayScores struct {
	Day        time.Time
	ElementID  string
	Answered   int
	Promoters  int
	Detractors int
	Satisfied  int
	Total      float64
}

// add adds o's counts to s.
func (s *dayScores) add(o dayScores) {
	s.Answered += o.Answered
	s.Promoters += o.Promoters
	s.Detractors += o.Detractors
	s.Satisfied += o.Satisfied
	s.Total += o.Total
}

// value is the metric over the answers s counts; nil without answers.
func (s dayScores) value(metric string) *float64 {
	if s.Answered == 0 {
		return nil
	}
	var v float64
	switch metric {
	case TrendMetricNPS:
		v = percent(float64(s.Promoters-s.Detractors), float64(s.Answered))
	case TrendMetricSatisfied:
		v = percent(float64(s.Satisfied), float64(s.Answered))
	default:
		v = round2(s.Total / float64(s.Answered))
	}
	return &v
}

// readTrendDays reads the matching responses grouped by the local day they were created in, and the numeric answers of the elements by day and element. Grouping is done by PostgreSQL in the zone's wall time; only days are returned, so the rows stay few however many responses there are.
func readTrendDays(ctx context.Context, db *gorm.DB, surveyID uuid.UUID, clause response.Clause, zone string, elementIDs []string) ([]dayCounts, []dayScores, error) {
	var counts []dayCounts
	args := append([]any{zone, surveyID}, clause.Args...)
	if err := db.WithContext(ctx).Raw(`SELECT (r.created_at AT TIME ZONE ?)::date AS day, count(*) AS responses, count(*) FILTER (WHERE r.finished) AS completed
		FROM responses r WHERE r.survey_id = ? AND `+clause.SQL+` GROUP BY 1 ORDER BY 1`, args...).Scan(&counts).Error; err != nil {
		return nil, nil, fmt.Errorf("count responses by day: %w", err)
	}
	if len(elementIDs) == 0 || len(counts) == 0 {
		return counts, nil, nil
	}
	values := make([]string, len(elementIDs))
	args = []any{zone}
	for i, id := range elementIDs {
		values[i] = "(?)"
		args = append(args, id)
	}
	args = append(append(args, surveyID), clause.Args...)
	var scores []dayScores
	if err := db.WithContext(ctx).Raw(`SELECT s.day, s.element_id, count(*) AS answered,
			count(*) FILTER (WHERE s.v >= 9) AS promoters, count(*) FILTER (WHERE s.v < 7) AS detractors,
			count(*) FILTER (WHERE s.v = 4 OR s.v = 5) AS satisfied, sum(s.v)::float8 AS total
		FROM (SELECT (r.created_at AT TIME ZONE ?)::date AS day, e.id AS element_id, (r.data -> e.id)::numeric AS v
			FROM responses r CROSS JOIN (VALUES `+strings.Join(values, ", ")+`) AS e(id)
			WHERE r.survey_id = ? AND `+clause.SQL+` AND jsonb_typeof(r.data -> e.id) = 'number') s
		GROUP BY 1, 2 ORDER BY 1, 2`, args...).Scan(&scores).Error; err != nil {
		return nil, nil, fmt.Errorf("aggregate scores by day: %w", err)
	}
	return counts, scores, nil
}

// localDate is the calendar day of t in loc, as midnight UTC, the form the days are compared and stepped in.
func localDate(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// dateOnly is a day read from PostgreSQL as midnight UTC.
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// bucketStart is the first day of the interval holding day: the day itself, the Monday of its ISO week, or the first of its month.
func bucketStart(day time.Time, interval string) time.Time {
	switch interval {
	case IntervalWeek:
		return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	case IntervalMonth:
		return time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return day
}

// nextBucket is the first day of the interval after the one starting on start.
func nextBucket(start time.Time, interval string) time.Time {
	switch interval {
	case IntervalWeek:
		return start.AddDate(0, 0, 7)
	case IntervalMonth:
		return start.AddDate(0, 1, 0)
	}
	return start.AddDate(0, 0, 1)
}

// autoInterval is the interval a range of days is shown in when the client leaves the choice to the server: by day up to about three months, by week up to about two years, by month beyond.
func autoInterval(first, last time.Time) string {
	days := int(last.Sub(first).Hours()/24) + 1
	switch {
	case days <= autoDayMaxDays:
		return IntervalDay
	case days <= autoWeekMaxDays:
		return IntervalWeek
	}
	return IntervalMonth
}

// trendRange is the first and last day the buckets cover: the filter's creation window in the zone where it has one (its end no later than today), the days the matching responses were created on otherwise, widened to every such day. ok is false when there is nothing to cover.
func trendRange(filter *response.Criteria, counts []dayCounts, loc *time.Location, now time.Time) (first, last time.Time, ok bool) {
	var hasFirst, hasLast bool
	if filter != nil && filter.CreatedAt != nil {
		if filter.CreatedAt.Min != nil {
			first, hasFirst = localDate(*filter.CreatedAt.Min, loc), true
		}
		if filter.CreatedAt.Max != nil {
			last, hasLast = localDate(*filter.CreatedAt.Max, loc), true
			if today := localDate(now, loc); last.After(today) {
				last = today
			}
		}
	}
	if len(counts) > 0 {
		earliest, latest := dateOnly(counts[0].Day), dateOnly(counts[len(counts)-1].Day)
		if !hasFirst || earliest.Before(first) {
			first, hasFirst = earliest, true
		}
		if !hasLast || latest.After(last) {
			last, hasLast = latest, true
		}
	}
	if !hasFirst || !hasLast || last.Before(first) {
		return time.Time{}, time.Time{}, false
	}
	return first, last, true
}

// trendElements are the survey's elements a trend has a metric for, in document order.
func trendElements(sv *surveyDoc) []*element {
	var out []*element
	for i := range sv.Elements {
		if trendMetric(sv.Elements[i].Type) != "" {
			out = append(out, &sv.Elements[i])
		}
	}
	return out
}

// buildTrends lays the days out in buckets of the interval ("" picks one) over the range, and computes each bucket's counts and the elements' metrics.
func buildTrends(elements []*element, counts []dayCounts, scores []dayScores, filter *response.Criteria, loc *time.Location, interval string, now time.Time) (*Trends, error) {
	out := &Trends{Interval: interval, TimeZone: loc.String(), Buckets: []TrendBucket{}, Elements: []ElementTrend{}}
	first, last, ok := trendRange(filter, counts, loc, now)
	if out.Interval == "" {
		out.Interval = IntervalDay
		if ok {
			out.Interval = autoInterval(first, last)
		}
	}
	var starts []time.Time
	index := map[time.Time]int{}
	if ok {
		for start := bucketStart(first, out.Interval); !start.After(last); start = nextBucket(start, out.Interval) {
			if len(starts) == maxTrendBuckets {
				return nil, apperr.InvalidInput(CodeTrendTooManyBuckets, fmt.Sprintf("The range needs more than %d buckets by %s; choose a longer interval", maxTrendBuckets, out.Interval))
			}
			index[start] = len(starts)
			starts = append(starts, start)
		}
	}
	for i, start := range starts {
		from, to := start, nextBucket(start, out.Interval).AddDate(0, 0, -1)
		if i == 0 && from.Before(first) {
			from = first
		}
		if i == len(starts)-1 && to.After(last) {
			to = last
		}
		out.Buckets = append(out.Buckets, TrendBucket{Start: from.Format(time.DateOnly), End: to.Format(time.DateOnly)})
	}
	for _, c := range counts {
		i, found := index[bucketStart(dateOnly(c.Day), out.Interval)]
		if !found {
			continue
		}
		out.Buckets[i].Responses += c.Responses
		out.Buckets[i].Completed += c.Completed
	}
	for i := range out.Buckets {
		out.Buckets[i].CompletionPercentage = percent(float64(out.Buckets[i].Completed), float64(out.Buckets[i].Responses))
	}
	perElement := map[string][]dayScores{}
	for _, el := range elements {
		perElement[el.ID] = make([]dayScores, len(starts))
	}
	for _, s := range scores {
		buckets, known := perElement[s.ElementID]
		i, found := index[bucketStart(dateOnly(s.Day), out.Interval)]
		if !known || !found {
			continue
		}
		buckets[i].add(s)
	}
	for _, el := range elements {
		metric := trendMetric(el.Type)
		points := make([]TrendPoint, len(starts))
		for i, s := range perElement[el.ID] {
			points[i] = TrendPoint{Answered: s.Answered, Value: s.value(metric)}
		}
		out.Elements = append(out.Elements, ElementTrend{ElementID: el.ID, Type: el.Type, Headline: surveydoc.TextContent(el.Headline.Default()), Metric: metric, Points: points})
	}
	return out, nil
}

// trends computes the survey's trend over the responses matching the filter, in one read-only snapshot.
func (s *Service) trends(ctx context.Context, sv *surveyDoc, filter *response.Criteria, interval string) (*Trends, error) {
	clause, err := response.Where(sv.Survey, filter)
	if err != nil {
		return nil, err
	}
	loc := trendZone(sv)
	elements := trendElements(sv)
	ids := make([]string, len(elements))
	for i, el := range elements {
		ids[i] = el.ID
	}
	var counts []dayCounts
	var scores []dayScores
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		counts, scores, err = readTrendDays(ctx, tx, sv.ID, clause, loc.String(), ids)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return buildTrends(elements, counts, scores, filter, loc, interval, time.Now())
}

// Trends is the survey's responses over time under the filter, by the interval asked for ("" lets the server pick one).
func (s *Service) Trends(ctx context.Context, userID, surveyID uuid.UUID, filter *response.Criteria, interval string) (*Trends, error) {
	sv, err := s.readableSurvey(ctx, userID, surveyID, authz.SurveyResponseRead)
	if err != nil {
		return nil, err
	}
	return s.trends(ctx, sv, filter, interval)
}

// trendExportFileName is trends-<name>-<interval>-<UTC time>-utc.<format>, lower case, the response export's name with the interval.
func trendExportFileName(surveyName, interval, format string, now time.Time) string {
	name := unsafeFileNameRun.ReplaceAllString(surveyName, "_")
	if len(name) > 255 {
		name = name[:255]
	}
	return strings.ToLower(fmt.Sprintf("trends-%s-%s-%s-utc.%s", name, interval, now.UTC().Format("2006-01-02-15-04-05"), format))
}

// trendExportHeaders are the columns of the trend export: the bucket's days and counts, then per score element its metric and how many answered, headed by the element's headline.
func trendExportHeaders(t *Trends) []string {
	headers := []string{"Period start", "Period end", "Responses", "Completed", "Completion rate (%)"}
	for _, el := range t.Elements {
		label := map[string]string{TrendMetricNPS: "NPS", TrendMetricSatisfied: "CSAT satisfied (%)", TrendMetricAverage: "Average"}[el.Metric]
		headers = append(headers, fmt.Sprintf("%s - %s", el.Headline, label), fmt.Sprintf("%s - Answered", el.Headline))
	}
	return headers
}

// ExportTrends writes the trend the summary shows, one row per bucket, as a CSV or XLSX file to w, and returns the file's name. It needs the response export's permission, since the file leaves the app.
func (s *Service) ExportTrends(ctx context.Context, userID, surveyID uuid.UUID, format string, filter *response.Criteria, interval string, w io.Writer) (string, error) {
	sv, err := s.readableSurvey(ctx, userID, surveyID, authz.SurveyResponseExport)
	if err != nil {
		return "", err
	}
	t, err := s.trends(ctx, sv, filter, interval)
	if err != nil {
		return "", err
	}
	var out sheetWriter
	if format == FormatXLSX {
		if out, err = newXLSXWriter(w); err != nil {
			return "", err
		}
	} else {
		out = newCSVWriter(w)
	}
	headers := trendExportHeaders(t)
	cells := make([]any, len(headers))
	for i, h := range headers {
		cells[i] = h
	}
	if err := out.row(cells); err != nil {
		return "", err
	}
	for i, b := range t.Buckets {
		cells = []any{b.Start, b.End, float64(b.Responses), float64(b.Completed), b.CompletionPercentage}
		for _, el := range t.Elements {
			p := el.Points[i]
			var value any
			if p.Value != nil {
				value = *p.Value
			}
			cells = append(cells, value, float64(p.Answered))
		}
		if err := out.row(cells); err != nil {
			return "", err
		}
	}
	if err := out.close(); err != nil {
		return "", err
	}
	org, ws := sv.OrganizationID.String(), sv.WorkspaceID.String()
	analytics.Capture(userID.String(), "trends_exported", analytics.Properties{
		"survey_id":       surveyID.String(),
		"format":          format,
		"interval":        t.Interval,
		"filter_applied":  filter != nil && !reflect.DeepEqual(*filter, response.Criteria{}),
		"organization_id": org,
		"workspace_id":    ws,
	}, analytics.Groups{OrganizationID: org, WorkspaceID: ws})
	return trendExportFileName(sv.Name, t.Interval, format, time.Now()), nil
}
