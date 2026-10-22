package embeddeddata

import (
	"reflect"
	"testing"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// The catalog cases of forma2's packages/types/embedded-data-resolver.test.ts.

func capturedResponse() ValueResponse {
	lang := "de"
	return ValueResponse{
		ID:        "clx0000000000000000000r3",
		SurveyID:  "clx0000000000000000000s2",
		CreatedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 1, 9, 2, 30, 0, time.UTC),
		Finished:  true,
		Language:  &lang,
		Data:      map[string]any{"plan": "premium"},
		Variables: map[string]any{},
		TTC:       map[string]any{"q1": 40000.0, "q2": 50400.0, "_total": 90400.0},
		Meta: map[string]any{
			"source":         "link",
			"url":            "https://example.com/pricing?utm_source=news&email=a@b.co",
			"userAgent":      map[string]any{"browser": "Chrome", "os": "macOS", "device": "desktop"},
			"country":        "DE",
			"action":         "Clicked Upgrade",
			"ipAddress":      "203.0.113.7",
			"pagePath":       "/pricing",
			"pageReferrer":   "https://news.example.org/weekly?issue=42",
			"utmSource":      "news",
			"utmMedium":      "email",
			"utmCampaign":    "august-launch",
			"utmTerm":        "pricing",
			"utmContent":     "hero-cta",
			"screenWidth":    2560.0,
			"screenHeight":   1440.0,
			"viewportWidth":  1280.0,
			"viewportHeight": 800.0,
			"timezone":       "Europe/Berlin",
			"locale":         "de-AT",
		},
	}
}

func TestEveryCatalogEntryResolvesAgainstAFullyCapturedResponse(t *testing.T) {
	want := map[string]any{
		"source": "link", "url": "https://example.com/pricing", "country": "DE", "action": "Clicked Upgrade",
		"browser": "Chrome", "os": "macOS", "deviceType": "desktop", "ipAddress": "203.0.113.7", "finished": "true",
		"language": "de", "responseId": "clx0000000000000000000r3", "surveyId": "clx0000000000000000000s2",
		"durationSeconds": 90.0, "startedAt": "2026-08-01T09:00:00.000Z", "finishedAt": "2026-08-01T09:02:30.000Z",
		"pagePath": "/pricing", "pageReferrer": "https://news.example.org/weekly", "utmSource": "news", "utmMedium": "email",
		"utmCampaign": "august-launch", "utmTerm": "pricing", "utmContent": "hero-cta", "screenWidth": 2560.0, "screenHeight": 1440.0,
		"viewportWidth": 1280.0, "viewportHeight": 800.0, "timezone": "Europe/Berlin", "locale": "de-AT",
	}
	if got := Project(ReservedCatalog, capturedResponse()); !reflect.DeepEqual(got, want) {
		t.Fatalf("Project = %#v\nwant %#v", got, want)
	}
}

func TestAHistoricalResponseResolvesWhatItCapturedAndNothingElse(t *testing.T) {
	r := ValueResponse{
		ID: "clx0000000000000000000r4", SurveyID: "clx0000000000000000000s2",
		CreatedAt: time.Date(2024, 3, 4, 8, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2024, 3, 4, 8, 1, 0, 0, time.UTC),
		Finished: true, Data: map[string]any{"plan": "free"}, Variables: map[string]any{},
		Meta: map[string]any{"source": "link", "url": "https://example.com/old"},
	}
	want := map[string]any{
		"source": "link", "url": "https://example.com/old", "finished": "true", "responseId": "clx0000000000000000000r4",
		"surveyId": "clx0000000000000000000s2", "startedAt": "2024-03-04T08:00:00.000Z", "finishedAt": "2024-03-04T08:01:00.000Z",
	}
	if got := Project(ReservedCatalog, r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Project = %#v\nwant %#v", got, want)
	}
}

func TestDurationSecondsIsUnsetOnAPartialResponse(t *testing.T) {
	r := capturedResponse()
	r.Finished = false
	r.TTC = map[string]any{"q1": 40000.0, "q2": 50400.0}
	got := Project(ReservedCatalog, r)
	if _, ok := got["durationSeconds"]; ok {
		t.Fatalf("durationSeconds = %v on a partial response", got["durationSeconds"])
	}
	if got["finished"] != "false" {
		t.Fatalf("finished = %v, want the string false", got["finished"])
	}
}

