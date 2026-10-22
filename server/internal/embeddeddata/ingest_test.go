package embeddeddata

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// The cases of forma2's packages/types/embedded-data-ingest.test.ts. JSON carries no Date instances, NaN or Infinity, so the cases built on those are left out: nothing at this boundary can receive them.

func ingestedField(storageKey, dataType string, locked bool) Linked {
	return Linked{Field: Field{StorageKey: storageKey, Name: storageKey, Source: SourceIngested, DataType: dataType}, Locked: locked}
}

func computedField(storageKey string) Linked {
	return Linked{Field: Field{StorageKey: storageKey, Name: storageKey, Source: SourceComputed, DataType: TypeString}}
}

// obj builds an incoming bag from key, value pairs in order.
func obj(pairs ...any) []jsvalue.Member {
	out := make([]jsvalue.Member, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, jsvalue.Member{Key: pairs[i].(string), Value: pairs[i+1]})
	}
	return out
}

type normalized struct {
	value any
	flag  string
}

func normalize(t *testing.T, v any, dataType string) *normalized {
	t.Helper()
	value, flag, ok := NormalizeValue(v, dataType)
	if !ok {
		return nil
	}
	return &normalized{value, flag}
}

var dataTypes = []string{TypeString, TypeNumber, TypeBoolean, TypeDate}

var candidateInputs = []any{
	"hello", "", "   ", "0", "1", "42", "-3.5", "1e3", " 7 ", "abc", "true", "FALSE", " True ", "yes", "no", "on", "off", "maybe",
	"2026-08-06", "2026-08-06T10:30:00Z", "2026-08-06T10:30:00.123Z", "2026-08-06T10:30:00+02:00", "2026-08-06T10:30:00+0200",
	"2026-08-06T10:30:00", "2026-08-06 10:30", "2026-02-30", "Aug 6, 2026", 0.0, 1.0, 42.0, -3.5, true, false,
}

func TestNormalizeValue(t *testing.T) {
	for _, dataType := range dataTypes {
		for _, v := range []any{nil, []any{"a", "b"}, map[string]any{"row": "column"}} {
			if got := normalize(t, v, dataType); got != nil {
				t.Errorf("%s accepted %#v as %#v; nothing can be stored for it", dataType, v, got)
			}
		}
	}
	cases := []struct {
		in       any
		dataType string
		want     normalized
	}{
		{"gold", TypeString, normalized{"gold", ""}},
		{"", TypeString, normalized{"", ""}},
		{"  padded  ", TypeString, normalized{"  padded  ", ""}},
		{42.0, TypeString, normalized{"42", ""}},
		{true, TypeString, normalized{"true", ""}},

		{42.0, TypeNumber, normalized{42.0, ""}},
		{"42", TypeNumber, normalized{42.0, ""}},
		{" -3.5 ", TypeNumber, normalized{-3.5, ""}},
		{"0", TypeNumber, normalized{0.0, ""}},
		{"abc", TypeNumber, normalized{"abc", FlagCoercionFailed}},
		{true, TypeNumber, normalized{"true", FlagCoercionFailed}},
		{"", TypeNumber, normalized{"", FlagCoercionFailed}},
		{"   ", TypeNumber, normalized{"   ", FlagCoercionFailed}},

		{"maybe", TypeBoolean, normalized{"maybe", FlagCoercionFailed}},
		{2.0, TypeBoolean, normalized{"2", FlagCoercionFailed}},

		{"2026-08-06", TypeDate, normalized{"2026-08-06", ""}},
		{" 2026-08-06 ", TypeDate, normalized{"2026-08-06", ""}},
		{"2026-08-06T10:30:00Z", TypeDate, normalized{"2026-08-06T10:30:00Z", ""}},
		{"2026-08-06T10:30:00+02:00", TypeDate, normalized{"2026-08-06T08:30:00.000Z", ""}},
		{"2026-08-06T10:30:00+0200", TypeDate, normalized{"2026-08-06T08:30:00.000Z", ""}},
		{"2026-08-06T10:30:00", TypeDate, normalized{"2026-08-06T10:30:00.000Z", ""}},
		{"2026-08-06 10:30", TypeDate, normalized{"2026-08-06T10:30:00.000Z", ""}},
		{"Aug 6, 2026", TypeDate, normalized{"Aug 6, 2026", FlagCoercionFailed}},
		{"2026-02-30", TypeDate, normalized{"2026-02-30", FlagCoercionFailed}},
		{1786012200000.0, TypeDate, normalized{"1786012200000", FlagCoercionFailed}},
	}
	for _, v := range []any{true, 1.0, "true", "TRUE", " True ", "1", "yes", "on"} {
		cases = append(cases, struct {
			in       any
			dataType string
			want     normalized
		}{v, TypeBoolean, normalized{"true", ""}})
	}
	for _, v := range []any{false, 0.0, "false", "FALSE", " False ", "0", "no", "off"} {
		cases = append(cases, struct {
			in       any
			dataType string
			want     normalized
		}{v, TypeBoolean, normalized{"false", ""}})
	}
	for _, c := range cases {
		got := normalize(t, c.in, c.dataType)
		if got == nil || !reflect.DeepEqual(*got, c.want) {
			t.Errorf("NormalizeValue(%#v, %s) = %#v, want %#v", c.in, c.dataType, got, c.want)
		}
	}
}

