package integration

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

func TestTruncate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in    string
		limit int
		want  string
	}{
		{"hello", 5, "hello"},
		{"hello!", 5, "hello..."},
		// Lengths are JavaScript's UTF-16 code units: an astral character counts twice.
		{"ab😀", 4, "ab😀"},
		{"ab😀c", 4, "ab😀..."},
		{"日本語", 2, "日本..."},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.limit); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.limit, got, c.want)
		}
	}
}

// forma2's getSafeOAuthCallbackError.
func TestSafeCallbackError(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"": "", "access_denied": "access_denied", "invalid_request": "invalid_request", "invalid_scope": "invalid_scope",
		"server_error": "server_error", "temporarily_unavailable": "temporarily_unavailable",
		"<script>": "oauth_error", "unauthorized_client": "oauth_error",
	} {
		if got := safeCallbackError(in); got != want {
			t.Errorf("safeCallbackError(%q) = %q, want %q", in, got, want)
		}
	}
}

func decodeJSON(t *testing.T, raw string) (any, json.RawMessage) {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return v, json.RawMessage(raw)
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := encodeJS(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// forma2's getValue and its `|| null`, including its handle-integrations tests "coerces non-string Notion text values and avoids invalid multi-url payloads" and "sanitizes mixed Notion multi-select values and preserves numeric precision".
func TestNotionValue(t *testing.T) {
	t.Parallel()
	in := func(raw string) notionInput {
		v, r := decodeJSON(t, raw)
		return notionInput{v: v, raw: r}
	}
	undefined := notionInput{v: jsvalue.Undefined}
	created := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	date := notionInput{date: &created}
	cases := []struct {
		name   string
		column string
		in     notionInput
		want   string
	}{
		{"number as title", "title", in(`42`), `[{"text":{"content":"42"}}]`},
		{"object as rich text, in its stored order", "rich_text", in(`{"zeta":"z","alpha":"a"}`), `[{"text":{"content":"{\"zeta\":\"z\",\"alpha\":\"a\"}"}}]`},
		{"list as title", "title", in(`["a",null,"b"]`), `[{"text":{"content":"a\n\nb"}}]`},
		{"empty title", "title", in(`""`), `null`},
		{"absent title", "title", undefined, `null`},
		{"creation time as title", "title", date, `[{"text":{"content":"2024-01-01T12:00:00.000Z"}}]`},
		{"long rich text", "rich_text", notionInput{v: strings.Repeat("x", 2000)}, `[{"text":{"content":"` + strings.Repeat("x", 1995) + `..."}}]`},
		{"long title is not cut", "title", notionInput{v: strings.Repeat("x", 2000)}, `[{"text":{"content":"` + strings.Repeat("x", 2000) + `"}}]`},
		{"several urls", "url", in(`["https://example.com/a","https://example.com/b"]`), `null`},
		{"one url", "url", in(`["https://example.com/a"]`), `"https://example.com/a"`},
		{"url string", "url", in(`"https://example.com"`), `"https://example.com"`},
		{"mixed multi select", "multi_select", in(`["Choice 1",{"name":"Choice, 3"},42]`), `[{"name":"Choice 1"},{"name":"Choice 3"}]`},
		{"empty multi select", "multi_select", in(`[]`), `[]`},
		{"multi select of a string", "multi_select", in(`"a"`), `null`},
		{"select without commas", "select", in(`"a,b"`), `{"name":"ab"}`},
		{"select of a list", "select", in(`["a"]`), `null`},
		{"fractional number", "number", in(`3.5`), `3.5`},
		{"numeric string", "number", in(`"7"`), `7`},
		{"zero is null", "number", in(`0`), `null`},
		{"not a number", "number", in(`"abc"`), `null`},
		{"creation time as number", "number", date, `1704110400000`},
		{"accepted consent", "checkbox", in(`"accepted"`), `true`},
		{"clicked call to action", "checkbox", in(`"clicked"`), `true`},
		{"false checkbox is null", "checkbox", in(`"dismissed"`), `null`},
		{"date", "date", in(`"2024-02-03"`), `{"start":"2024-02-03"}`},
		{"absent date", "date", undefined, `{}`},
		{"creation time as date", "date", date, `{"start":"2024-01-01T12:00:00.000Z"}`},
		{"status", "status", in(`"Done"`), `{"name":"Done"}`},
		{"absent status", "status", undefined, `{}`},
		{"email", "email", in(`"a@example.com"`), `"a@example.com"`},
		{"empty email", "email", in(`""`), `null`},
		{"phone number", "phone_number", in(`"+1 555"`), `"+1 555"`},
		{"unsupported column", "rollup", in(`"x"`), `null`},
	}
	for _, c := range cases {
		if got := asJSON(t, notionValue(c.column, c.in)); got != c.want {
			t.Errorf("%s: %s", c.name, got)
		}
	}
}

func TestMetadataSkipsWhatIsNotSet(t *testing.T) {
	t.Parallel()
	r := &submission{meta: map[string]any{"source": "", "url": "https://example.com", "userAgent": map[string]any{"os": "Linux"}, "country": nil}}
	if got := r.metadata(); got != "URL: https://example.com\nOS: Linux" {
		t.Errorf("metadata = %q", got)
	}
}

func TestStoredLinksRoundTrip(t *testing.T) {
	t.Parallel()
	links := []SheetLink{{LinkBase: LinkBase{CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), SurveyID: "s", SurveyName: "S", IncludeCreatedAt: ptr(true)}, Elements: Elements{ElementIDs: []string{"q1"}, Elements: "All questions"}, SpreadsheetID: "sheet", SpreadsheetName: "Sheet"}}
	raw, err := encodeLinks(links)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"data":[{"createdAt":"2024-01-01T00:00:00Z","surveyId":"s","surveyName":"S","includeCreatedAt":true,"elementIds":["q1"],"elements":"All questions","spreadsheetId":"sheet","spreadsheetName":"Sheet"}]}`
	if string(raw) != want {
		t.Errorf("config = %s", raw)
	}
	back, err := decodeLinks[SheetLink](raw)
	if err != nil || !reflect.DeepEqual(back, links) {
		t.Errorf("decoded = %+v, %v", back, err)
	}
	if empty, _ := encodeLinks[SlackLink](nil); string(empty) != `{"data":[]}` {
		t.Errorf("no links = %s", empty)
	}
}