func TestServerValuesLetDeclaredNamesShadowReservedFields(t *testing.T) {
	r := capturedResponse()
	r.Data = map[string]any{"q1": "answer"}
	// A declared field named url shadows the reserved url even when the response left it blank, and an element id shadows country.
	got := ServerValues(r, []Linked{ingestedField("url", TypeString, false)}, []string{"country"})
	if _, ok := got["url"]; ok {
		t.Errorf("a declared url still resolved the reserved one: %v", got["url"])
	}
	if _, ok := got["country"]; ok {
		t.Errorf("an element id still resolved the reserved country: %v", got["country"])
	}
	if got["browser"] != "Chrome" || got["q1"] != "answer" {
		t.Errorf("ServerValues = %#v", got)
	}
	r.Data = map[string]any{"browser": "typed by hand"}
	if got := ServerValues(r, nil, nil); got["browser"] != "typed by hand" {
		t.Errorf("the response's own data did not win over the reserved value: %v", got["browser"])
	}
}

func TestLogicVariableValue(t *testing.T) {
	fields := []Linked{
		{Field: Field{StorageKey: "score", Source: SourceComputed, DataType: TypeNumber, DefaultValue: 5.0}},
		{Field: Field{StorageKey: "tier", Source: SourceComputed, DataType: TypeString, DefaultValue: "gold"}},
	}
	cases := []struct {
		key       string
		variables map[string]any
		want      any
	}{
		{"score", map[string]any{"score": 42.0}, 42.0},
		{"tier", map[string]any{"tier": "silver"}, "silver"},
		{"score", map[string]any{"score": "42"}, 42.0},
		{"score", map[string]any{"score": "abc"}, 0.0},
		{"tier", map[string]any{"tier": 0.0}, ""},
		{"score", map[string]any{}, 0.0},
		{"tier", map[string]any{}, ""},
		{"deleted_variable", map[string]any{"deleted_variable": 7.0}, jsvalue.Undefined},
	}
	for _, c := range cases {
		if got := LogicVariableValue(fields, c.key, c.variables); got != c.want {
			t.Errorf("LogicVariableValue(%s, %v) = %#v, want %#v", c.key, c.variables, got, c.want)
		}
	}
}

// The catalog's display placement, as forma2's RESERVED_FIELD_CATALOG declares it: what the response views show first, what they show second, and what they never show.
func TestCatalogDisplay(t *testing.T) {
	byDisplay := map[string][]string{}
	for _, e := range ReservedCatalog {
		byDisplay[e.Display] = append(byDisplay[e.Display], e.Name)
	}
	want := map[string][]string{
		DisplayPrimary: {"source", "url", "country", "action", "browser", "os", "deviceType"},
		DisplaySecondary: {"ipAddress", "pagePath", "pageReferrer", "utmSource", "utmMedium", "utmCampaign", "utmTerm", "utmContent",
			"screenWidth", "screenHeight", "viewportWidth", "viewportHeight", "timezone", "locale"},
		DisplayNone: {"finished", "language", "responseId", "surveyId", "durationSeconds", "startedAt", "finishedAt"},
	}
	if !reflect.DeepEqual(byDisplay, want) {
		t.Errorf("display placement %v", byDisplay)
	}
}

// forma2's formatFieldNameToTitleCase cases (packages/types/safe-identifier.test.ts).
func TestTitleCase(t *testing.T) {
	for in, want := range map[string]string{
		"durationSeconds": "Duration Seconds", "ip_address": "Ip Address", "utmSource": "Utm Source", "deviceType": "Device Type",
		"os": "Os", "a__b": "A B", "_lead": "Lead", "screenWidth2X": "Screen Width2 X", "": "",
	} {
		if got := TitleCase(in); got != want {
			t.Errorf("TitleCase(%q) = %q, want %q", in, got, want)
		}
	}
}
