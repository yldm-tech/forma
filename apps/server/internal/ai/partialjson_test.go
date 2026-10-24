package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFixJSON(t *testing.T) {
	cases := map[string]string{
		``:                               ``,
		`{`:                              `{}`,
		`{"na`:                           `{}`,
		`{"name"`:                        `{}`,
		`{"name":`:                       `{}`,
		`{"name": "On`:                   `{"name": "On"}`,
		`{"name": "On\`:                  `{"name": "On"}`,
		`{"name": "On\n`:                 `{"name": "On\n"}`,
		`{"name": "Onboarding",`:         `{"name": "Onboarding"}`,
		`{"a": [`:                        `{"a": []}`,
		`{"a": ["x", "y`:                 `{"a": ["x", "y"]}`,
		`{"a": [1, 2`:                    `{"a": [1, 2]}`,
		`{"a": 12.`:                      `{"a": 12}`,
		`{"a": -`:                        `{}`,
		`{"a": tr`:                       `{"a": true}`,
		`{"a": fa`:                       `{"a": false}`,
		`{"a": n`:                        `{"a": null}`,
		`{"a": null, "b": {"c": [{"d": `: `{"a": null, "b": {"c": [{}]}}`,
		`[`:                              `[]`,
		`{"x": "ü€`:                      `{"x": "ü€"}`,
		`{"done": true}`:                 `{"done": true}`,
	}
	for in, want := range cases {
		if got := fixJSON(in); got != want {
			t.Errorf("fixJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePartialJSON(t *testing.T) {
	cases := []struct {
		in   string
		want any
		ok   bool
	}{
		{`{"name": "Onb`, map[string]any{"name": "Onb"}, true},
		{`{"blocks": [{"name": "A", "questions": [{"type": "open`, map[string]any{"blocks": []any{map[string]any{"name": "A", "questions": []any{map[string]any{"type": "open"}}}}}, true},
		{`{"a": 1}`, map[string]any{"a": float64(1)}, true},
		{`{"a": -`, map[string]any{}, true},
		{``, nil, false},
	}
	for _, tc := range cases {
		got, ok := parsePartialJSON(tc.in)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			encoded, _ := json.Marshal(got)
			t.Errorf("parsePartialJSON(%q) = %s, %v", tc.in, encoded, ok)
		}
	}
}
