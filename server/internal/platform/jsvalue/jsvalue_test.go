package jsvalue

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestNumber(t *testing.T) {
	cases := []struct {
		in   any
		want float64
	}{
		{nil, 0}, {true, 1}, {false, 0}, {" 42 ", 42}, {"", 0}, {"1e3", 1000}, {"0x10", 16}, {"-Infinity", math.Inf(-1)},
		{[]any{}, 0}, {[]any{"5"}, 5}, {"abc", math.NaN()}, {[]any{"1", "2"}, math.NaN()}, {map[string]any{}, math.NaN()}, {Undefined, math.NaN()},
	}
	for _, c := range cases {
		got := Number(c.in)
		if got != c.want && !(math.IsNaN(got) && math.IsNaN(c.want)) {
			t.Errorf("Number(%#v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "null"}, {Undefined, "undefined"}, {true, "true"}, {1.5, "1.5"}, {1e21, "1e+21"}, {1e-7, "1e-7"}, {100.0, "100"},
		{[]any{"a", nil, 2.0}, "a,,2"}, {map[string]any{"a": "b"}, "[object Object]"},
	}
	for _, c := range cases {
		if got := String(c.in); got != c.want {
			t.Errorf("String(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStrictEqual(t *testing.T) {
	arr := []any{"a"}
	cases := []struct {
		a, b any
		want bool
	}{
		{"a", "a", true}, {1.0, 1.0, true}, {"1", 1.0, false}, {nil, Undefined, false}, {Undefined, Undefined, true},
		{arr, arr, true}, {arr, []any{"a"}, false}, {math.NaN(), math.NaN(), false}, {"a", arr, false},
	}
	for _, c := range cases {
		if got := StrictEqual(c.a, c.b); got != c.want {
			t.Errorf("StrictEqual(%#v, %#v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"2024-01-15", "2024-01-15T00:00:00.000Z", true},
		{"2024-01-15T10:30:00+02:00", "2024-01-15T08:30:00.000Z", true},
		{"2024-01-15T10:30", "2024-01-15T10:30:00.000Z", true},
		{"2024/01/15", "2024-01-15T00:00:00.000Z", true},
		{"Jan 15, 2024", "2024-01-15T00:00:00.000Z", true},
		{"2024-13-01", "", false},
		{"not a date", "", false},
	}
	for _, c := range cases {
		got, ok := ParseDate(c.in)
		if ok != c.ok || (ok && ISOString(got) != c.want) {
			t.Errorf("ParseDate(%q) = %s, %v; want %s, %v", c.in, ISOString(got), ok, c.want, c.ok)
		}
	}
	if s := ISOString(time.Date(12345, 1, 1, 0, 0, 0, 0, time.UTC)); s != "+012345-01-01T00:00:00.000Z" {
		t.Errorf("ISOString of an extended year = %s", s)
	}
}

func TestDecodeObjectEnumeratesAsJavaScript(t *testing.T) {
	members, err := DecodeObject([]byte(`{"b": 1, "10": "x", "a": [1], "2": null, "b": 2, "01": true}`))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, m := range members {
		keys = append(keys, m.Key)
	}
	if got, want := fmt.Sprint(keys), "[2 10 b a 01]"; got != want {
		t.Fatalf("keys %s, want %s", got, want)
	}
	if members[2].Value != 2.0 {
		t.Fatalf("repeated key kept %v, want the last value", members[2].Value)
	}
	if _, err := DecodeObject([]byte(`[1]`)); err == nil {
		t.Fatal("an array decoded as an object")
	}
}
