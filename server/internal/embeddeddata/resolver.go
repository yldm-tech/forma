package embeddeddata

import (
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// This file is the read side of Embedded Data, forma2's packages/types/embedded-data-resolver.ts: the catalog of reserved fields every response carries, the coercion every stored value is read back through, and the projections logic, quotas and filters see. Values are JSON as encoding/json decodes it, with jsvalue.Undefined for an absent key.

// Privacy of a reserved field under the survey's anonymize setting.
const (
	PrivacyKeep        = "keep"
	PrivacyDrop        = "drop"
	PrivacyRedactQuery = "redactQuery"
)

// ValueResponse is the persisted response a reserved field reads, forma2's TEmbeddedValueResponse. Data, Variables and Meta are the stored JSON objects; TTC holds milliseconds.
type ValueResponse struct {
	ID        string
	SurveyID  string
	CreatedAt time.Time
	UpdatedAt time.Time
	Finished  bool
	Language  *string
	Data      map[string]any
	Variables map[string]any
	TTC       map[string]any
	Meta      map[string]any
}

// ReservedField is one entry of RESERVED_FIELD_CATALOG: a value every response carries, addressed by Name and read rather than stored.
type ReservedField struct {
	Name     string
	DataType string
	Privacy  string
	read     func(r ValueResponse) any
}

func member(m map[string]any, key string) any {
	if v, ok := m[key]; ok {
		return v
	}
	return jsvalue.Undefined
}

func metaField(key string) func(r ValueResponse) any {
	return func(r ValueResponse) any { return member(r.Meta, key) }
}

func userAgentField(key string) func(r ValueResponse) any {
	return func(r ValueResponse) any {
		ua, ok := r.Meta["userAgent"].(map[string]any)
		if !ok {
			return jsvalue.Undefined
		}
		return member(ua, key)
	}
}

// jsRound is Math.round: halves round towards positive infinity.
func jsRound(f float64) float64 { return math.Floor(f + 0.5) }

// ReservedCatalog is RESERVED_FIELD_CATALOG, in its order.
var ReservedCatalog = []ReservedField{
	{Name: "source", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("source")},
	{Name: "url", DataType: TypeString, Privacy: PrivacyRedactQuery, read: metaField("url")},
	{Name: "country", DataType: TypeString, Privacy: PrivacyDrop, read: metaField("country")},
	{Name: "action", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("action")},
	{Name: "browser", DataType: TypeString, Privacy: PrivacyDrop, read: userAgentField("browser")},
	{Name: "os", DataType: TypeString, Privacy: PrivacyDrop, read: userAgentField("os")},
	{Name: "deviceType", DataType: TypeString, Privacy: PrivacyDrop, read: userAgentField("device")},
	{Name: "ipAddress", DataType: TypeString, Privacy: PrivacyDrop, read: metaField("ipAddress")},
	{Name: "finished", DataType: TypeBoolean, Privacy: PrivacyKeep, read: func(r ValueResponse) any { return r.Finished }},
	{Name: "language", DataType: TypeString, Privacy: PrivacyKeep, read: func(r ValueResponse) any {
		if r.Language == nil {
			return nil
		}
		return *r.Language
	}},
	{Name: "responseId", DataType: TypeString, Privacy: PrivacyKeep, read: func(r ValueResponse) any { return r.ID }},
	{Name: "surveyId", DataType: TypeString, Privacy: PrivacyKeep, read: func(r ValueResponse) any { return r.SurveyID }},
	// Total time to complete, in seconds: ttc._total holds milliseconds and only exists on finished responses.
	{Name: "durationSeconds", DataType: TypeNumber, Privacy: PrivacyKeep, read: func(r ValueResponse) any {
		if total, ok := r.TTC["_total"].(float64); ok {
			return jsRound(total / 1000)
		}
		return jsvalue.Undefined
	}},
	{Name: "startedAt", DataType: TypeDate, Privacy: PrivacyKeep, read: func(r ValueResponse) any { return r.CreatedAt }},
	{Name: "finishedAt", DataType: TypeDate, Privacy: PrivacyKeep, read: func(r ValueResponse) any { return r.UpdatedAt }},
	{Name: "pagePath", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("pagePath")},
	{Name: "pageReferrer", DataType: TypeString, Privacy: PrivacyRedactQuery, read: metaField("pageReferrer")},
	{Name: "utmSource", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("utmSource")},
	{Name: "utmMedium", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("utmMedium")},
	{Name: "utmCampaign", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("utmCampaign")},
	{Name: "utmTerm", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("utmTerm")},
	{Name: "utmContent", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("utmContent")},
	{Name: "screenWidth", DataType: TypeNumber, Privacy: PrivacyKeep, read: metaField("screenWidth")},
	{Name: "screenHeight", DataType: TypeNumber, Privacy: PrivacyKeep, read: metaField("screenHeight")},
	{Name: "viewportWidth", DataType: TypeNumber, Privacy: PrivacyKeep, read: metaField("viewportWidth")},
	{Name: "viewportHeight", DataType: TypeNumber, Privacy: PrivacyKeep, read: metaField("viewportHeight")},
	{Name: "timezone", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("timezone")},
	{Name: "locale", DataType: TypeString, Privacy: PrivacyKeep, read: metaField("locale")},
}

// ReservedPrivacy is the privacy of the reserved field named name, "" for a name the catalog does not hold.
func ReservedPrivacy(name string) string {
	for _, f := range ReservedCatalog {
		if f.Name == name {
			return f.Privacy
		}
	}
	return ""
}

// isISODateOrDateTime is z.union([z.iso.date(), z.iso.datetime()]): a calendar date, or a UTC date and time.
func isISODateOrDateTime(s string) bool {
	if len(s) == len(time.DateOnly) {
		_, err := time.Parse(time.DateOnly, s)
		return err == nil
	}
	if !isoDateTime.MatchString(s) {
		return false
	}
	_, err := time.Parse(time.DateOnly, s[:10])
	return err == nil
}

// Coerce is coerceToEmbeddedDataType: the value as the declared type reads it (a string, a float64 or a bool), or false when it does not read as that type.
func Coerce(v any, dataType string) (any, bool) {
	if jsvalue.IsNullish(v) {
		return nil, false
	}
	switch dataType {
	case TypeString:
		switch x := v.(type) {
		case string:
			return x, true
		case float64:
			if math.IsInf(x, 0) || math.IsNaN(x) {
				return nil, false
			}
			return jsvalue.FormatNumber(x), true
		case bool:
			return jsvalue.String(x), true
		case time.Time:
			return jsvalue.ISOString(x), true
		}
	case TypeNumber:
		var n float64
		switch x := v.(type) {
		case float64:
			n = x
		case string:
			if jsvalue.Trim(x) == "" {
				return nil, false
			}
			n = jsvalue.ParseNumber(x)
		default:
			return nil, false
		}
		if math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, false
		}
		return n, true
	case TypeBoolean:
		switch x := v.(type) {
		case bool:
			return x, true
		case string:
			switch strings.ToLower(jsvalue.Trim(x)) {
			case "true":
				return true, true
			case "false":
				return false, true
			}
		}
	case TypeDate:
		switch x := v.(type) {
		case time.Time:
			return jsvalue.ISOString(x), true
		case string:
			if isISODateOrDateTime(x) {
				return x, true
			}
		}
	}
	return nil, false
}

