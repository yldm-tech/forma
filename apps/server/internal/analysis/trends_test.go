package analysis

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/response"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAutoIntervalFollowsTheSpan(t *testing.T) {
	first := day("2026-01-01")
	for _, c := range []struct {
		days int
		want string
	}{{1, IntervalDay}, {92, IntervalDay}, {93, IntervalWeek}, {731, IntervalWeek}, {732, IntervalMonth}} {
		if got := autoInterval(first, first.AddDate(0, 0, c.days-1)); got != c.want {
			t.Errorf("%d days: %s, want %s", c.days, got, c.want)
		}
	}
}

func TestBucketStartIsTheDayTheISOWeekOrTheMonth(t *testing.T) {
	for _, c := range []struct {
		day, interval, want string
	}{
		{"2026-10-25", IntervalDay, "2026-10-25"},
		{"2026-10-26", IntervalWeek, "2026-10-26"}, // a Monday
		{"2026-10-25", IntervalWeek, "2026-10-19"}, // a Sunday belongs to the week before
		{"2026-11-01", IntervalWeek, "2026-10-26"},
		{"2026-10-31", IntervalMonth, "2026-10-01"},
	} {
		if got := bucketStart(day(c.day), c.interval).Format(time.DateOnly); got != c.want {
			t.Errorf("%s by %s: %s, want %s", c.day, c.interval, got, c.want)
		}
	}
}

func TestBuildTrendsFillsGapsAndCutsTheEnds(t *testing.T) {
	counts := []dayCounts{{Day: day("2026-10-21"), Responses: 2, Completed: 1}, {Day: day("2026-10-23"), Responses: 1, Completed: 1}, {Day: day("2026-11-03"), Responses: 4, Completed: 1}}
	nps := &element{}
	nps.ID, nps.Type = "n", "nps"
	scores := []dayScores{
		{Day: day("2026-10-21"), ElementID: "n", Answered: 2, Promoters: 2},
		{Day: day("2026-10-23"), ElementID: "n", Answered: 1, Detractors: 1},
	}
	got, err := buildTrends([]*element{nps}, nil, trendDays{counts: counts, scores: scores}, nil, time.UTC, IntervalWeek, day("2026-12-01"))
	if err != nil {
		t.Fatal(err)
	}
	wantBuckets := []TrendBucket{
		{Start: "2026-10-21", End: "2026-10-25", Responses: 3, Completed: 2, CompletionPercentage: 66.67},
		{Start: "2026-10-26", End: "2026-11-01"},
		{Start: "2026-11-02", End: "2026-11-03", Responses: 4, Completed: 1, CompletionPercentage: 25},
	}
	if got.Interval != IntervalWeek || got.TimeZone != "UTC" || !reflect.DeepEqual(got.Buckets, wantBuckets) {
		t.Errorf("trend %+v, want buckets %+v", got, wantBuckets)
	}
	// Two promoters and a detractor in the first week: 33.33; nobody answered in the others.
	wantPoints := []TrendPoint{{Answered: 3, Value: ptr(33.33)}, {}, {}}
	if len(got.Elements) != 1 || got.Elements[0].Metric != TrendMetricNPS || !reflect.DeepEqual(got.Elements[0].Points, wantPoints) {
		t.Errorf("elements %+v", got.Elements)
	}
}

