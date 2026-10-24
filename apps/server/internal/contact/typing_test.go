package contact

import (
	"math"
	"testing"
	"time"
)

// Ported from forma2's detect-attribute-type.test.ts, attribute-storage.test.ts and validate-attribute-type.test.ts.

func TestDetectCSVType(t *testing.T) {
	cases := map[string]string{
		"2024-01-15":               TypeDate,
		"2024-01-15T10:30:00Z":     TypeDate,
		"2024-01-15T10:30:00.000Z": TypeDate,
		"2023-12-31":               TypeDate,
		"15-01-2024":               TypeDate,
		"01/15/2024":               TypeDate,
		"2024/01/15":               TypeDate,
		"42":                       TypeNumber,
		"3.14":                     TypeNumber,
		"-10":                      TypeNumber,
		"0":                        TypeNumber,
		"  123  ":                  TypeNumber,
		"Infinity":                 TypeNumber,
		"hello":                    TypeString,
		"john@example.com":         TypeString,
		"123abc":                   TypeString,
		"":                         TypeString,
		"   ":                      TypeString,
		"NaN":                      TypeString,
		"2024-13-01":               TypeString,
		"not-a-date":               TypeString,
	}
	for in, want := range cases {
		if got := detectCSVType(in); got != want {
			t.Errorf("detectCSVType(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestDetectSDKType(t *testing.T) {
	cases := []struct {
		in   Value
		want string
	}{
		{Value{Number: 42, IsNumber: true}, TypeNumber},
		{Value{String: "42"}, TypeString},
		{Value{String: "2024-01-15"}, TypeDate},
		{Value{String: "2024-01-15T10:30:00.000Z"}, TypeDate},
		{Value{String: "2024-01-15T10:30:00"}, TypeDate},
		{Value{String: "15-01-2024"}, TypeString},
		{Value{String: "2024-13-45"}, TypeString},
		{Value{String: "hello"}, TypeString},
	}
	for _, c := range cases {
		if got := detectSDKType(c.in); got != c.want {
			t.Errorf("detectSDKType(%+v) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestTryParseDate(t *testing.T) {
	cases := map[string]string{
		"2024-01-15":           "2024-01-15T00:00:00.000Z",
		"2024/01/15":           "2024-01-15T00:00:00.000Z",
		"2024-01-15T10:30:00Z": "2024-01-15T10:30:00.000Z",
		"2024-02-30":           "2024-03-01T00:00:00.000Z",
		"15-01-2024":           "2024-01-15T00:00:00.000Z",
		"15/01/2024":           "2024-01-15T00:00:00.000Z",
		"01-15-2024":           "2024-01-15T00:00:00.000Z",
		"03-04-2024":           "2024-03-04T00:00:00.000Z",
	}
	for in, want := range cases {
		got, ok := tryParseDate(in)
		if !ok || jsISOString(got) != want {
			t.Errorf("tryParseDate(%q) = %s %v, want %s", in, jsISOString(got), ok, want)
		}
	}
	for _, in := range []string{"2024-13-01", "2024-01-15abc", "hello", "1-2"} {
		if got, ok := tryParseDate(in); ok {
			t.Errorf("tryParseDate(%q) = %s, want no date", in, jsISOString(got))
		}
	}
}

func TestPrepareColumns(t *testing.T) {
	str := func(s string) Value { return Value{String: s} }
	num := func(f float64) Value { return Value{Number: f, IsNumber: true} }
	cases := []struct {
		name     string
		in       Value
		dataType string
		want     string
		parsed   bool
	}{
		{"string stays", str("hello"), TypeString, "hello", true},
		{"number to string", num(42), TypeString, "42", true},
		{"number", num(100), TypeNumber, "100", true},
		{"numeric string", str("99.99"), TypeNumber, "99.99", true},
		{"numeric string with whitespace", str("  100  "), TypeNumber, "100", true},
		{"invalid number degrades", str("not a number"), TypeNumber, "not a number", false},
		{"ISO date", str("2024-06-15T10:30:00.000Z"), TypeDate, "2024-06-15T10:30:00.000Z", true},
		{"slashed date", str("2024/06/15"), TypeDate, "2024-06-15T00:00:00.000Z", true},
		{"day-first date", str("15-06-2024"), TypeDate, "2024-06-15T00:00:00.000Z", true},
		{"month-name fallback", str("Jun 15, 2024"), TypeDate, "2024-06-15T00:00:00.000Z", true},
		{"timestamp", num(1718447400000), TypeDate, "2024-06-15T10:30:00.000Z", true},
		{"invalid date degrades", str("not a date"), TypeDate, "not a date", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := prepareColumns(c.in, c.dataType)
			if got.String() != c.want || got.parsedAs(c.dataType) != c.parsed {
				t.Fatalf("got %q parsed=%v, want %q parsed=%v", got.String(), got.parsedAs(c.dataType), c.want, c.parsed)
			}
			n := 0
			for _, set := range []bool{got.Text != nil, got.Number != nil, got.Date != nil} {
				if set {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("%d columns set, the table allows exactly one", n)
			}
		})
	}
}

func TestPrepareCSVColumnsKeepsEmptyTypedCellsEmpty(t *testing.T) {
	for _, dataType := range []string{TypeNumber, TypeDate} {
		got := prepareCSVColumns("", dataType)
		if got.Text == nil || *got.Text != "" {
			t.Errorf("%s: an empty cell became %q", dataType, got.String())
		}
	}
}

func TestReadBack(t *testing.T) {
	f := 3.14159
	d := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	s := "invalid date stored"
	if got := (Columns{Number: &f}).String(); got != "3.14159" {
		t.Errorf("number: %q", got)
	}
	if got := (Columns{Date: &d}).String(); got != "2024-06-15T10:30:00.000Z" {
		t.Errorf("date: %q", got)
	}
	if got := (Columns{Text: &s}).String(); got != s {
		t.Errorf("text: %q", got)
	}
}

func TestValidateValue(t *testing.T) {
	str := func(s string) Value { return Value{String: s} }
	num := func(f float64) Value { return Value{Number: f, IsNumber: true} }
	cases := []struct {
		name     string
		in       Value
		dataType string
		code     string
		message  string
	}{
		{"string accepts string", str("hello"), TypeString, "", ""},
		{"string rejects number", num(1), TypeString, "string_type_mismatch", "Attribute 'k' expects a string but received a number. Pass an actual string value."},
		{"number accepts number", num(42), TypeNumber, "", ""},
		{"number rejects numeric string", str("42"), TypeNumber, "number_type_mismatch", "Attribute 'k' expects a number but received a string. Pass an actual number value (e.g., 123 instead of \"123\")."},
		{"date accepts ISO date", str("2024-01-15"), TypeDate, "", ""},
		{"date accepts ISO date time", str("2024-01-15T10:30:00.000Z"), TypeDate, "", ""},
		{"date rejects day-first", str("15-01-2024"), TypeDate, "date_format_invalid", "Attribute 'k' expects a date in ISO 8601 format (e.g., \"2024-01-15\" or \"2024-01-15T10:30:00.000Z\") or a Date object. Received: \"15-01-2024\""},
		{"date rejects impossible month", str("2024-13-01"), TypeDate, "date_format_invalid", ""},
		{"date rejects number", num(5), TypeDate, "date_unexpected_type", "Attribute 'k' expects a valid date. Received: number value '5'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cols, err := validateValue(c.in, c.dataType, "k")
			if c.code == "" {
				if err != nil {
					t.Fatalf("refused: %+v", err)
				}
				if !cols.parsedAs(c.dataType) {
					t.Fatalf("stored %+v", cols)
				}
				return
			}
			if err == nil || err.Code != c.code || (c.message != "" && err.Message != c.message) {
				t.Fatalf("got %+v, want %s %q", err, c.code, c.message)
			}
		})
	}
}

func TestJSNumberSemantics(t *testing.T) {
	strings := map[float64]string{1e21: "1e+21", 1e-7: "1e-7", 1e15: "1000000000000000", -50: "-50", math.Inf(1): "Infinity", 1.5e-10: "1.5e-10"}
	tenth, fifth := 0.1, 0.2
	strings[tenth+fifth] = "0.30000000000000004"
	strings[0.000001] = "0.000001"
	for in, want := range strings {
		if got := jsNumberString(in); got != want {
			t.Errorf("String(%v) = %q, want %q", in, got, want)
		}
	}
	numbers := []struct {
		in   string
		want float64
	}{{" 12 ", 12}, {"0x1A", 26}, {".5", 0.5}, {"5.", 5}, {"1e3", 1000}, {"+5", 5}, {"\u00a012", 12}}
	for _, n := range numbers {
		if got, ok := jsNumber(n.in); !ok || got != n.want {
			t.Errorf("Number(%q) = %v %v, want %v", n.in, got, ok, n.want)
		}
	}
	for _, in := range []string{"1_0", "-0x1", "abc", "NaN", "1e"} {
		if _, ok := jsNumber(in); ok {
			t.Errorf("Number(%q) parsed, want NaN", in)
		}
	}
}

func TestJSDateSemantics(t *testing.T) {
	valid := map[string]string{
		"2024-01-15T10:30:00":           "2024-01-15T10:30:00.000Z",
		"2024-01-15 10:30":              "2024-01-15T10:30:00.000Z",
		"2024-01-15T10:30:00.123+02:00": "2024-01-15T08:30:00.123Z",
		"2024-01-15T24:00:00Z":          "2024-01-16T00:00:00.000Z",
		"2024-1-5":                      "2024-01-05T00:00:00.000Z",
		"15 January 2024":               "2024-01-15T00:00:00.000Z",
		"0024-01-01":                    "0024-01-01T00:00:00.000Z",
	}
	for in, want := range valid {
		got, ok := jsDate(in)
		if !ok || jsISOString(got) != want {
			t.Errorf("new Date(%q) = %s %v, want %s", in, jsISOString(got), ok, want)
		}
	}
	for _, in := range []string{"2024-02-32", "2024-00-10", "2024-01-15T25:00:00Z", "2024-01-15T10:60:00Z", "2024-01-15abc"} {
		if got, ok := jsDate(in); ok {
			t.Errorf("new Date(%q) = %s, want Invalid Date", in, jsISOString(got))
		}
	}
	if got := jsISOString(jsLocalDate(24, 0, 1)); got != "1924-01-01T00:00:00.000Z" {
		t.Errorf("new Date(24, 0, 1) = %s", got)
	}
}

func TestIdentifiers(t *testing.T) {
	for _, k := range []string{"plan", "a1", "job_description", "a__b"} {
		if !IsSafeIdentifier(k) {
			t.Errorf("%q should be safe", k)
		}
	}
	for _, k := range []string{"", "1a", "_a", "Plan", "first-name", "a b"} {
		if IsSafeIdentifier(k) {
			t.Errorf("%q should not be safe", k)
		}
	}
	titles := map[string]string{"job_description": "Job Description", "api_key": "Api Key", "a__b": "A B", "_lead_": "Lead"}
	for in, want := range titles {
		if got := TitleCase(in); got != want {
			t.Errorf("TitleCase(%q) = %q, want %q", in, got, want)
		}
	}
	if !IsReservedKey(" User_Id ") || IsReservedKey("userId") {
		t.Error("reserved keys are matched trimmed and lowercased, and only in their snake_case form")
	}
	want := "Reserved attribute key(s): user_id, last_name. These keys are reserved for the v5.1 safe-identifier default attribute migration and cannot be created as custom attributes."
	if got := reservedKeyIssue([]string{"user_id", "plan", "LAST_NAME", "user_id"}); got != want {
		t.Errorf("issue %q", got)
	}
	if !isStringMatch("email", "Email") || !isStringMatch("name", "First Name") || isStringMatch("Email Address", "Email") || !isStringMatch("first-name", "First Name") {
		t.Error("isStringMatch must test that the key's name contains the column, ignoring case, spaces, underscores and hyphens")
	}
}
