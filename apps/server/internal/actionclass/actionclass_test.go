package actionclass

import (
	"encoding/json"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

func fieldErrors(t *testing.T, err error) []apperr.FieldError {
	t.Helper()
	e, ok := apperr.As(err)
	if !ok || e.Code != apperr.CodeValidation {
		t.Fatalf("not a validation error: %v", err)
	}
	return e.Details["fields"].([]apperr.FieldError)
}

func TestParseNoCodeConfig(t *testing.T) {
	valid := map[string]string{
		`null`:                                `null`,
		`{"type":"pageView","urlFilters":[]}`: `{"type":"pageView","urlFilters":[]}`,
		// Values are trimmed and fields of other types dropped, as zod's parse output was.
		`{"type":"pageView","urlFilters":[{"value":"  /pricing ","rule":"startsWith"}],"urlFiltersConnector":"and","elementSelector":{"cssSelector":"#a"},"timeInSeconds":5,"extra":1}`: `{"type":"pageView","urlFilters":[{"value":"/pricing","rule":"startsWith"}],"urlFiltersConnector":"and"}`,
		`{"type":"click","urlFilters":[],"elementSelector":{"cssSelector":" .buy ","innerHtml":""}}`:                                                                                    `{"type":"click","urlFilters":[],"elementSelector":{"cssSelector":".buy","innerHtml":""}}`,
		`{"type":"click","urlFilters":[],"elementSelector":{"innerHtml":"Buy"}}`:                                                                                                        `{"type":"click","urlFilters":[],"elementSelector":{"innerHtml":"Buy"}}`,
		`{"type":"pageDwell","urlFilters":[],"timeInSeconds":3600}`:                                                                                                                     `{"type":"pageDwell","urlFilters":[],"timeInSeconds":3600}`,
		`{"type":"pageDwell","urlFilters":[],"timeInSeconds":1.0}`:                                                                                                                      `{"type":"pageDwell","urlFilters":[],"timeInSeconds":1}`,
		`{"type":"exitIntent","urlFilters":[{"value":"https://example.com/a","rule":"exactMatch"},{"value":"http://localhost:3000","rule":"notMatch"}]}`:                                `{"type":"exitIntent","urlFilters":[{"value":"https://example.com/a","rule":"exactMatch"},{"value":"http://localhost:3000","rule":"notMatch"}]}`,
		`{"type":"fiftyPercentScroll","urlFilters":[{"value":"http://10.0.0.1/x","rule":"startsWith"},{"value":"https://[::1]/","rule":"exactMatch"}]}`:                                 `{"type":"fiftyPercentScroll","urlFilters":[{"value":"http://10.0.0.1/x","rule":"startsWith"},{"value":"https://[::1]/","rule":"exactMatch"}]}`,
		// Rules that do not compare a whole URL take any text.
		`{"type":"pageView","urlFilters":[{"value":"checkout","rule":"contains"},{"value":"^/a(b|c)$","rule":"matchesRegex"},{"value":"x","rule":"notContains"},{"value":"y","rule":"endsWith"}]}`: `{"type":"pageView","urlFilters":[{"value":"checkout","rule":"contains"},{"value":"^/a(b|c)$","rule":"matchesRegex"},{"value":"x","rule":"notContains"},{"value":"y","rule":"endsWith"}]}`,
	}
	for in, want := range valid {
		got, err := ParseNoCodeConfig(json.RawMessage(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if want == "null" {
			if got != nil {
				t.Fatalf("%s: %s", in, got)
			}
			continue
		}
		if string(got) != want {
			t.Fatalf("%s:\n got %s\nwant %s", in, got, want)
		}
	}

	invalid := map[string]apperr.FieldError{
		`{"urlFilters":[]}`:                    {Field: "noCodeConfig.type", Rule: "required"},
		`{"type":"hover","urlFilters":[]}`:     {Field: "noCodeConfig.type", Rule: "oneof", Param: "click pageView exitIntent fiftyPercentScroll pageDwell"},
		`{"type":"pageView"}`:                  {Field: "noCodeConfig.urlFilters", Rule: "required"},
		`{"type":"pageView","urlFilters":"x"}`: {Field: "noCodeConfig", Rule: "type"},
		`{"type":"pageView","urlFilters":[{"value":" ","rule":"contains"}]}`:                     {Field: "noCodeConfig.urlFilters.0.value", Rule: "min", Param: "1"},
		`{"type":"pageView","urlFilters":[{"value":"a","rule":"is"}]}`:                           {Field: "noCodeConfig.urlFilters.0.rule", Rule: "oneof", Param: "exactMatch contains startsWith endsWith notMatch notContains matchesRegex"},
		`{"type":"pageView","urlFilters":[{"value":"example.com","rule":"exactMatch"}]}`:         {Field: "noCodeConfig.urlFilters.0.value", Rule: "url"},
		`{"type":"pageView","urlFilters":[{"value":"ftp://example.com","rule":"startsWith"}]}`:   {Field: "noCodeConfig.urlFilters.0.value", Rule: "url"},
		`{"type":"pageView","urlFilters":[{"value":"https://intranet","rule":"notMatch"}]}`:      {Field: "noCodeConfig.urlFilters.0.value", Rule: "url"},
		`{"type":"pageView","urlFilters":[{"value":"javascript:alert(1)","rule":"exactMatch"}]}`: {Field: "noCodeConfig.urlFilters.0.value", Rule: "url"},
		`{"type":"pageView","urlFilters":[],"urlFiltersConnector":"xor"}`:                        {Field: "noCodeConfig.urlFiltersConnector", Rule: "oneof", Param: "or and"},
		`{"type":"click","urlFilters":[]}`:                                                       {Field: "noCodeConfig.elementSelector", Rule: "required"},
		`{"type":"click","urlFilters":[],"elementSelector":{"cssSelector":"  ","innerHtml":""}}`: {Field: "noCodeConfig.elementSelector", Rule: "element_selector"},
		`{"type":"pageDwell","urlFilters":[]}`:                                                   {Field: "noCodeConfig.timeInSeconds", Rule: "required"},
		`{"type":"pageDwell","urlFilters":[],"timeInSeconds":0}`:                                 {Field: "noCodeConfig.timeInSeconds", Rule: "min", Param: "1"},
		`{"type":"pageDwell","urlFilters":[],"timeInSeconds":3601}`:                              {Field: "noCodeConfig.timeInSeconds", Rule: "max", Param: "3600"},
		`{"type":"pageDwell","urlFilters":[],"timeInSeconds":1.5}`:                               {Field: "noCodeConfig.timeInSeconds", Rule: "int"},
		`[]`: {Field: "noCodeConfig", Rule: "type"},
	}
	for in, want := range invalid {
		_, err := ParseNoCodeConfig(json.RawMessage(in))
		if fields := fieldErrors(t, err); len(fields) != 1 || fields[0] != want {
			t.Fatalf("%s: %+v, want %+v", in, fields, want)
		}
	}
}

func TestValidateInput(t *testing.T) {
	key := "  checkout  "
	in := Input{Name: "  Checkout ", Type: TypeCode, Key: &key, NoCodeConfig: json.RawMessage(`{}`)}
	if err := ValidateInput(&in, json.RawMessage(`{"type":"pageView","urlFilters":[]}`)); err != nil {
		t.Fatal(err)
	}
	if in.Name != "Checkout" || *in.Key != "checkout" || in.NoCodeConfig != nil {
		t.Fatalf("code input %+v", in)
	}

	in = Input{Name: "Pricing", Type: TypeNoCode, Key: &key}
	if err := ValidateInput(&in, json.RawMessage(`{"type":"pageView","urlFilters":[]}`)); err != nil {
		t.Fatal(err)
	}
	if in.Key != nil || string(in.NoCodeConfig) != `{"type":"pageView","urlFilters":[]}` {
		t.Fatalf("no-code input %+v", in)
	}

	blank := " "
	for _, tc := range []struct {
		in   Input
		want apperr.FieldError
	}{
		{Input{Name: "  ", Type: TypeCode}, apperr.FieldError{Field: "name", Rule: "min", Param: "1"}},
		{Input{Name: "A", Type: TypeCode, Key: &blank}, apperr.FieldError{Field: "key", Rule: "min", Param: "1"}},
		{Input{Name: "A", Type: "automatic"}, apperr.FieldError{Field: "type", Rule: "oneof", Param: "code noCode"}},
	} {
		if fields := fieldErrors(t, ValidateInput(&tc.in, nil)); len(fields) != 1 || fields[0] != tc.want {
			t.Fatalf("%+v: %+v", tc.in, fields)
		}
	}
	// A code action may have no key, and a no-code one no configuration.
	for _, in := range []Input{{Name: "A", Type: TypeCode}, {Name: "A", Type: TypeNoCode}} {
		if err := ValidateInput(&in, json.RawMessage(`null`)); err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
	}
}
