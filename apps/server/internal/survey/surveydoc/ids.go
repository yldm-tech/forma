package surveydoc

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

// ForbiddenIDs are forma2's FORBIDDEN_IDS: names the link survey reads from its own URL, so no element or hidden field may take them.
var ForbiddenIDs = []string{"userId", "source", "suid", "end", "start", "welcomeCard", "hidden", "verifiedEmail", "multiLanguage", "embed", "verify"}

// linkSurveySystemParams are forma2's LINK_SURVEY_SYSTEM_PARAMS, spelled as the link survey reads them.
var linkSurveySystemParams = []string{"suId", "suToken", "lang", "preview", "startAt", "skipPrefilled", "offlineSupport"}

// linkSurveySystemParamKeys is forma2's LINK_SURVEY_SYSTEM_PARAM_KEYS, in its iteration order.
var linkSurveySystemParamKeys = append(append([]string{}, ForbiddenIDs...), linkSurveySystemParams...)

// reservedDeclaredFieldNames is forma2's RESERVED_DECLARED_FIELD_NAMES, lowercased.
var reservedDeclaredFieldNames = func() map[string]bool {
	out := map[string]bool{}
	for _, k := range linkSurveySystemParamKeys {
		out[strings.ToLower(k)] = true
	}
	return out
}()

// ReservedFieldNames is forma2's RESERVED_FIELD_NAMES: the auto-captured fields every survey can read by name, lowercased.
var ReservedFieldNames = map[string]bool{
	"source": true, "url": true, "country": true, "action": true, "browser": true, "os": true, "devicetype": true, "ipaddress": true,
	"finished": true, "language": true, "responseid": true, "surveyid": true, "durationseconds": true, "startedat": true, "finishedat": true,
	"pagepath": true, "pagereferrer": true, "utmsource": true, "utmmedium": true, "utmcampaign": true, "utmterm": true, "utmcontent": true,
	"screenwidth": true, "screenheight": true, "viewportwidth": true, "viewportheight": true, "timezone": true, "locale": true,
}

var (
	legacyIDCharset    = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	legacyVariableName = regexp.MustCompile(`^[a-z0-9_]+$`)
	safeIdentifier     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	cuid2Pattern       = regexp.MustCompile(`^[0-9a-z]+$`)
)

func isForbiddenID(id string) bool {
	for _, f := range ForbiddenIDs {
		if f == id {
			return true
		}
	}
	return false
}

// IsLegacyIDCharset is forma2's isLegacyIdCharset.
func IsLegacyIDCharset(s string) bool { return legacyIDCharset.MatchString(s) }

// IsSafeIdentifier is forma2's isSafeIdentifier: a lowercase letter, then lowercase letters, digits and underscores.
func IsSafeIdentifier(s string) bool { return safeIdentifier.MatchString(s) }

// IsCUID2 is zod's cuid2 check, the shape of block, ending, variable and logic ids.
func IsCUID2(s string) bool { return cuid2Pattern.MatchString(s) }