// The invariant that keeps the write and read seams from drifting: whatever is accepted without a flag reads back as its type.
func TestEverythingAcceptedWithoutAFlagReadsBack(t *testing.T) {
	for _, dataType := range dataTypes {
		for _, in := range candidateInputs {
			n := normalize(t, in, dataType)
			if n == nil || n.flag != "" {
				continue
			}
			if _, ok := Coerce(n.value, dataType); !ok {
				t.Errorf("%s accepted %#v as %#v, which the read seam refuses", dataType, in, n.value)
			}
		}
	}
}

func TestNormalizationIsIdempotent(t *testing.T) {
	for _, dataType := range dataTypes {
		for _, in := range candidateInputs {
			first := normalize(t, in, dataType)
			if first == nil {
				continue
			}
			second := normalize(t, first.value, dataType)
			if second == nil || !reflect.DeepEqual(second.value, first.value) {
				t.Errorf("%s re-normalized %#v differently: %#v then %#v", dataType, in, first.value, second)
			}
		}
	}
}

func TestIngest(t *testing.T) {
	cases := []struct {
		name     string
		incoming []jsvalue.Member
		fields   []Linked
		elements []string
		data     map[string]any
		flags    []Flag
		dropped  []Drop
	}{
		{
			name:     "stores a declared value under its declared key",
			incoming: obj("plan", "gold", "seats", "12"),
			fields:   []Linked{ingestedField("plan", TypeString, false), ingestedField("seats", TypeNumber, false)},
			data:     map[string]any{"plan": "gold", "seats": 12.0},
		},
		{
			name:     "drops a key no ingested field declares",
			incoming: obj("plan", "gold", "rogue", "x"),
			fields:   []Linked{ingestedField("plan", TypeString, false)},
			data:     map[string]any{"plan": "gold"},
			dropped:  []Drop{{"rogue", DropUnknownKey}},
		},
		{
			name:     "only ingested fields are settable: computed keys are dropped",
			incoming: obj("score", "999"),
			fields:   []Linked{computedField("score")},
			data:     map[string]any{},
			dropped:  []Drop{{"score", DropUnknownKey}},
		},
		{
			name:     "a locked field ignores external writes and no default is written",
			incoming: obj("plan", "gold"),
			fields:   []Linked{ingestedField("plan", TypeString, true)},
			data:     map[string]any{},
			dropped:  []Drop{{"plan", DropLockedField}},
		},
		{
			name:   "nothing is written for a field nothing arrived for",
			fields: []Linked{ingestedField("plan", TypeString, false)},
			data:   map[string]any{},
		},
		{
			name:     "stores a bad value raw, flags it, and keeps going",
			incoming: obj("seats", "many", "plan", "gold"),
			fields:   []Linked{ingestedField("seats", TypeNumber, false), ingestedField("plan", TypeString, false)},
			data:     map[string]any{"seats": "many", "plan": "gold"},
			flags:    []Flag{{"seats", FlagCoercionFailed}},
		},
		{
			name:     "drops a non-scalar under a declared key",
			incoming: obj("plan", []any{"gold", "silver"}),
			fields:   []Linked{ingestedField("plan", TypeString, false)},
			data:     map[string]any{},
			dropped:  []Drop{{"plan", DropUnsupportedValue}},
		},
		{
			name:     "stores a __proto__ storage key",
			incoming: obj("__proto__", "gold", "plan", "silver"),
			fields:   []Linked{ingestedField("__proto__", TypeString, false), ingestedField("plan", TypeString, false)},
			data:     map[string]any{"__proto__": "gold", "plan": "silver"},
		},
		{
			name:     "passes a __proto__ question answer through",
			incoming: obj("__proto__", "answer"),
			elements: []string{"__proto__"},
			data:     map[string]any{"__proto__": "answer"},
		},
		{
			name:     "fills a declared field from a differently cased key under the declared spelling",
			incoming: obj("customerref", "abc"),
			fields:   []Linked{ingestedField("CustomerRef", TypeString, false)},
			data:     map[string]any{"CustomerRef": "abc"},
		},
		{
			name:     "an exact match beats a case-insensitive one (case-insensitive key first)",
			incoming: obj("Plan", "a", "plan", "b"),
			fields:   []Linked{ingestedField("plan", TypeString, false)},
			data:     map[string]any{"plan": "b"},
			// The other spelling filled nothing, so it is reported like any key nothing declares.
			dropped: []Drop{{"Plan", DropUnknownKey}},
		},
		{
			name:     "an exact match beats a case-insensitive one (exact key first)",
			incoming: obj("plan", "b", "Plan", "a"),
			fields:   []Linked{ingestedField("plan", TypeString, false)},
			data:     map[string]any{"plan": "b"},
			// The other spelling filled nothing, so it is reported like any key nothing declares.
			dropped: []Drop{{"Plan", DropUnknownKey}},
		},
		{
			name:     "reports a drop under the declared spelling",
			incoming: obj("plan", "gold", "seats", map[string]any{"nested": true}),
			fields:   []Linked{ingestedField("Plan", TypeString, true), ingestedField("Seats", TypeString, false)},
			data:     map[string]any{},
			dropped:  []Drop{{"Plan", DropLockedField}, {"Seats", DropUnsupportedValue}},
		},
		{
			name:     "keys a flag and a drop the same way",
			incoming: obj("seats", "many", "plan", "gold"),
			fields:   []Linked{ingestedField("Seats", TypeNumber, false), ingestedField("Plan", TypeString, true)},
			data:     map[string]any{"Seats": "many"},
			flags:    []Flag{{"Seats", FlagCoercionFailed}},
			dropped:  []Drop{{"Plan", DropLockedField}},
		},
		{
			name:     "one key fills two fields that differ only by case",
			incoming: obj("PLAN", "gold"),
			fields:   []Linked{ingestedField("plan", TypeString, false), ingestedField("Plan", TypeString, false)},
			data:     map[string]any{"plan": "gold", "Plan": "gold"},
		},
		{
			name:     "passes a question answer through untouched",
			incoming: obj("q1", "42", "q2", []any{"a", "b"}, "q3", map[string]any{"row": "column"}),
			fields:   []Linked{ingestedField("plan", TypeNumber, false)},
			elements: []string{"q1", "q2", "q3"},
			data:     map[string]any{"q1": "42", "q2": []any{"a", "b"}, "q3": map[string]any{"row": "column"}},
		},
		{
			name:     "reports a question answer whose shape cannot be stored",
			incoming: obj("q1", "answer", "q2", map[string]any{"nested": map[string]any{"deep": 1.0}}, "q3", nil),
			elements: []string{"q1", "q2", "q3"},
			data:     map[string]any{"q1": "answer"},
			dropped:  []Drop{{"q2", DropUnsupportedValue}, {"q3", DropUnsupportedValue}},
		},
		{
			name:     "never rewrites the answer address, even when a field declares it",
			incoming: obj("q1", "answer"),
			fields:   []Linked{ingestedField("q1", TypeNumber, false)},
			elements: []string{"q1"},
			data:     map[string]any{"q1": "answer"},
			dropped:  []Drop{{"q1", DropElementIDCollision}},
		},
		{
			name:     "a question answer is never rerouted onto a field that matches it only by case",
			incoming: obj("plan", "answer"),
			fields:   []Linked{ingestedField("Plan", TypeString, false)},
			elements: []string{"plan"},
			data:     map[string]any{"plan": "answer"},
		},
		{
			name:     "an empty allow-list drops everything",
			incoming: obj("plan", "gold"),
			data:     map[string]any{},
			dropped:  []Drop{{"plan", DropUnknownKey}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Ingest(c.incoming, c.fields, c.elements)
			if c.flags == nil {
				c.flags = []Flag{}
			}
			if c.dropped == nil {
				c.dropped = []Drop{}
			}
			if !reflect.DeepEqual(got.Data, c.data) || !reflect.DeepEqual(got.Flags, c.flags) || !reflect.DeepEqual(got.Dropped, c.dropped) {
				t.Fatalf("Ingest = %#v\nwant data %#v flags %#v dropped %#v", got, c.data, c.flags, c.dropped)
			}
		})
	}
}

