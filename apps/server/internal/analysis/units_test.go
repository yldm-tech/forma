package analysis

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

func TestJavaScriptNumberRules(t *testing.T) {
	for _, c := range []struct{ in, round2, fixed2 float64 }{
		{33.333333, 33.33, 33.33},
		{66.666666, 66.67, 66.67},
		{1.005, 1, 1}, // 1.005 * 100 is 100.49999999999999, and toFixed reads 1.00499999999999989...
		{0.125, 0.13, 0.13},
		{12.5, 12.5, 12.5},
		{2.675, 2.68, 2.67},
		{-1.5, -1.5, -1.5},
		{0, 0, 0},
	} {
		if got := round2(c.in); got != c.round2 {
			t.Errorf("round2(%v) = %v, want %v", c.in, got, c.round2)
		}
		if got := toFixed2(c.in); got != c.fixed2 {
			t.Errorf("toFixed2(%v) = %v, want %v", c.in, got, c.fixed2)
		}
	}
	if jsRound(-2.5) != -2 || jsRound(2.5) != 3 || jsRound(0.49999999999999994) != 0 {
		t.Error("jsRound does not round halves up")
	}
	for _, c := range []struct {
		in   string
		want float64
	}{{"7", 7}, {"-3", -3}, {"2.5", 2}, {"1e+21", 1}, {"\t12px", 12}} {
		if got := parseIntJS(c.in); got != c.want {
			t.Errorf("parseInt(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	if !math.IsNaN(parseIntJS("NaN")) {
		t.Error("parseInt of no digits is not NaN")
	}
	keys := []string{"b", "10", "a", "2", "01", "-1"}
	jsKeyOrder(keys)
	if want := []string{"2", "10", "b", "a", "01", "-1"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("key order %v, want %v", keys, want)
	}
	if got := jsonbKeys(map[string]int{"userId": 1, "email": 1, "z": 1, "3": 1, "ab": 1}); !reflect.DeepEqual(got, []string{"3", "z", "ab", "email", "userId"}) {
		t.Errorf("jsonb key order %v", got)
	}
	if b, _ := json.Marshal([]Num{Num(math.NaN()), 1.5}); string(b) != "[null,1.5]" {
		t.Errorf("Num JSON %s", b)
	}
}

// forma2's getFormattedDateTimeString tests (lib/response/utils.test.ts, lib/utils/datetime.test.ts) and the en-CA names Node prints.
func TestFormatTimestamp(t *testing.T) {
	winter := time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at   time.Time
		zone string
		want string
	}{
		{winter, "UTC", "2026-01-01 20:00:00 UTC"},
		{winter, "Asia/Manila", "2026-01-02 04:00:00 GMT+8"},
		{winter, "Nowhere/City", "2026-01-01 20:00:00 UTC"},
		{winter, "", "2026-01-01 20:00:00 UTC"},
		{winter, "America/New_York", "2026-01-01 15:00:00 EST"},
		{summer, "America/New_York", "2026-07-15 08:00:00 EDT"},
		{summer, "Europe/London", "2026-07-15 13:00:00 GMT+1"},
		{winter, "Europe/London", "2026-01-01 20:00:00 GMT"},
		{winter, "Asia/Kolkata", "2026-01-02 01:30:00 IST"},
		{winter, "Australia/Adelaide", "2026-01-02 06:30:00 GMT+10:30"},
		{winter, "Pacific/Marquesas", "2026-01-01 10:30:00 GMT-9:30"},
		{winter, "Etc/GMT+5", "2026-01-01 15:00:00 GMT-5"},
		{winter, "Etc/UTC", "2026-01-01 20:00:00 UTC"},
		{time.Date(2026, 1, 1, 23, 59, 59, 999_000_000, time.UTC), "UTC", "2026-01-01 23:59:59 UTC"},
	} {
		if got := FormatTimestamp(c.at, c.zone); got != c.want {
			t.Errorf("%s in %q: %q, want %q", c.at, c.zone, got, c.want)
		}
	}
}

func TestExportFileName(t *testing.T) {
	now := time.Date(2026, 9, 19, 23, 30, 45, 0, time.FixedZone("JST", 9*3600))
	for name, want := range map[string]string{
		"Test Survey":         "export-test_survey-2026-09-19-14-30-45-utc.csv",
		"IT Survey: Q3/2026!": "export-it_survey_q3_2026_-2026-09-19-14-30-45-utc.csv",
		"Café-über.v2":        "export-caf_-_ber.v2-2026-09-19-14-30-45-utc.csv",
		"":                    "export--2026-09-19-14-30-45-utc.csv",
	} {
		if got := exportFileName(name, "csv", now); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
	if got := exportFileName(strings.Repeat("a", 300), "xlsx", now); got != "export-"+strings.Repeat("a", 255)+"-2026-09-19-14-30-45-utc.xlsx" {
		t.Errorf("long name: %q", got)
	}
}

func TestProcessResponseData(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{`"text"`, "text"},
		{`42`, "42"},
		{`0.1`, "0.1"},
		{`["a", "", null, 3, "b"]`, "a; 3; b"},
		{`{"zip": "123", "city": "", "10": "x", "street": "Main"}`, "10: x\nzip: 123\nstreet: Main"},
		{`true`, ""},
		{`null`, ""},
	} {
		var v any
		if err := json.Unmarshal([]byte(c.in), &v); err != nil {
			t.Fatal(err)
		}
		if got := processResponseData(v); got != c.want {
			t.Errorf("%s: %q, want %q", c.in, got, c.want)
		}
	}
	if processResponseData(member(nil, "absent")) != "" {
		t.Error("an absent answer is not empty")
	}
}

// forma2's extractChoiceIdsFromResponse tests.
func TestChoiceIDs(t *testing.T) {
	multi := &element{Element: surveydoc.Element{Type: surveydoc.ElementMultipleChoiceMulti, Choices: []surveydoc.Choice{
		{ID: "choice-1", Label: surveydoc.I18nString{"default": "Option 1", "es": "Opción 1"}},
		{ID: "choice-2", Label: surveydoc.I18nString{"default": "Option 2", "es": "Opción 2"}},
		{ID: "choice-3", Label: surveydoc.I18nString{"default": "Option 3", "es": "Opción 3"}},
	}}}
	single := &element{Element: surveydoc.Element{Type: surveydoc.ElementMultipleChoiceSingle, Choices: []surveydoc.Choice{
		{ID: "choice-a", Label: surveydoc.I18nString{"default": "Choice A", "fr": "Choix A"}},
		{ID: "choice-b", Label: surveydoc.I18nString{"default": "Choice B", "fr": "Choix B"}},
	}}}
	for _, c := range []struct {
		el     *element
		answer any
		lang   string
		want   []string
	}{
		{multi, []any{"Option 1", "Option 3"}, "default", []string{"choice-1", "choice-3"}},
		{multi, []any{"Opción 1", "Opción 2"}, "es", []string{"choice-1", "choice-2"}},
		{multi, []any{"Opción 1", "Option 2"}, "default", []string{"choice-1", "choice-2"}},
		{multi, []any{"Option 1", "Non-existent option", "Option 3"}, "default", []string{"choice-1", "other", "choice-3"}},
		{multi, []any{"Option 1", ""}, "default", []string{"choice-1"}},
		{multi, []any{}, "default", nil},
		{multi, "Option 1", "default", []string{"choice-1"}},
		{multi, 123.0, "default", nil},
		{multi, map[string]any{"invalid": "object"}, "default", nil},
		{single, "Choice A", "default", []string{"choice-a"}},
		{single, "Choix B", "fr", []string{"choice-b"}},
		{single, "Choix A", "default", []string{"choice-a"}},
		{single, "", "default", nil},
	} {
		if got := choiceIDs(c.el, c.answer, c.lang); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v in %s: %v, want %v", c.answer, c.lang, got, c.want)
		}
	}
}