func TestBuildTrendsCoversTheFilterWindowInTheZone(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	// The window's instants, a browser's local midnight in UTC+9, are days in the organization's zone; its end is cut to today.
	from, to := time.Date(2026, 3, 1, 0, 0, 0, 0, tokyo), time.Date(2026, 3, 31, 23, 59, 59, 0, tokyo)
	filter := &response.Criteria{CreatedAt: &response.Range{Min: &from, Max: &to}}
	got, err := buildTrends(nil, nil, trendDays{}, filter, tokyo, "", time.Date(2026, 3, 5, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != IntervalDay || len(got.Buckets) != 5 || got.Buckets[0].Start != "2026-03-01" || got.Buckets[4].End != "2026-03-05" {
		t.Errorf("trend %+v", got)
	}
	// Without a window or responses there is nothing to cover.
	if got, err := buildTrends(nil, nil, trendDays{}, nil, time.UTC, "", time.Now()); err != nil || got.Interval != IntervalDay || len(got.Buckets) != 0 || got.Elements == nil {
		t.Errorf("empty trend %+v, %v", got, err)
	}
}

func TestBuildTrendsRefusesTooManyBuckets(t *testing.T) {
	from, to := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	filter := &response.Criteria{CreatedAt: &response.Range{Min: &from, Max: &to}}
	// Responses spread over six years are more days than the buckets hold: by day, the interval the client asked for, they are refused.
	counts := []dayCounts{{Day: day("2020-01-01"), Responses: 1}, {Day: day("2025-12-31"), Responses: 1}}
	_, err := buildTrends(nil, nil, trendDays{counts: counts}, filter, time.UTC, IntervalDay, to)
	if e, ok := apperr.As(err); !ok || e.Code != CodeTrendTooManyBuckets || e.Status != http.StatusBadRequest || !strings.HasSuffix(e.Message, "choose a longer interval") {
		t.Errorf("by day: %v", err)
	}
	if got, err := buildTrends(nil, nil, trendDays{counts: counts}, filter, time.UTC, IntervalMonth, to); err != nil || len(got.Buckets) != 73 {
		t.Errorf("by month: %v, %v", got, err)
	}
	// Without responses there is nothing to lay out beyond the limit: the trend is empty, not refused.
	if got, err := buildTrends(nil, nil, trendDays{}, filter, time.UTC, IntervalDay, to); err != nil || len(got.Buckets) != 0 {
		t.Errorf("empty by day: %v, %v", got, err)
	}
}

func TestBuildTrendsCutsAFarOffWindowToTheResponses(t *testing.T) {
	// A custom range from 1900, by month 1522 buckets, of a survey answered in the last few days.
	from, to := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	filter := &response.Criteria{CreatedAt: &response.Range{Min: &from, Max: &to}}
	counts := []dayCounts{{Day: day("2026-10-20"), Responses: 2, Completed: 1}, {Day: day("2026-10-25"), Responses: 1, Completed: 1}}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, interval := range []string{"", IntervalDay, IntervalWeek, IntervalMonth} {
		got, err := buildTrends(nil, nil, trendDays{counts: counts}, filter, time.UTC, interval, now)
		if err != nil {
			t.Errorf("by %q: %v", interval, err)
			continue
		}
		if got.Buckets[0].Start != "2026-10-20" || got.Buckets[len(got.Buckets)-1].End != "2026-10-26" {
			t.Errorf("by %q: buckets %+v", interval, got.Buckets)
		}
	}
	// The server's pick shows days for the six days with responses, not months back to 1900.
	if got, _ := buildTrends(nil, nil, trendDays{counts: counts}, filter, time.UTC, "", now); got.Interval != IntervalDay || len(got.Buckets) != 7 {
		t.Errorf("auto: %s, %d buckets", got.Interval, len(got.Buckets))
	}
	// A window that fits keeps its empty lead-in: it follows the filter.
	recent := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	fits := &response.Criteria{CreatedAt: &response.Range{Min: &recent, Max: &to}}
	if got, _ := buildTrends(nil, nil, trendDays{counts: counts}, fits, time.UTC, "", now); got.Buckets[0].Start != "2026-10-01" {
		t.Errorf("fitting window: %+v", got.Buckets[0])
	}
	// Responses dated over more than 1000 months apart, which the management API can set, still give the server's pick a trend: the latest 1000 months.
	spread := []dayCounts{{Day: day("1900-01-05"), Responses: 1}, {Day: day("2026-10-20"), Responses: 1}}
	got, err := buildTrends(nil, nil, trendDays{counts: spread}, filter, time.UTC, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != IntervalMonth || len(got.Buckets) != maxTrendBuckets || got.Buckets[0].Start != "1943-07-01" || got.Buckets[len(got.Buckets)-1].Responses != 1 {
		t.Errorf("spread auto: %s %d %+v", got.Interval, len(got.Buckets), got.Buckets[0])
	}
	// The client's own month is refused, with advice that does not point at a longer interval.
	if _, err := buildTrends(nil, nil, trendDays{counts: spread}, filter, time.UTC, IntervalMonth, now); err == nil || !strings.HasSuffix(err.Error(), "choose a shorter date range") {
		t.Errorf("spread by month: %v", err)
	}
}

func TestBuildTrendsMarksPartialBuckets(t *testing.T) {
	// A Tokyo browser's "last 7 days" on 26 October in an organization on UTC: the window starts at 15:00 on the 19th and ends at 14:59:59.999 on the 26th.
	from, to := time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 14, 59, 59, 999e6, time.UTC)
	filter := &response.Criteria{CreatedAt: &response.Range{Min: &from, Max: &to}}
	counts := []dayCounts{{Day: day("2026-10-19"), Responses: 1}, {Day: day("2026-10-20"), Responses: 3}, {Day: day("2026-10-25"), Responses: 4}}
	partial := func(t *Trends) []bool {
		out := make([]bool, len(t.Buckets))
		for i, b := range t.Buckets {
			out[i] = b.Partial
		}
		return out
	}

	// After the 26th the window's first day is cut by its start, its last by its end.
	got, err := buildTrends(nil, nil, trendDays{counts: counts}, filter, time.UTC, IntervalDay, day("2026-11-01"))
	if want := []bool{true, false, false, false, false, false, false, true}; err != nil || !reflect.DeepEqual(partial(got), want) {
		t.Errorf("by day: %v %v, want %v", err, partial(got), want)
	}
	// On the 26th itself, today is partial too, as is the week it is in; the week of the 19th is cut by the window's start.
	now := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	if got, _ := buildTrends(nil, nil, trendDays{counts: counts}, filter, time.UTC, IntervalWeek, now); !reflect.DeepEqual(partial(got), []bool{true, true}) {
		t.Errorf("by week: %v", partial(got))
	}

	// A window of whole days in the zone has no partial bucket once it is over, ending at 23:59:59.999 or at 23:59:59.
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	for _, end := range []time.Time{time.Date(2026, 10, 25, 23, 59, 59, 999e6, tokyo), time.Date(2026, 10, 25, 23, 59, 59, 0, tokyo)} {
		start := time.Date(2026, 10, 19, 0, 0, 0, 0, tokyo)
		whole := &response.Criteria{CreatedAt: &response.Range{Min: &start, Max: &end}}
		got, _ := buildTrends(nil, nil, trendDays{counts: counts}, whole, tokyo, IntervalDay, day("2026-11-01"))
		for _, b := range got.Buckets {
			if b.Partial {
				t.Errorf("window ending %s: %+v is partial", end, b)
			}
		}
	}

	// Without a filter only the interval that has not ended is partial.
	if got, _ := buildTrends(nil, nil, trendDays{counts: counts}, nil, time.UTC, IntervalMonth, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)); !reflect.DeepEqual(partial(got), []bool{true}) {
		t.Errorf("this month: %v", partial(got))
	}
	if got, _ := buildTrends(nil, nil, trendDays{counts: counts}, nil, time.UTC, IntervalMonth, day("2026-11-01")); !reflect.DeepEqual(partial(got), []bool{false}) {
		t.Errorf("last month: %v", partial(got))
	}
}

const trendBlocks = `[
	{"id":"b1","name":"B1","elements":[
		{"id":"q1","type":"openText","headline":{"default":"Your name"},"required":false,"inputType":"text"},
		{"id":"nps","type":"nps","headline":{"default":"Recommend?"},"required":false},
		{"id":"csat","type":"csat","headline":{"default":"Satisfied?"},"required":false,"scale":"smiley","range":5},
		{"id":"stars","type":"rating","headline":{"default":"Stars"},"required":false,"scale":"star","range":5}
	]}
]`

// trendFixture is a survey of an organization in Asia/Tokyo whose first two responses fall on one UTC day and on two days in Tokyo.
func trendFixture(t *testing.T, timeZone *string) (e *env, org, ws, owner, sv uuid.UUID) {
	e = newEnv(t)
	owner = e.user()
	org, ws = e.organization(owner, timeZone)
	sv = e.survey(ws, surveyOpts{name: "CSAT Q1", blocks: trendBlocks})
	e.response(sv, row{at: time.Date(2026, 3, 1, 14, 30, 0, 0, time.UTC), finished: true, data: `{"q1":"Ada","nps":10,"csat":5,"stars":4}`})
	e.response(sv, row{at: time.Date(2026, 3, 1, 15, 30, 0, 0, time.UTC), finished: true, data: `{"nps":3,"csat":2,"stars":2}`})
	e.response(sv, row{at: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC), data: `{"nps":9,"csat":"n/a"}`})
	e.response(sv, row{at: time.Date(2026, 3, 4, 1, 0, 0, 0, time.UTC), finished: true, data: `{"csat":4}`})
	return e, org, ws, owner, sv
}