func TestIngestSizeLimits(t *testing.T) {
	note := []Linked{ingestedField("note", TypeString, false)}

	got := Ingest(obj("note", strings.Repeat("a", MaxIngestedValueBytes+100)), note, nil)
	if len(got.Data["note"].(string)) != MaxIngestedValueBytes || !reflect.DeepEqual(got.Flags, []Flag{{"note", FlagTruncated}}) {
		t.Fatalf("oversize value: %d bytes, flags %v", len(got.Data["note"].(string)), got.Flags)
	}

	emoji := strings.Repeat("😀", MaxIngestedValueBytes)
	got = Ingest(obj("note", emoji), note, nil)
	stored := got.Data["note"].(string)
	if stored != strings.Repeat("😀", MaxIngestedValueBytes/4) || !utf8.ValidString(stored) || !reflect.DeepEqual(got.Flags, []Flag{{"note", FlagTruncated}}) {
		t.Fatalf("emoji value cut to %d bytes, flags %v", len(stored), got.Flags)
	}

	exact := strings.Repeat("a", MaxIngestedValueBytes)
	got = Ingest(obj("note", exact), note, nil)
	if got.Data["note"] != exact || len(got.Flags) != 0 {
		t.Fatalf("a value that exactly fits was changed: flags %v", got.Flags)
	}

	got = Ingest(obj("seats", strings.Repeat("a", MaxIngestedValueBytes+1)), []Linked{ingestedField("seats", TypeNumber, false)}, nil)
	if !reflect.DeepEqual(got.Flags, []Flag{{"seats", FlagCoercionFailed}, {"seats", FlagTruncated}}) {
		t.Fatalf("oversize value of the wrong type flagged %v, want both reasons", got.Flags)
	}
}

