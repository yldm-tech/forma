package surveydoc

import (
	"strconv"
	"strings"
)

// Issue is one rule a document breaks, in zod's terms: where (Path, from the survey root), why (Message), and any structured detail (Params, such as the invalidLanguageCodes of a translation issue). Code is the v3 invalid_params code when the rule has one.
type Issue struct {
	Path    []any          `json:"path"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params,omitempty"`
	Code    string         `json:"code,omitempty"`
}

// Name joins the path with dots, as v3's invalid_params names and zod's issue.path.join(".") did.
func (i Issue) Name() string {
	parts := make([]string, len(i.Path))
	for k, p := range i.Path {
		switch v := p.(type) {
		case int:
			parts[k] = strconv.Itoa(v)
		case string:
			parts[k] = v
		}
	}
	return strings.Join(parts, ".")
}

// path builds an issue path.
func path(parts ...any) []any { return parts }

func extend(base []any, parts ...any) []any {
	out := make([]any, 0, len(base)+len(parts))
	out = append(out, base...)
	return append(out, parts...)
}

// issues accumulates issues in the order the rules find them.
type issues []Issue

func (is *issues) add(p []any, message string) {
	*is = append(*is, Issue{Path: p, Message: message})
}