func TestTrendsBucketResponsesAndScoresInTheOrganizationsZone(t *testing.T) {
	e, org, ws, owner, sv := trendFixture(t, ptr("Asia/Tokyo"))
	path := "/api/app/surveys/" + sv.String() + "/trends"

	var got Trends
	decode(t, e.request(owner, http.MethodPost, path, map[string]any{}), &got)
	wantBuckets := []TrendBucket{
		{Start: "2026-03-01", End: "2026-03-01", Responses: 1, Completed: 1, CompletionPercentage: 100},
		{Start: "2026-03-02", End: "2026-03-02", Responses: 2, Completed: 1, CompletionPercentage: 50},
		{Start: "2026-03-03", End: "2026-03-03"},
		{Start: "2026-03-04", End: "2026-03-04", Responses: 1, Completed: 1, CompletionPercentage: 100},
	}
	if got.Interval != IntervalDay || got.TimeZone != "Asia/Tokyo" || !reflect.DeepEqual(got.Buckets, wantBuckets) {
		t.Errorf("trend %+v, want buckets %+v", got, wantBuckets)
	}
	wantElements := []ElementTrend{
		{ElementID: "nps", Type: "nps", Headline: "Recommend?", Metric: TrendMetricNPS, Points: []TrendPoint{{1, ptr(100.0)}, {2, ptr(0.0)}, {}, {}}},
		{ElementID: "csat", Type: "csat", Headline: "Satisfied?", Metric: TrendMetricSatisfied, Points: []TrendPoint{{1, ptr(100.0)}, {1, ptr(0.0)}, {}, {1, ptr(100.0)}}},
		{ElementID: "stars", Type: "rating", Headline: "Stars", Metric: TrendMetricAverage, Points: []TrendPoint{{1, ptr(4.0)}, {1, ptr(2.0)}, {}, {}}},
	}
	if !reflect.DeepEqual(got.Elements, wantElements) {
		t.Errorf("elements %+v, want %+v", got.Elements, wantElements)
	}

	// The current filter applies: only the completed responses, so the 9 of the unfinished one drops out of 2 March.
	decode(t, e.request(owner, http.MethodPost, path, map[string]any{"filterCriteria": map[string]any{"finished": true}}), &got)
	if got.Buckets[1].Responses != 1 || got.Buckets[1].CompletionPercentage != 100 || *got.Elements[0].Points[1].Value != -100 {
		t.Errorf("finished filter: %+v %+v", got.Buckets, got.Elements[0])
	}

	// By month the four days are one bucket: two promoters and a detractor of three answers.
	decode(t, e.request(owner, http.MethodPost, path, map[string]any{"interval": "month"}), &got)
	if want := []TrendBucket{{Start: "2026-03-01", End: "2026-03-04", Responses: 4, Completed: 3, CompletionPercentage: 75}}; got.Interval != IntervalMonth || !reflect.DeepEqual(got.Buckets, want) {
		t.Errorf("by month %+v", got)
	}
	if p := got.Elements[0].Points[0]; p.Answered != 3 || *p.Value != 33.33 {
		t.Errorf("monthly nps %+v", p)
	}

	// An interval the trend does not know, and a malformed filter, are refused.
	if rec := e.request(owner, http.MethodPost, path, map[string]any{"interval": "year"}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown interval: %d %s", rec.Code, rec.Body)
	}
	if rec := e.request(owner, http.MethodPost, path, map[string]any{"filterCriteria": map[string]any{"data": map[string]any{"q1": map[string]any{"op": "nope"}}}}); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed filter: %d %s", rec.Code, rec.Body)
	}

	// Readers and writers of the workspace may see the trend and download it; anyone else, and anyone asking about a survey that does not exist, is not authorized.
	reader, writer, stranger := e.user(), e.user(), e.user()
	e.grant(org, ws, reader, "read")
	e.grant(org, ws, writer, "readWrite")
	e.organization(stranger, nil)
	for _, user := range []uuid.UUID{reader, writer} {
		if rec := e.request(user, http.MethodPost, path, map[string]any{}); rec.Code != http.StatusOK {
			t.Errorf("member: %d %s", rec.Code, rec.Body)
		}
		if rec := e.request(user, http.MethodPost, path+"/export", map[string]any{"format": "csv"}); rec.Code != http.StatusOK {
			t.Errorf("member export: %d %s", rec.Code, rec.Body)
		}
	}
	for name, c := range map[string]struct{ user, survey uuid.UUID }{"stranger": {stranger, sv}, "unknown survey": {reader, uuid.New()}} {
		for _, p := range []string{"/trends", "/trends/export"} {
			rec := e.request(c.user, http.MethodPost, "/api/app/surveys/"+c.survey.String()+p, map[string]any{"format": "csv"})
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Not authorized") {
				t.Errorf("%s %s: %d %s", name, p, rec.Code, rec.Body)
			}
		}
	}
}

