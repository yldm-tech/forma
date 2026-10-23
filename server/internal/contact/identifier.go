package contact

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// safeIdentifier is forma2's isSafeIdentifier (packages/types/safe-identifier.ts): a lowercase letter, then lowercase letters, digits and underscores.
var safeIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// IsSafeIdentifier reports whether key may name a new contact attribute key.
func IsSafeIdentifier(key string) bool {
	return safeIdentifier.MatchString(key)
}

// TitleCase is forma2's formatSnakeCaseToTitleCase, the display name a new key gets from its snake_case key: "job_description" becomes "Job Description". Empty segments are dropped, so "a__b" does not render with a double space.
func TitleCase(key string) string {
	var words []string
	for word := range strings.SplitSeq(key, "_") {
		if word == "" {
			continue
		}
		r, size := utf8.DecodeRuneInString(word)
		words = append(words, string(unicode.ToUpper(r))+word[size:])
	}
	return strings.Join(words, " ")
}

// reservedKeys are the safe identifiers forma2 kept back for its planned migration of the default keys from camelCase (apps/web/modules/contacts/lib/attribute-key-policy.ts); they cannot be created as custom keys.
var reservedKeys = []string{"user_id", "first_name", "last_name"}

// ReservedKeysText lists the reserved keys as forma2's messages print them.
var ReservedKeysText = strings.Join(reservedKeys, ", ")

// ReservedKeyValidationMessage is the message of the create-key form's reserved-key check.
var ReservedKeyValidationMessage = "Key is reserved for the v5.1 safe-identifier default attribute migration. Reserved keys: " + ReservedKeysText + "."

func normalizeKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

// IsReservedKey reports whether key, trimmed and lowercased, is reserved.
func IsReservedKey(key string) bool {
	n := normalizeKey(key)
	for _, r := range reservedKeys {
		if n == r {
			return true
		}
	}
	return false
}

// reservedAmong returns the distinct reserved keys among keys, normalized, in their first-seen order.
func reservedAmong(keys []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range keys {
		n := normalizeKey(k)
		if n == "" || !IsReservedKey(n) || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// reservedKeyIssue is forma2's getReservedFutureDefaultAttributeKeyIssue.
func reservedKeyIssue(keys []string) string {
	return "Reserved attribute key(s): " + strings.Join(reservedAmong(keys), ", ") + ". These keys are reserved for the v5.1 safe-identifier default attribute migration and cannot be created as custom attributes."
}

// isStringMatch is forma2's isStringMatch (apps/web/lib/utils/helper.ts), which the CSV import uses to suggest a key for a column: with spaces, underscores and hyphens removed and both lowercased, value must contain query.
func isStringMatch(query, value string) bool {
	strip := strings.NewReplacer(" ", "", "_", "", "-", "")
	return strings.Contains(strip.Replace(strings.ToLower(value)), strip.Replace(strings.ToLower(query)))
}

// ReservedKeysIssue is forma2's getReservedFutureDefaultAttributeKeyIssue: the message the v2 API and the bulk upload refuse reserved keys with, naming them lowercased and once each.
func ReservedKeysIssue(keys []string) string {
	var named []string
	for _, k := range keys {
		n := normalizeKey(k)
		if n != "" && IsReservedKey(n) && !slices.Contains(named, n) {
			named = append(named, n)
		}
	}
	return "Reserved attribute key(s): " + strings.Join(named, ", ") + ". These keys are reserved for the v5.1 safe-identifier default attribute migration and cannot be created as custom attributes."
}
