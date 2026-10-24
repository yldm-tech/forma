package mgmtapi

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v1api"
)

// The management APIs' input schemas are forma2's zod schemas, checked here in their declared key order so that a failure reports the paths and zod 4 messages forma2 did. v1 keyed each issue by its path joined with "." (a later issue on a path replacing an earlier one, transformErrorToDetails); v2 lists every issue in order with the custom ones' params as meta (formatZodError).

// absent is a key the input did not carry: zod's undefined.
type absent struct{}

// object is a parsed input read as zod reads an object: a key it lacks is absent.
type object map[string]any

func (o object) get(key string) any {
	v, ok := o[key]
	if !ok {
		return absent{}
	}
	return v
}

func isAbsent(v any) bool {
	_, ok := v.(absent)
	return ok
}

// issue is one failed rule.
type issue struct {
	path    string
	message string
	meta    map[string]any
}

// issues collects a schema's failures in the order zod reported them.
type issues struct {
	list []issue
	// aborted is set by a type failure; zod skips an object's refinements after one.
	aborted bool
}

func (is *issues) add(path, message string) {
	is.list = append(is.list, issue{path: path, message: message})
}

func (is *issues) addMeta(path, message string, meta map[string]any) {
	is.list = append(is.list, issue{path: path, message: message, meta: meta})
}

func (is *issues) typeError(path, expected string, v any) {
	is.add(path, "Invalid input: expected "+expected+", received "+received(v))
	is.aborted = true
}

func (is *issues) failed() bool { return len(is.list) > 0 }

// v1Details is transformErrorToDetails: path to message, the last issue on a path winning.
func (is *issues) v1Details() map[string]any {
	out := map[string]any{}
	for _, i := range is.list {
		out[i.path] = i.message
	}
	return out
}

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

func index(path string, i int) string { return join(path, strconv.Itoa(i)) }

// presence is whether a field may be absent (optional) or null (nullable).
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

// id is z.cuid2() under p, widened to the rewrite's UUIDs.
func (is *issues) id(path string, v any, p presence) (string, bool) {
	s, ok := is.str(path, v, p)
	if ok && !v1api.IsID(s) {
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

// enum is z.enum(options) under p; zod words a one-option enum as a literal.
func (is *issues) enum(path string, v any, p presence, options []string) (string, bool) {
	if p.skip(v) {
		return "", false
	}
	s, ok := v.(string)
	if !ok || !contains(options, s) {
		message := "Invalid option: expected one of " + v1api.QuotedOptions(options)
		if len(options) == 1 {
			message = "Invalid input: expected " + v1api.QuotedOptions(options)
		}
		is.add(path, message)
		is.aborted = true
		return "", false
	}
	return s, true
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

// record is z.record(z.string(), value) under p; each entry is checked by value, which reports under its path.
func (is *issues) record(path string, v any, p presence, value func(path string, v any)) (map[string]any, bool) {
	if p.skip(v) {
		return nil, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		is.typeError(path, "record", v)
		return nil, false
	}
	before := len(is.list)
	for _, k := range sortedKeys(m) {
		value(join(path, k), m[k])
	}
	return m, len(is.list) == before
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

// responseData is ZResponseData under p, the data member.
func (is *issues) responseData(v any, p presence) map[string]any {
	m, ok := is.record("data", v, p, func(path string, v any) {
		if !dataValue(v) {
			is.add(path, "Invalid input")
		}
	})
	if !ok {
		return nil
	}
	return m
}

// variables is ZResponseVariables under p, the variables member: strings and numbers.
func (is *issues) variables(v any, p presence) map[string]any {
	m, ok := is.record("variables", v, p, func(path string, v any) {
		if _, isString := v.(string); isString {
			return
		}
		if _, isNumber := finite(v); !isNumber {
			is.add(path, "Invalid input")
		}
	})
	if !ok {
		return nil
	}
	return m
}

// ttc is the ttc member under p, a record of numbers.
func (is *issues) ttc(v any, p presence) map[string]float64 {
	m, ok := is.record("ttc", v, p, func(path string, v any) {
		is.number(path, v, required)
	})
	if !ok {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, e := range m {
		out[k], _ = finite(e)
	}
	return out
}

// stringRecord is z.record(z.string(), z.string()) under p.
func (is *issues) stringRecord(path string, v any, p presence) (map[string]string, bool) {
	m, ok := is.record(path, v, p, func(path string, v any) {
		is.str(path, v, required)
	})
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(m))
	for k, e := range m {
		out[k], _ = e.(string)
	}
	return out, true
}

// coerceDate is z.coerce.date(): whatever `new Date(value)` makes of the value must be a valid date. An absent value is `new Date(undefined)`, which is not, unless p lets it be absent.
func (is *issues) coerceDate(path string, v any, p presence) *time.Time {
	if isAbsent(v) && (p == optional || p == nullish) {
		return nil
	}
	var t time.Time
	valid := false
	switch x := v.(type) {
	case nil:
		t, valid = time.UnixMilli(0).UTC(), true
	case bool:
		ms := int64(0)
		if x {
			ms = 1
		}
		t, valid = time.UnixMilli(ms).UTC(), true
	case float64:
		if math.Abs(x) <= 8.64e15 {
			t, valid = time.UnixMilli(int64(x)).UTC(), true
		}
	case string:
		t, valid = jsvalue.ParseDate(x)
	}
	if !valid {
		is.add(path, "Invalid input: expected date, received Date")
		is.aborted = true
		return nil
	}
	return &t
}

// metaFields are ZResponseMeta's keys in declared order, with whether each is a number.
var metaFields = []struct {
	key    string
	number bool
}{
	{"pagePath", false}, {"pageReferrer", false}, {"utmSource", false}, {"utmMedium", false}, {"utmCampaign", false}, {"utmTerm", false},
	{"utmContent", false}, {"screenWidth", true}, {"screenHeight", true}, {"viewportWidth", true}, {"viewportHeight", true},
	{"timezone", false}, {"locale", false}, {"source", false}, {"url", false},
}

// meta is ZResponseMeta under p (v1's packages/types shape): its known keys kept, unknown ones stripped as zod strips them, userAgent with its browser, os and device.
func (is *issues) meta(path string, v any, p presence) map[string]any {
	if p.skip(v) {
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
	if ua := is.userAgent(join(path, "userAgent"), o.get("userAgent")); ua != nil {
		out["userAgent"] = ua
	}
	for _, k := range []string{"country", "action", "ipAddress"} {
		if s := is.optString(join(path, k), o.get(k), optional); s != nil {
			out[k] = *s
		}
	}
	return out
}

func (is *issues) userAgent(path string, v any) map[string]any {
	if isAbsent(v) {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		is.typeError(path, "object", v)
		return nil
	}
	out := map[string]any{}
	for _, k := range []string{"browser", "os", "device"} {
		if s := is.optString(join(path, k), object(m).get(k), optional); s != nil {
			out[k] = *s
		}
	}
	return out
}

// jsTrim is String.prototype.trim, which z.string().trim() applies before its checks.
func jsTrim(s string) string { return strings.TrimFunc(s, jsvalue.IsWhitespace) }

// sortedKeys are an object's keys in a stable order; the decoded body no longer has the order they were sent in.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
