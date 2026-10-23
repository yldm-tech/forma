package answers

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/langcode"
)

// messagesJSON is the errors namespace of every packages/surveys/locales bundle, reduced to the keys the checks use; TestMessagesMatchSurveyLocales keeps it identical.
//
//go:embed messages.json
var messagesJSON []byte

// messageKeys are the runtime's errors keys the checks use.
var messageKeys = []string{
	"all_options_must_be_ranked", "all_rows_must_be_answered", "file_extension_must_be", "file_extension_must_not_be", "invalid_format",
	"is_between", "is_earlier_than", "is_greater_than", "is_later_than", "is_less_than", "is_not_between", "max_length", "max_selections",
	"max_value", "min_length", "min_selections", "min_value", "minimum_options_ranked", "minimum_rows_answered",
	"please_enter_a_valid_email_address", "please_enter_a_valid_phone_number", "please_enter_a_valid_url", "please_fill_out_this_field",
	"value_must_contain", "value_must_equal", "value_must_not_contain", "value_must_not_equal",
}

// fallbackBundle is the runtime's DEFAULT_SURVEY_LANGUAGE_CODE, the last bundle every lookup falls back to.
const fallbackBundle = "en-US"

var messages = func() map[string]map[string]string {
	var m map[string]map[string]string
	if err := json.Unmarshal(messagesJSON, &m); err != nil {
		panic("decode embedded answer messages: " + err.Error())
	}
	return m
}()

// translate returns the message for a key with its parameters filled in.
type translate func(key string, params map[string]string) string

// translator is the runtime's getTranslations for one language: the bundle the language resolves to (its own, its canonical form's, or its language's default tag's, so de and de-AT read de-DE), then English.
func translator(language string) translate {
	bundles := []string{fallbackBundle}
	if b := langcode.RuntimeBundle(language); b != "" && b != fallbackBundle {
		bundles = []string{b, fallbackBundle}
	}
	return func(key string, params map[string]string) string {
		text := "errors." + key
		for _, b := range bundles {
			if s, ok := messages[b][key]; ok {
				text = s
				break
			}
		}
		for k, v := range params {
			text = strings.ReplaceAll(text, "{"+k+"}", v)
		}
		return text
	}
}