func TestTrendsWithoutAZoneAreInUTC(t *testing.T) {
	e, _, _, owner, sv := trendFixture(t, nil)
	var got Trends
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/trends", map[string]any{}), &got)
	if got.TimeZone != "UTC" || len(got.Buckets) != 4 || got.Buckets[0].Start != "2026-03-01" || got.Buckets[0].Responses != 2 || got.Buckets[1].Responses != 1 {
		t.Errorf("trend %+v", got)
	}
}

func TestTrendsExportWritesARowPerBucket(t *testing.T) {
	e, _, _, owner, sv := trendFixture(t, ptr("Asia/Tokyo"))
	rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/trends/export", map[string]any{"format": "csv", "filterCriteria": map[string]any{}})
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="trends-csat_q1-day-`) || !strings.HasSuffix(cd, `-utc.csv"`) {
		t.Errorf("content disposition %q", cd)
	}
	body := rec.Body.Bytes()
	if !bytes.HasPrefix(body, []byte("\xef\xbb\xbf")) {
		t.Errorf("no byte order mark")
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"Period start", "Period end", "Partial period", "Responses", "Completed", "Completion rate (%)", "Recommend? - NPS", "Recommend? - Answered", "Satisfied? - CSAT satisfied (%)", "Satisfied? - Answered", "Stars - Average", "Stars - Answered"},
		{"2026-03-01", "2026-03-01", "No", "1", "1", "100", "100", "1", "100", "1", "4", "1"},
		{"2026-03-02", "2026-03-02", "No", "2", "1", "50", "0", "2", "0", "1", "2", "1"},
		{"2026-03-03", "2026-03-03", "No", "0", "0", "0", "", "0", "", "0", "", "0"},
		{"2026-03-04", "2026-03-04", "No", "1", "1", "100", "", "0", "100", "1", "", "0"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("csv %q, want %q", rows, want)
	}

	if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/trends/export", map[string]any{"format": "xlsx", "interval": "week"}); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != ContentTypeXLSX {
		t.Errorf("xlsx export: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/trends/export", map[string]any{"format": "pdf"}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown format: %d", rec.Code)
	}
}

const sliderScoreBlocks = `[
	{"id":"b1","name":"B1","elements":[
		{"id":"temp","type":"slider","headline":{"default":"How warm?"},"required":false,"min":0,"max":10,"step":1},
		{"id":"stars","type":"rating","headline":{"default":"Stars"},"required":false,"scale":"star","range":5}
	]}
]`

func TestTrendsAverageASliderAndTheTotalScoreOfCompletedResponses(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{name: "Quiz", blocks: sliderScoreBlocks})
	path := "/api/app/surveys/" + sv.String() + "/trends"

	// Without a total score variable there is no score trend.
	e.response(sv, row{at: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), finished: true, data: `{"temp":6}`, variables: `{"total":4}`})
	var got Trends
	decode(t, e.request(owner, http.MethodPost, path, map[string]any{}), &got)
	if got.Score != nil {
		t.Errorf("score without a score variable: %+v", got.Score)
	}

	e.exec(`UPDATE surveys SET variables = ?::jsonb WHERE id = ?`, `[{"id": "bonus", "name": "bonus", "type": "number", "value": 0}, {"id": "total", "name": "score", "type": "number", "value": 0, "isScore": true}]`, sv)
	e.field(ws, sv, "total", "score", "computed", "number")
	e.response(sv, row{at: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), finished: true, data: `{"temp":8,"stars":4}`, variables: `{"total":2,"bonus":100}`})
	// Not finished: the slider answer counts, as in the slider summary, but the partial score is left out, as in the score summary.
	e.response(sv, row{at: time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC), data: `{"temp":1}`, variables: `{"total":1}`})
	// A slider answer that is not a number and a completed response without a score count in neither.
	e.response(sv, row{at: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC), finished: true, data: `{"temp":"warm"}`, variables: `{"bonus":3}`})
	e.response(sv, row{at: time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC), finished: true, data: `{"temp":10}`, variables: `{"total":"10"}`})
	e.response(sv, row{at: time.Date(2026, 3, 3, 11, 0, 0, 0, time.UTC), finished: true, variables: `{"total":10}`})

	decode(t, e.request(owner, http.MethodPost, path, map[string]any{}), &got)
	if len(got.Buckets) != 3 {
		t.Fatalf("buckets %+v", got.Buckets)
	}
	wantElements := []ElementTrend{
		{ElementID: "temp", Type: "slider", Headline: "How warm?", Metric: TrendMetricAverage, Points: []TrendPoint{{3, ptr(5.0)}, {}, {1, ptr(10.0)}}},
		{ElementID: "stars", Type: "rating", Headline: "Stars", Metric: TrendMetricAverage, Points: []TrendPoint{{1, ptr(4.0)}, {}, {}}},
	}
	if !reflect.DeepEqual(got.Elements, wantElements) {
		t.Errorf("elements %+v, want %+v", got.Elements, wantElements)
	}
	wantScore := &ScoreTrend{VariableID: "total", Name: "score", Points: []TrendPoint{{2, ptr(3.0)}, {}, {1, ptr(10.0)}}}
	if !reflect.DeepEqual(got.Score, wantScore) {
		t.Errorf("score %+v, want %+v", got.Score, wantScore)
	}

	// The current filter applies to the score too: only the scores above 3 remain.
	decode(t, e.request(owner, http.MethodPost, path, map[string]any{"filterCriteria": map[string]any{"variables": map[string]any{"total": map[string]any{"op": "greaterThan", "value": 3}}}}), &got)
	if want := []TrendPoint{{1, ptr(4.0)}, {}, {1, ptr(10.0)}}; got.Score == nil || !reflect.DeepEqual(got.Score.Points, want) {
		t.Errorf("filtered score %+v, want points %+v", got.Score, want)
	}

	// By month the completed scores 4, 2 and 10 are one bucket.
	decode(t, e.request(owner, http.MethodPost, path, map[string]any{"interval": "month"}), &got)
	if want := []TrendPoint{{3, ptr(5.33)}}; got.Score == nil || !reflect.DeepEqual(got.Score.Points, want) {
		t.Errorf("monthly score %+v, want points %+v", got.Score, want)
	}

	rec := e.request(owner, http.MethodPost, path+"/export", map[string]any{"format": "csv"})
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(rec.Body.Bytes(), []byte("\xef\xbb\xbf")))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"Period start", "Period end", "Partial period", "Responses", "Completed", "Completion rate (%)", "How warm? - Average", "How warm? - Answered", "Stars - Average", "Stars - Answered", "Total score (score) - Average", "Total score (score) - Responses with a score"},
		{"2026-03-01", "2026-03-01", "No", "3", "2", "66.67", "5", "3", "4", "1", "3", "2"},
		{"2026-03-02", "2026-03-02", "No", "1", "1", "100", "", "0", "", "0", "", "0"},
		{"2026-03-03", "2026-03-03", "No", "2", "2", "100", "10", "1", "", "0", "10", "1"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("csv %q, want %q", rows, want)
	}
}
