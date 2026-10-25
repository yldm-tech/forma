package analysis

import (
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// The language mapping the summary and the export share: answers are stored as the labels the respondent saw, in the response's language, and both read them back under the default-language labels, so a translated response counts and exports like one given in the default language.

// localized is the respondent's getLocalizedValue: the label in lang, the default label where it has no translation (an absent or blank one), which is what a response in lang stored.
func localized(s surveydoc.I18nString, lang string) string {
	if l, ok := s[lang]; ok && strings.TrimSpace(l) != "" {
		return l
	}
	return s.Default()
}

// translated is forma2's checkForI18n: a choice answer given in a response language mapped back to the default-language labels. A multi-select or ranking answer becomes a list (a single string a one-element list); an answer that is neither a string nor a list, which forma2 failed on, becomes an empty list. A value no choice has in lang, such as an "Other" text, is kept as given.
func translated(el *element, answer any, lang string) any {
	label := func(v any) any {
		s, ok := v.(string)
		if !ok {
			return v
		}
		for _, c := range el.Choices {
			if l, has := c.Label[lang]; has && l == s {
				if d := c.Label.Default(); d != "" {
					return d
				}
				return s
			}
		}
		return s
	}
	switch el.Type {
	case surveydoc.ElementMultipleChoiceMulti, surveydoc.ElementRanking:
		var values []any
		switch x := answer.(type) {
		case string:
			values = []any{x}
		case []any:
			values = x
		}
		out := make([]any, len(values))
		for i, v := range values {
			out[i] = label(v)
		}
		return out
	case surveydoc.ElementMultipleChoiceSingle:
		return label(answer)
	}
	return answer
}

// matrixAnswer is a matrix row's answer in a response given in lang: the value stored under the row's label as the respondent saw it, absent (false) when the row was not answered, and the column the value names in that language, nil for a value no column has.
func matrixAnswer(el *element, cells map[string]any, row surveydoc.Choice, lang string) (any, *surveydoc.Choice, bool) {
	key := localized(row.Label, lang)
	if key == "" {
		return nil, nil, false
	}
	value, ok := cells[key]
	if !ok {
		return nil, nil, false
	}
	if s, isString := value.(string); isString && s != "" {
		for i, c := range el.Columns {
			if localized(c.Label, lang) == s {
				return value, &el.Columns[i], true
			}
		}
	}
	return value, nil, true
}