func TestIngestIsIdempotent(t *testing.T) {
	fields := []Linked{ingestedField("plan", TypeString, false), ingestedField("seats", TypeNumber, false), ingestedField("trial", TypeBoolean, false), ingestedField("signedUpAt", TypeDate, false)}
	first := Ingest(obj("plan", "gold", "seats", "12", "trial", "yes", "signedUpAt", "2026-08-06T10:30:00+02:00", "q1", "a"), fields, []string{"q1"})
	var again []jsvalue.Member
	for _, k := range []string{"plan", "seats", "trial", "signedUpAt", "q1"} {
		again = append(again, jsvalue.Member{Key: k, Value: first.Data[k]})
	}
	second := Ingest(again, fields, []string{"q1"})
	if !reflect.DeepEqual(second.Data, first.Data) || len(second.Flags) != 0 || len(second.Dropped) != 0 {
		t.Fatalf("second run %#v, first %#v", second, first.Data)
	}
}

func TestMergeFlags(t *testing.T) {
	if got := MergeFlags([]Flag{{"seats", FlagCoercionFailed}}, map[string]any{"seats": 12.0}, nil); len(got) != 0 {
		t.Errorf("a rewritten key kept its flags: %v", got)
	}
	got := MergeFlags([]Flag{{"seats", FlagCoercionFailed}}, map[string]any{"plan": "gold"}, []Flag{{"plan", FlagTruncated}})
	if !reflect.DeepEqual(got, []Flag{{"seats", FlagCoercionFailed}, {"plan", FlagTruncated}}) {
		t.Errorf("untouched keys: %v", got)
	}
	if got := MergeFlags([]Flag{{"plan", FlagCoercionFailed}}, map[string]any{}, nil); !reflect.DeepEqual(got, []Flag{{"plan", FlagCoercionFailed}}) {
		t.Errorf("a dropped key lost its flag: %v", got)
	}
}