// NewID returns a new id of cuid2's shape: 24 characters, a lowercase letter then lowercase letters and digits, from a cryptographic source. Block, ending, variable and logic ids inside a survey document take this shape.
func NewID() string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	out := make([]byte, len(raw))
	out[0] = letters[int(raw[0])%len(letters)]
	for i := 1; i < len(raw); i++ {
		out[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(out)
}

// ValidateIDCode is forma2's TValidateIdErrorCode.
type ValidateIDCode string

const (
	IDEmpty             ValidateIDCode = "empty"
	IDDuplicate         ValidateIDCode = "duplicate"
	IDReserved          ValidateIDCode = "reserved"
	IDHasSpaces         ValidateIDCode = "has_spaces"
	IDInvalidChars      ValidateIDCode = "invalid_chars"
	IDNotSafeIdentifier ValidateIDCode = "not_safe_identifier"
)

// IDError is a refused name and why.
type IDError struct {
	Code  ValidateIDCode
	Field string
}

// ValidateID is forma2's validateId for an element id, hidden field id or variable name. strict applies the rule for newly declared fields: reserved names in any casing, the auto-captured field names, and the safe-identifier shape.
func ValidateID(field string, existing []string, strict bool) *IDError {
	if strings.TrimSpace(field) == "" {
		return &IDError{IDEmpty, field}
	}
	for _, id := range existing {
		if strings.EqualFold(id, field) {
			return &IDError{IDDuplicate, field}
		}
	}
	reserved := isForbiddenID(field)
	if strict {
		lower := strings.ToLower(field)
		reserved = reservedDeclaredFieldNames[lower] || ReservedFieldNames[lower]
	}
	if reserved {
		return &IDError{IDReserved, field}
	}
	if strings.Contains(field, " ") {
		return &IDError{IDHasSpaces, field}
	}
	if !IsLegacyIDCharset(field) {
		return &IDError{IDInvalidChars, field}
	}
	if strict && !IsSafeIdentifier(field) {
		return &IDError{IDNotSafeIdentifier, field}
	}
	return nil
}

// DeclaredFields are a survey's variables and hidden fields as a save carries them; a nil carrier is "not declared", as forma2's null and undefined were.
type DeclaredFields struct {
	Variables    *[]Variable
	HiddenFields *HiddenFields
}

func (d DeclaredFields) names() []string {
	var out []string
	if d.Variables != nil {
		for _, v := range *d.Variables {
			out = append(out, v.Name)
		}
	}
	if d.HiddenFields != nil {
		out = append(out, d.HiddenFields.FieldIDs...)
	}
	return out
}

// ValidateNewDeclaredFieldNames is forma2's validateNewDeclaredFieldNames: every name the save declares that the survey did not already have (case-insensitively) must pass the strict ValidateID; each refused name is reported once.
func ValidateNewDeclaredFieldNames(existing, incoming DeclaredFields) []IDError {
	grandfathered := map[string]bool{}
	for _, n := range existing.names() {
		grandfathered[strings.ToLower(n)] = true
	}
	seen := map[string]bool{}
	var out []IDError
	for _, name := range incoming.names() {
		lower := strings.ToLower(name)
		if grandfathered[lower] || seen[lower] {
			continue
		}
		seen[lower] = true
		if err := ValidateID(name, nil, true); err != nil {
			out = append(out, *err)
		}
	}
	return out
}

type orderedNames struct {
	keys  []string
	names map[string]string
}

func namesOf(variables *[]Variable, hidden *HiddenFields) (orderedNames, orderedNames) {
	vars := orderedNames{names: map[string]string{}}
	if variables != nil {
		for _, v := range *variables {
			lower := strings.ToLower(v.Name)
			if _, ok := vars.names[lower]; !ok {
				vars.keys = append(vars.keys, lower)
			}
			vars.names[lower] = v.Name
		}
	}
	fields := orderedNames{names: map[string]string{}}
	if hidden != nil {
		for _, id := range hidden.FieldIDs {
			lower := strings.ToLower(id)
			if _, ok := fields.names[lower]; !ok {
				fields.keys = append(fields.keys, lower)
			}
			fields.names[lower] = id
		}
	}
	return vars, fields
}

// ValidateNewDeclaredFieldClashes is forma2's validateNewDeclaredFieldClashes: a variable may not share its name with a hidden field (case-insensitively) unless the survey already had exactly that clash. A carrier the save omits falls back to the stored one.
func ValidateNewDeclaredFieldClashes(existing, incoming DeclaredFields) []IDError {
	currentVars, currentFields := namesOf(existing.Variables, existing.HiddenFields)
	nextVariables, nextHidden := incoming.Variables, incoming.HiddenFields
	if nextVariables == nil {
		nextVariables = existing.Variables
	}
	if nextHidden == nil {
		nextHidden = existing.HiddenFields
	}
	nextVars, nextFields := namesOf(nextVariables, nextHidden)
	var out []IDError
	for _, lower := range nextVars.keys {
		hiddenID, ok := nextFields.names[lower]
		if !ok {
			continue
		}
		_, hadVar := currentVars.names[lower]
		_, hadField := currentFields.names[lower]
		if hadVar && hadField {
			continue
		}
		field := nextVars.names[lower]
		if hadVar {
			field = hiddenID
		}
		out = append(out, IDError{Code: IDDuplicate, Field: field})
	}
	return out
}

// ValidateNewDeclaredFields is forma2's validateNewDeclaredFields: the name rules, then the clashes, without reporting a refused name twice.
func ValidateNewDeclaredFields(existing, incoming DeclaredFields) []IDError {
	reserved := ValidateNewDeclaredFieldNames(existing, incoming)
	refused := map[string]bool{}
	for _, e := range reserved {
		refused[strings.ToLower(e.Field)] = true
	}
	out := reserved
	for _, e := range ValidateNewDeclaredFieldClashes(existing, incoming) {
		if !refused[strings.ToLower(e.Field)] {
			out = append(out, e)
		}
	}
	return out
}

var declaredFieldNameReasons = map[ValidateIDCode]string{
	IDEmpty:             "it must not be empty",
	IDHasSpaces:         "it must not contain spaces",
	IDInvalidChars:      "it may contain only letters, numbers, underscores and hyphens",
	IDNotSafeIdentifier: "it must start with a lowercase letter and contain only lowercase letters, numbers and underscores",
	IDDuplicate:         "a second field in this survey would carry that name, and recall and logic address fields by name",
}

func describeReservedReason(field string) string {
	for _, key := range linkSurveySystemParamKeys {
		if strings.EqualFold(key, field) {
			return fmt.Sprintf("it collides with \"%s\", a name the link-survey URL contract reserves for itself, so a hidden field under that name is never filled from the URL", key)
		}
	}
	if ReservedFieldNames[strings.ToLower(field)] {
		return "it names an auto-captured system field that every survey can already read by name, so a second field under that name would be ambiguous in recall and logic"
	}
	return "it is a reserved name"
}

// DescribeDeclaredFieldNameError is forma2's describeDeclaredFieldNameError.
func DescribeDeclaredFieldNameError(e IDError) string {
	reason := declaredFieldNameReasons[e.Code]
	if e.Code == IDReserved {
		reason = describeReservedReason(e.Field)
	}
	return fmt.Sprintf("Field name \"%s\" cannot be used: %s.", e.Field, reason)
}

// DescribeDeclaredFieldNameErrors is forma2's describeDeclaredFieldNameErrors, the message of the save's InvalidInputError.
func DescribeDeclaredFieldNameErrors(errs []IDError) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = DescribeDeclaredFieldNameError(e)
	}
	return strings.Join(parts, " ") + " Fields a survey already has keep working; this applies to newly added names only."
}
