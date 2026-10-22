package clientapi

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// The client API's input schemas are forma2's zod schemas (packages/types), checked here in their declared key order so that a failure reports the same paths and zod 4 messages forma2 did. transformErrorToDetails keyed each issue by its path joined with "."; a later issue on the same path replaced the earlier message.

// absent marks a key the body did not carry: zod's undefined.
type absent struct{}

// object is a parsed body read as zod reads an object: a key the body lacks is absent, and a body that is not an object reads as an empty one, the way forma2 spread it into its input.
type object map[string]any

func asObject(v any) object {
	if m, ok := v.(map[string]any); ok {
		return object(m)
	}
	return object{}
}

func (o object) get(key string) any {
	v, ok := o[key]
	if !ok {
		return absent{}
	}
	return v
}

// issues collects a schema's failures in the order zod reported them.
type issues struct {
	details fields
	// aborted is set by a type failure; zod skips an object's refinements after one.
	aborted bool
}

func (is *issues) add(path, message string) { is.details.set(path, message) }

func (is *issues) typeError(path, expected string, v any) {
	is.add(path, "Invalid input: expected "+expected+", received "+received(v))
	is.aborted = true
}

func (is *issues) failed() bool { return len(is.details) > 0 }

// received is zod 4's name for the type of a parsed value.
func received(v any) string {
	switch x := v.(type) {
	case absent:
		return "undefined"
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		if math.IsNaN(x) {
			return "NaN"
		}
		return "number"
	case []any:
		return "array"
	}
	return "object"
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// Optionality of a field: whether it may be absent (optional) or null (nullable).
type presence int

const (
	required presence = iota
	optional
	nullable
	nullish
)

// skip reports whether v needs no further check under p.
func (p presence) skip(v any) bool {
	switch v.(type) {
	case absent:
		return p == optional || p == nullish
	case nil:
		return p == nullable || p == nullish
	}
	return false
}

// str is z.string() under p; ok is false when v is absent, null or not a string.
func (is *issues) str(path string, v any, p presence) (string, bool) {
	if p.skip(v) {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		is.typeError(path, "string", v)
		return "", false
	}
	return s, true
}

// optString reads a string field that may be absent or null.
func (is *issues) optString(path string, v any, p presence) *string {
	if s, ok := is.str(path, v, p); ok {
		return &s
	}
	return nil
}

// cuid2 is zod's z.cuid2() character set.
var cuid2 = regexp.MustCompile(`^[0-9a-z]+$`)

// isID is ZId, z.cuid2(), widened to the rewrite's ids: a UUID, or a forma2 cuid that resolves to one through the legacy id function.
func isID(s string) bool {
	if _, err := uuid.Parse(s); err == nil && len(s) == 36 {
		return true
	}
	return cuid2.MatchString(s)
}

// id is ZId under p.
func (is *issues) id(path string, v any, p presence) (string, bool) {
	s, ok := is.str(path, v, p)
	if ok && !isID(s) {
		is.add(path, "Invalid cuid2")
		return s, false
	}
	return s, ok
}

func (is *issues) boolean(path string, v any, p presence) *bool {
	if p.skip(v) {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		is.typeError(path, "boolean", v)
		return nil
	}
	return &b
}

func finite(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok && !math.IsInf(f, 0) && !math.IsNaN(f)
}

// number is z.number(): finite numbers only.
func (is *issues) number(path string, v any, p presence) *float64 {
	if p.skip(v) {
		return nil
	}
	f, ok := finite(v)
	if !ok {
		is.typeError(path, "number", v)
		return nil
	}
	return &f
}

// record reads a z.record(z.string(), value) under p, with its keys in the order the body sent them; each entry is checked by value.
func (is *issues) record(path string, v any, raw json.RawMessage, p presence, value func(path string, v any)) ([]jsvalue.Member, bool) {
	if p.skip(v) {
		return nil, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		is.typeError(path, "record", v)
		return nil, false
	}
	members, err := jsvalue.DecodeObject(raw)
	if err != nil {
		// The body parsed, so its members decode; ±Infinity (an out of range number) is the one value encoding/json refuses, and a record holding one fails its check below.
		members = nil
		for k, e := range m {
			members = append(members, jsvalue.Member{Key: k, Value: e})
		}
	}
	before := len(is.details)
	for _, mem := range members {
		value(join(path, mem.Key), m[mem.Key])
	}
	return members, len(is.details) == before
}

// dataValue is ZResponseDataValue: a string, a number, an array of strings or a record of strings.
func dataValue(v any) bool {
	switch x := v.(type) {
	case string:
		return true
	case float64:
		_, ok := finite(x)
		return ok
	case []any:
		for _, e := range x {
			if _, ok := e.(string); !ok {
				return false
			}
		}
		return true
	case map[string]any:
		for _, e := range x {
			if _, ok := e.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}

// responseData is ZResponseData.
func (is *issues) responseData(path string, v any, raw json.RawMessage, p presence) ([]jsvalue.Member, bool) {
	return is.record(path, v, raw, p, func(path string, v any) {
		if !dataValue(v) {
			is.add(path, "Invalid input")
		}
	})
}

// variables is ZResponseVariables: strings and numbers.
func (is *issues) variables(path string, v any, raw json.RawMessage) (map[string]any, bool) {
	members, ok := is.record(path, v, raw, optional, func(path string, v any) {
		if _, isString := v.(string); isString {
			return
		}
		if _, isNumber := finite(v); !isNumber {
			is.add(path, "Invalid input")
		}
	})
	if !ok {
		return nil, false
	}
	out := make(map[string]any, len(members))
	for _, m := range members {
		out[m.Key] = v.(map[string]any)[m.Key]
	}
	return out, true
}

// ttc is ZResponseTtcInput: numbers, clamped later by the response module.
func (is *issues) ttc(path string, v any, raw json.RawMessage) (map[string]float64, bool) {
	members, ok := is.record(path, v, raw, optional, func(path string, v any) {
		is.number(path, v, required)
	})
	if !ok {
		return nil, false
	}
	out := make(map[string]float64, len(members))
	for _, m := range members {
		out[m.Key], _ = finite(v.(map[string]any)[m.Key])
	}
	return out, true
}

// coerceDate is z.coerce.date(): whatever `new Date(value)` makes of the value must be a valid date.
func (is *issues) coerceDate(path string, v any) {
	if _, ok := v.(absent); ok {
		return
	}
	valid := false
	switch x := v.(type) {
	case nil, bool:
		valid = true
	case float64:
		valid = math.Abs(x) <= 8.64e15
	case string:
		_, valid = jsvalue.ParseDate(x)
	case []any:
		_, valid = jsvalue.ParseDate(jsvalue.String(x))
	}
	if !valid {
		is.add(path, "Invalid input: expected date, received Date")
		is.aborted = true
	}
}

// metaFields are ZResponseMeta's keys in declared order, with their types.
var metaFields = []struct {
	key    string
	number bool
}{
	{"pagePath", false}, {"pageReferrer", false}, {"utmSource", false}, {"utmMedium", false}, {"utmCampaign", false}, {"utmTerm", false},
	{"utmContent", false}, {"screenWidth", true}, {"screenHeight", true}, {"viewportWidth", true}, {"viewportHeight", true},
	{"timezone", false}, {"locale", false}, {"source", false}, {"url", false},
}

// meta is ZResponseMeta, optional. Only its keys are kept: unknown keys are stripped, as zod strips them.
func (is *issues) meta(path string, v any) map[string]any {
	if _, ok := v.(absent); ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		is.typeError(path, "object", v)
		return nil
	}
	o := object(m)
	out := map[string]any{}
	for _, f := range metaFields {
		value := o.get(f.key)
		if f.number {
			if n := is.number(join(path, f.key), value, optional); n != nil {
				out[f.key] = *n
			}
			continue
		}
		if s := is.optString(join(path, f.key), value, optional); s != nil {
			out[f.key] = *s
		}
	}
	if ua := o.get("userAgent"); !isAbsent(ua) {
		uaObj, ok := ua.(map[string]any)
		if !ok {
			is.typeError(join(path, "userAgent"), "object", ua)
		} else {
			for _, k := range []string{"browser", "os", "device"} {
				is.optString(join(join(path, "userAgent"), k), object(uaObj).get(k), optional)
			}
		}
	}
	for _, k := range []string{"country", "action", "ipAddress"} {
		if s := is.optString(join(path, k), o.get(k), optional); s != nil {
			out[k] = *s
		}
	}
	return out
}

func isAbsent(v any) bool {
	_, ok := v.(absent)
	return ok
}

// tooBig is zod 4's default message for a string over its maximum length.
func tooBig(max int) string {
	return "Too big: expected string to have <=" + strconv.Itoa(max) + " characters"
}

// jsTrim is String.prototype.trim, which z.string().trim() applies before its checks.
func jsTrim(s string) string { return strings.TrimFunc(s, jsvalue.IsWhitespace) }
