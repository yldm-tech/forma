package answers

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite messages.json from packages/surveys/locales")

// The messages are a copy of the survey runtime's bundles; this keeps the copy identical. After editing a bundle, run go test ./internal/response/answers -run TestMessagesMatchSurveyLocales -update.
func TestMessagesMatchSurveyLocales(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "..", "packages", "surveys", "locales")
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no survey locales in %s: %v", dir, err)
	}
	want := map[string]map[string]string{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var bundle struct {
			Errors map[string]any `json:"errors"`
		}
		if err := json.Unmarshal(raw, &bundle); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		locale := strings.TrimSuffix(filepath.Base(f), ".json")
		want[locale] = map[string]string{}
		for _, key := range messageKeys {
			if s, ok := bundle.Errors[key].(string); ok {
				want[locale][key] = s
			}
		}
	}
	encoded, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if *update {
		if err := os.WriteFile("messages.json", encoded, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if !bytes.Equal(messagesJSON, encoded) {
		t.Fatal("messages.json differs from packages/surveys/locales; run go test ./internal/response/answers -run TestMessagesMatchSurveyLocales -update")
	}
	if len(messages[fallbackBundle]) != len(messageKeys) {
		t.Fatalf("the English bundle has %d of the %d messages", len(messages[fallbackBundle]), len(messageKeys))
	}
}

func TestTranslatorResolvesTheRuntimeBundle(t *testing.T) {
	cases := map[string]string{
		"de-DE": "Bitte füllen Sie dieses Feld aus", "de": "Bitte füllen Sie dieses Feld aus", "de-AT": "Bitte füllen Sie dieses Feld aus",
		"en": "Please fill out this field", "fr-FR": "Veuillez remplir ce champ", "xx": "Please fill out this field", "": "Please fill out this field",
	}
	for language, want := range cases {
		if got := translator(language)("please_fill_out_this_field", nil); got != want {
			t.Errorf("%q: %q, want %q", language, got, want)
		}
	}
	if got := translator("en-US")("min_length", map[string]string{"min": "3"}); got != "Please enter at least 3 characters" {
		t.Fatalf("parameters: %q", got)
	}
}