func TestExportColumnsFollowTheSurvey(t *testing.T) {
	sv := &surveyDoc{Survey: &response.Survey{}}
	if err := sv.readBlocks(json.RawMessage(`[{"id":"b","elements":[
		{"id":"q1","type":"multipleChoiceSingle","headline":{"default":"Question 1"},"choices":[{"id":"1","label":{"default":"Option 1"}}]},
		{"id":"q2","type":"matrix","headline":{"default":"Matrix Question"},"rows":[{"id":"r1","label":{"default":"Row 1"}},{"id":"r2","label":{"default":"<p>Row 2</p>"}}],"columns":[]},
		{"id":"q3","type":"openText","headline":{"default":"About #recall:q4/fallback:x# and #recall:country/fallback:y#"}},
		{"id":"q4","type":"ranking","headline":{"default":"Rank #recall:q3/fallback:z#"},"choices":[]},
		{"id":"q5","type":"pictureSelection","headline":{"default":""},"choices":[]}
	]}]`)); err != nil {
		t.Fatal(err)
	}
	sv.Fields = []embeddeddata.Linked{
		{Field: embeddeddata.Field{StorageKey: "v2", Name: "renamed_tier", Source: embeddeddata.SourceComputed}},
		{Field: embeddeddata.Field{StorageKey: "url", Name: "url", Source: embeddeddata.SourceIngested}},
		{Field: embeddeddata.Field{StorageKey: "v1", Name: "score", Source: embeddeddata.SourceComputed}},
	}
	cols := newExportColumns(sv, []string{"email"})
	wantElements := [][]string{
		{"1. Question 1", "1. Question 1 - Option ID"},
		{"2. Matrix Question - Row 1", "2. Matrix Question - Row 2"},
		// q4's headline is read before it is rewritten, so its own token shows as ___; q4 then sees q3's rewritten headline.
		{"3. About @Rank ___ and @Country"},
		{"4. Rank @About @Rank ___ and @Country", "4. Rank @About @Rank ___ and @Country - Option ID"},
		{"5. "},
	}
	if !reflect.DeepEqual(cols.elements, wantElements) {
		t.Errorf("element columns %q", cols.elements)
	}
	headers := cols.headers(sv)
	reserved := strings.Join(headers[9:slices.Index(headers, "1. Question 1")], "|")
	if reserved != "Source|Country|Action|Browser|Os|Device Type|Language|Duration Seconds|Finished At|Page Path|Page Referrer|Utm Source|Utm Medium|Utm Campaign|Utm Term|Utm Content|Screen Width|Screen Height|Viewport Width|Viewport Height|Timezone|Locale" {
		t.Errorf("reserved columns %s", reserved)
	}
	if tail := headers[len(headers)-4:]; !reflect.DeepEqual(tail, []string{"renamed_tier", "score", "url", "person.email"}) {
		t.Errorf("tail %v", tail)
	}
	sv.IsCaptureIPEnabled, sv.IsVerifyEmailEnabled, sv.IsAnonymizeResponsesEnabled = true, true, true
	headers = newExportColumns(sv, nil).headers(sv)
	if !strings.Contains(strings.Join(headers, "|"), "|Ip Address|") || headers[len(headers)-1] != "Verified Email" || !strings.Contains(strings.Join(headers, "|"), "|Country|") {
		t.Errorf("capture IP and verify email headers %v", headers)
	}
}