// specialSchemes are the WHATWG URL schemes with a tuple origin; every other scheme's origin is the opaque "null".
var specialSchemes = map[string]string{"http": "80", "https": "443", "ws": "80", "wss": "443", "ftp": "21"}

// RedactURLQuery is redactUrlQuery: an absolute URL keeps its origin and path, without query, fragment or credentials; anything else is cut at its first "?" or "#".
func RedactURLQuery(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		scheme := strings.ToLower(u.Scheme)
		if defaultPort, special := specialSchemes[scheme]; special {
			host := strings.ToLower(u.Hostname())
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			if port := u.Port(); port != "" && port != defaultPort {
				host += ":" + port
			}
			path := u.EscapedPath()
			if path == "" {
				path = "/"
			}
			return scheme + "://" + host + path
		}
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// Project is projectReservedValues: every entry's value on the response, coerced to its type, a boolean as its text and a redactQuery field redacted. An entry that does not resolve is left out.
func Project(entries []ReservedField, r ValueResponse) map[string]any {
	values := map[string]any{}
	for _, e := range entries {
		v, ok := Coerce(e.read(r), e.DataType)
		if !ok {
			continue
		}
		if b, isBool := v.(bool); isBool {
			v = jsvalue.String(b)
		}
		if s, isString := v.(string); isString && e.Privacy == PrivacyRedactQuery {
			v = RedactURLQuery(s)
		}
		values[e.Name] = v
	}
	return values
}

// MergeReserved is mergeReservedValues: the response's data over the reserved values, so an answer or ingested value of the same name wins.
func MergeReserved(reserved, data map[string]any) map[string]any {
	out := make(map[string]any, len(reserved)+len(data))
	for k, v := range reserved {
		out[k] = v
	}
	for k, v := range data {
		out[k] = v
	}
	return out
}

// DropShadowed is dropShadowedReservedEntries: the catalog without the entries a declared name shadows.
func DropShadowed(entries []ReservedField, declared []string) []ReservedField {
	shadowed := make(map[string]bool, len(declared))
	for _, d := range declared {
		shadowed[d] = true
	}
	out := make([]ReservedField, 0, len(entries))
	for _, e := range entries {
		if !shadowed[e.Name] {
			out = append(out, e)
		}
	}
	return out
}

// ShadowingNames is listShadowingNames: the storage keys of the survey's fields and its element ids, every name that takes precedence over a reserved field of the same name.
func ShadowingNames(fields []Linked, elementIDs []string) []string {
	out := make([]string, 0, len(fields)+len(elementIDs))
	for _, f := range fields {
		out = append(out, f.StorageKey)
	}
	return append(out, elementIDs...)
}

// ServerValues is buildServerEmbeddedValues: the full reserved catalog minus what the survey's own fields and elements shadow, projected from the persisted response, with the response's data over it.
func ServerValues(r ValueResponse, fields []Linked, elementIDs []string) map[string]any {
	return MergeReserved(Project(DropShadowed(ReservedCatalog, ShadowingNames(fields, elementIDs)), r), r.Data)
}

// Linked is forma2's TLinkedEmbeddedField: a field definition and the key the survey's link stores its value under.
type Linked struct {
	Field
	Locked bool
}

// OfSource is the fields of one source, in order.
func OfSource(fields []Linked, source string) []Linked {
	var out []Linked
	for _, f := range fields {
		if f.Source == source {
			out = append(out, f)
		}
	}
	return out
}

// FindComputed is findComputedEmbeddedField.
func FindComputed(computed []Linked, storageKey string) *Linked {
	for i := range computed {
		if computed[i].StorageKey == storageKey {
			return &computed[i]
		}
	}
	return nil
}

// LogicVariableValue is getLogicVariableValue: a computed field's value as logic reads it. A number field reads as its number, 0 when unset or not numeric; any other field as its value, "" when unset or falsy. A key the survey does not compute is Undefined.
func LogicVariableValue(computed []Linked, storageKey string, variables map[string]any) any {
	f := FindComputed(computed, storageKey)
	if f == nil {
		return jsvalue.Undefined
	}
	v := member(variables, storageKey)
	if f.DataType == TypeNumber {
		n := jsvalue.Number(v)
		if math.IsNaN(n) {
			return 0.0
		}
		return n
	}
	if !truthy(v) {
		return ""
	}
	return v
}

// truthy is JavaScript's Boolean(v).
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return v != jsvalue.Undefined
}
