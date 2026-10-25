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
	got, err := buildTrends([]*element{nps}, counts, scores, nil, time.UTC, IntervalWeek, day("2026-12-01"))
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
	got, err := buildTrends(nil, nil, nil, filter, tokyo, "", time.Date(2026, 3, 5, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != IntervalDay || len(got.Buckets) != 5 || got.Buckets[0].Start != "2026-03-01" || got.Buckets[4].End != "2026-03-05" {
		t.Errorf("trend %+v", got)
	}
	// Without a window or responses there is nothing to cover.
	if got, err := buildTrends(nil, nil, nil, nil, time.UTC, "", time.Now()); err != nil || got.Interval != IntervalDay || len(got.Buckets) != 0 || got.Elements == nil {
		t.Errorf("empty trend %+v, %v", got, err)
	}
}

func TestBuildTrendsRefusesTooManyBuckets(t *testing.T) {
	from, to := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	filter := &response.Criteria{CreatedAt: &response.Range{Min: &from, Max: &to}}
	_, err := buildTrends(nil, nil, nil, filter, time.UTC, IntervalDay, to)
	if e, ok := apperr.As(err); !ok || e.Code != CodeTrendTooManyBuckets || e.Status != http.StatusBadRequest {
		t.Errorf("by day: %v", err)
	}
	if got, err := buildTrends(nil, nil, nil, filter, time.UTC, IntervalMonth, to); err != nil || len(got.Buckets) != 73 {
		t.Errorf("by month: %v, %v", got, err)
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
		{"Period start", "Period end", "Responses", "Completed", "Completion rate (%)", "Recommend? - NPS", "Recommend? - Answered", "Satisfied? - CSAT satisfied (%)", "Satisfied? - Answered", "Stars - Average", "Stars - Answered"},
		{"2026-03-01", "2026-03-01", "1", "1", "100", "100", "1", "100", "1", "4", "1"},
		{"2026-03-02", "2026-03-02", "2", "1", "50", "0", "2", "0", "1", "2", "1"},
		{"2026-03-03", "2026-03-03", "0", "0", "0", "", "0", "", "0", "", "0"},
		{"2026-03-04", "2026-03-04", "1", "1", "100", "", "0", "100", "1", "", "0"},
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