func TestCoerce(t *testing.T) {
	cases := []struct {
		in       any
		dataType string
		want     any
		ok       bool
	}{
		{"x", TypeString, "x", true},
		{1.5, TypeString, "1.5", true},
		{true, TypeString, "true", true},
		{nil, TypeString, nil, false},
		{jsvalue.Undefined, TypeString, nil, false},
		{" 12 ", TypeNumber, 12.0, true},
		{"", TypeNumber, nil, false},
		{"1e400", TypeNumber, nil, false},
		{math.Inf(1), TypeNumber, nil, false},
		{" TRUE ", TypeBoolean, true, true},
		{"yes", TypeBoolean, nil, false},
		{"2026-08-06", TypeDate, "2026-08-06", true},
		{"2026-08-06T10:30Z", TypeDate, "2026-08-06T10:30Z", true},
		{"2026-08-06T10:30:00+02:00", TypeDate, nil, false},
		{"2026-02-29", TypeDate, nil, false},
		{"2024-02-29", TypeDate, "2024-02-29", true},
	}
	for _, c := range cases {
		got, ok := Coerce(c.in, c.dataType)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Coerce(%#v, %s) = %#v, %v; want %#v, %v", c.in, c.dataType, got, ok, c.want, c.ok)
		}
	}
}

func TestRedactURLQuery(t *testing.T) {
	cases := map[string]string{
		"https://Example.com:443/a/b?email=x#frag":  "https://example.com/a/b",
		"https://example.com?x=1":                   "https://example.com/",
		"http://user:pw@example.com:8080/p?q":       "http://example.com:8080/p",
		"/relative/path?token=1":                    "/relative/path",
		"mailto:someone@example.com?subject=x":      "mailto:someone@example.com",
		"not a url#x":                               "not a url",
		"https://example.com/pricing":               "https://example.com/pricing",
		"http://localhost:3000/s/abc?x=1":           "http://localhost:3000/s/abc",
		"https://example.com/app#access_token=x":    "https://example.com/app",
		"https://app.test/s/abc?suToken=secret#top": "https://app.test/s/abc",
		"": "",
	}
	for in, want := range cases {
		if got := RedactURLQuery(in); got != want {
			t.Errorf("RedactURLQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