// forma2's convertToCsv and convertToXlsxBuffer tests (lib/utils/file-conversion.test.ts).
func TestSheetWriters(t *testing.T) {
	payloads := []string{`=HYPERLINK("https://evil.tld","Click")`, "+1+1", "-2+3", "@SUM(A1:A2)", "\tleading-tab", "\rleading-cr"}
	rows := [][]any{{"name", "age", "=evil"}, {"Alice", 30.0, nil}, {"Bob", 25.0, ""}, {"Alice = Bob", true, map[string]any{"a": 1.0}}}
	for _, p := range payloads {
		rows = append(rows, []any{p, 0.0, nil})
	}

	var csv bytes.Buffer
	w := newCSVWriter(&csv)
	for _, r := range rows {
		if err := w.row(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimPrefix(csv.String(), "\ufeff"), "\n")
	if !strings.HasPrefix(csv.String(), "\ufeff") || lines[0] != `"name","age","'=evil"` || lines[1] != `"Alice",30,` || lines[2] != `"Bob",25,""` || lines[3] != `"Alice = Bob",true,"{""a"":1}"` {
		t.Errorf("csv %q", csv.String())
	}
	if lines[4] != `"'=HYPERLINK(""https://evil.tld"",""Click"")",0,` {
		t.Errorf("csv formula %q", lines[4])
	}

	var xlsx bytes.Buffer
	xw, err := newXLSXWriter(&xlsx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if err := xw.row(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := xw.close(); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&xlsx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	get := func(cell string) string {
		v, err := f.GetCellValue("Sheet1", cell)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// XLSX keeps the text as given: no literal apostrophe, which a reader would see, but Excel's quote prefix on the cells that start like a formula.
	if get("A1") != "name" || get("C1") != "=evil" || get("B2") != "30" || get("C2") != "" {
		t.Errorf("xlsx header and first row: %q %q %q %q", get("A1"), get("C1"), get("B2"), get("C2"))
	}
	if !xlsxQuotePrefixed(t, f, "C1") || xlsxQuotePrefixed(t, f, "A1") || xlsxQuotePrefixed(t, f, "A4") || xlsxQuotePrefixed(t, f, "B2") {
		t.Errorf("xlsx quote prefix: C1 %v, A1 %v, A4 %v, B2 %v", xlsxQuotePrefixed(t, f, "C1"), xlsxQuotePrefixed(t, f, "A1"), xlsxQuotePrefixed(t, f, "A4"), xlsxQuotePrefixed(t, f, "B2"))
	}
	for i, p := range payloads {
		cell, _ := excelize.CoordinatesToCellName(1, i+5)
		if got := get(cell); got != p {
			t.Errorf("xlsx %s: %q, want %q", cell, got, p)
		}
		if !xlsxQuotePrefixed(t, f, cell) {
			t.Errorf("xlsx %s is not quote-prefixed", cell)
		}
		if formula, _ := f.GetCellFormula("Sheet1", cell); formula != "" {
			t.Errorf("xlsx %s is a formula: %q", cell, formula)
		}
	}
}

// xlsxQuotePrefixed reports whether the cell's format carries Excel's quote prefix, which excelize's Style does not show.
func xlsxQuotePrefixed(t *testing.T, f *excelize.File, cell string) bool {
	t.Helper()
	id, err := f.GetCellStyle("Sheet1", cell)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.GetStyle(id); err != nil {
		t.Fatal(err)
	}
	xf := f.Styles.CellXfs.Xf[id]
	return xf.QuotePrefix != nil && *xf.QuotePrefix
}

// An answer as its export cell: numbers stay numbers, so a spreadsheet can compute with scores; a number input's text becomes the number it holds unless it is not a plain decimal or has more digits than a float64 keeps.
func TestExportAnswer(t *testing.T) {
	number := &element{Element: surveydoc.Element{Type: surveydoc.ElementOpenText, InputType: "number"}}
	text := &element{Element: surveydoc.Element{Type: surveydoc.ElementOpenText}}
	nps := &element{Element: surveydoc.Element{Type: surveydoc.ElementNPS}}
	for _, c := range []struct {
		el     *element
		answer any
		want   any
	}{
		{nps, 10.0, 10.0},
		{nps, 0.0, 0.0},
		{text, 42.0, 42.0},
		{number, "30", 30.0},
		{number, " -12.5 ", -12.5},
		{number, "1e3", 1000.0},
		{number, "007", 7.0},
		{number, "123456789012345", 123456789012345.0},
		{number, "1234567890123456789", "1234567890123456789"},
		{number, "３０", "３０"},
		{number, "0x10", "0x10"},
		{number, "Inf", "Inf"},
		{number, "", ""},
		{text, "30", "30"},
		{text, "+1 for webhooks", "+1 for webhooks"},
		{text, []any{"a", "b"}, "a; b"},
		{number, member(nil, "absent"), ""},
	} {
		if got := exportAnswer(c.el, c.answer); got != c.want {
			t.Errorf("%s %#v: %#v, want %#v", c.el.OpenTextInputType(), c.answer, got, c.want)
		}
	}
}

// The language mapping the summary and the export share.
func TestDefaultLanguageMapping(t *testing.T) {
	if localized(surveydoc.I18nString{"default": "Speed", "de-DE": "Tempo"}, "de-DE") != "Tempo" ||
		localized(surveydoc.I18nString{"default": "Speed", "de-DE": " "}, "de-DE") != "Speed" ||
		localized(surveydoc.I18nString{"default": "Speed"}, "ja-JP") != "Speed" ||
		localized(surveydoc.I18nString{"default": "Speed", "de-DE": "Tempo"}, "default") != "Speed" {
		t.Error("localized does not fall back to the default label as the respondent does")
	}

	single := &element{Element: surveydoc.Element{Type: surveydoc.ElementMultipleChoiceSingle, Choices: []surveydoc.Choice{
		{ID: "fast", Label: surveydoc.I18nString{"default": "Speed", "ja-JP": "速さ"}},
		{ID: "other", Label: surveydoc.I18nString{"default": "Other", "ja-JP": "その他"}},
	}}}
	ranking := &element{Element: surveydoc.Element{Type: surveydoc.ElementRanking, Choices: []surveydoc.Choice{
		{ID: "x", Label: surveydoc.I18nString{"default": "X", "de-DE": "Iks"}},
		{ID: "y", Label: surveydoc.I18nString{"default": "Y", "de-DE": "Ypsilon"}},
	}}}
	for _, c := range []struct {
		el     *element
		answer any
		lang   string
		want   any
	}{
		{single, "速さ", "ja-JP", "Speed"},
		{single, "自分の答え", "ja-JP", "自分の答え"},
		{single, "Speed", "ja-JP", "Speed"},
		{ranking, []any{"Ypsilon", "Iks"}, "de-DE", []any{"Y", "X"}},
		{ranking, "Iks", "de-DE", []any{"X"}},
		{ranking, 3.0, "de-DE", []any{}},
	} {
		if got := translated(c.el, c.answer, c.lang); !reflect.DeepEqual(got, c.want) {
			t.Errorf("translated %v in %s: %#v, want %#v", c.answer, c.lang, got, c.want)
		}
	}

	matrix := &element{Element: surveydoc.Element{Type: surveydoc.ElementMatrix,
		Rows: []surveydoc.Choice{
			{ID: "r1", Label: surveydoc.I18nString{"default": "Speed", "de-DE": "Tempo"}},
			{ID: "r2", Label: surveydoc.I18nString{"default": "Support", "de-DE": ""}},
			{ID: "r3", Label: surveydoc.I18nString{"default": "Price", "de-DE": "Preis"}},
		},
		Columns: []surveydoc.Choice{
			{ID: "c1", Label: surveydoc.I18nString{"default": "Good", "de-DE": "Gut"}},
			{ID: "c2", Label: surveydoc.I18nString{"default": "Bad"}},
		}}}
	cells := map[string]any{"Tempo": "Gut", "Support": "Bad", "Preis": "Mittel", "Speed": "Bad"}
	for _, c := range []struct {
		row    int
		lang   string
		value  any
		column string
		ok     bool
	}{
		{0, "de-DE", "Gut", "c1", true},
		// An untranslated row and column were shown, and stored, with their default labels.
		{1, "de-DE", "Bad", "c2", true},
		{2, "de-DE", "Mittel", "", true},
		{0, "default", "Bad", "c2", true},
		{2, "default", nil, "", false},
	} {
		value, column, ok := matrixAnswer(matrix, cells, matrix.Rows[c.row], c.lang)
		id := ""
		if column != nil {
			id = column.ID
		}
		if value != c.value || id != c.column || ok != c.ok {
			t.Errorf("matrix row %d in %s: %v %q %v, want %v %q %v", c.row, c.lang, value, id, ok, c.value, c.column, c.ok)
		}
	}
}
