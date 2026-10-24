package embeddeddata

import (
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
)

// This file is the write side of Embedded Data, forma2's packages/types/embedded-data-ingest.ts (ENG-1845): which keys of an incoming response.data are stored, under which key and in which form. Every server boundary that writes response data runs it, because the renderer's own filtering is never trusted.

// MaxIngestedValueBytes is the UTF-8 byte ceiling of one ingested string value; a longer one is cut on a code point boundary and flagged.
const MaxIngestedValueBytes = 16 * 1024

// Reasons a stored value will not read back as its declared type (TIngestFlagReason).
const (
	FlagCoercionFailed = "coercion_failed"
	FlagTruncated      = "truncated"
)

// Reasons an incoming key did not become a stored value (TIngestDropReason). Never an error: a bad key must not block a response.
const (
	DropUnknownKey         = "unknown_key"
	DropLockedField        = "locked_field"
	DropUnsupportedValue   = "unsupported_value"
	DropElementIDCollision = "element_id_collision"
)

// Flag is one thing worth remembering about one stored key, keyed by the storage key. It is persisted in responses.ingest_flags.
type Flag struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// Drop is an incoming key that was not stored: the declared storage key when a declared field is involved, the incoming key for unknown_key.
type Drop struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// IngestResult is what to store (accepted values under their storage keys, plus question answers untouched), the flags to persist and what was dropped.
type IngestResult struct {
	Data    map[string]any
	Flags   []Flag
	Dropped []Drop
}

var (
	trueTokens  = map[string]bool{"true": true, "1": true, "yes": true, "on": true}
	falseTokens = map[string]bool{"false": true, "0": true, "no": true, "off": true}
	// isoDateTimeInput is an ISO 8601 datetime with the zone optional and a space tolerated for the T, both spellings query strings carry.
	isoDateTimeInput = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})[Tt ](\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?)([Zz]|[+-]\d{2}:?\d{2})?$`)
)

// isResponseDataValue is the shape response.data can hold under a question's id: a string, a finite number, an array of strings or an object of strings.
func isResponseDataValue(v any) bool {
	switch x := v.(type) {
	case string:
		return true
	case float64:
		return !math.IsInf(x, 0) && !math.IsNaN(x)
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

// isScalar is toIngestableScalar for JSON: a string, a number or a boolean can be stored; nothing else can.
func isScalar(v any) bool {
	switch v.(type) {
	case string, float64, bool:
		return true
	}
	return false
}

// normalizeZone is normalizeZoneDesignator: no designator means UTC, and ±HHMM gains its colon.
func normalizeZone(zone string) string {
	if zone == "" || strings.EqualFold(zone, "z") {
		return "Z"
	}
	if strings.Contains(zone, ":") {
		return zone
	}
	return zone[:3] + ":" + zone[3:]
}

func normalizeScalar(v any, dataType string) (any, bool) {
	switch dataType {
	case TypeString:
		return jsvalue.String(v), true
	case TypeNumber:
		switch x := v.(type) {
		case float64:
			return x, !math.IsInf(x, 0) && !math.IsNaN(x)
		case string:
			trimmed := jsvalue.Trim(x)
			if trimmed == "" {
				return nil, false
			}
			n := jsvalue.ParseNumber(trimmed)
			return n, !math.IsInf(n, 0) && !math.IsNaN(n)
		}
	case TypeBoolean:
		switch x := v.(type) {
		case bool:
			return jsvalue.String(x), true
		case float64:
			switch x {
			case 1:
				return "true", true
			case 0:
				return "false", true
			}
		case string:
			token := strings.ToLower(jsvalue.Trim(x))
			if trueTokens[token] {
				return "true", true
			}
			if falseTokens[token] {
				return "false", true
			}
		}
	case TypeDate:
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		trimmed := jsvalue.Trim(s)
		if _, ok := Coerce(trimmed, TypeDate); ok {
			return trimmed, true
		}
		parts := isoDateTimeInput.FindStringSubmatch(trimmed)
		if parts == nil {
			return nil, false
		}
		instant, ok := jsvalue.ParseDate(parts[1] + "T" + parts[2] + normalizeZone(parts[3]))
		if !ok {
			return nil, false
		}
		return jsvalue.ISOString(instant), true
	}
	return nil, false
}

// NormalizeValue is normalizeIngestedValue: one incoming value against one field's type. ok is false only when nothing storable arrived (null or a non-scalar); otherwise the value is stored, in its canonical form or, flagged coercion_failed, as the text that arrived. The canonical form is checked against Coerce, so a value that would read back as unset is always a flagged one.
func NormalizeValue(v any, dataType string) (value any, flag string, ok bool) {
	if !isScalar(v) {
		return nil, "", false
	}
	if candidate, ok := normalizeScalar(v, dataType); ok {
		if _, readable := Coerce(candidate, dataType); readable {
			return candidate, "", true
		}
	}
	return jsvalue.String(v), FlagCoercionFailed, true
}

// truncateUTF8 cuts s to at most maxBytes UTF-8 bytes on a code point boundary.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := 0
	for i, r := range s {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = 3
		}
		if i+size > maxBytes {
			break
		}
		cut = i + size
	}
	return s[:cut]
}

// MatchDeclaredName is matchDeclaredFieldName: the candidate equal to target, else the first that equals it ignoring case.
func MatchDeclaredName(candidates []string, target string) (string, bool) {
	for _, c := range candidates {
		if c == target {
			return c, true
		}
	}
	lower := strings.ToLower(target)
	for _, c := range candidates {
		if strings.ToLower(c) == lower {
			return c, true
		}
	}
	return "", false
}

// Ingest is applyIngestContract with the size limit enforced, as every server boundary runs it: the allow-list of the survey's ingested fields, then locked, then coercion, then the size limit, over an incoming response.data in its enumeration order. A key equal to an element id is a question answer and passes through when response.data can hold it. A declared field matches its incoming key exactly, else case-insensitively, and is stored under its declared storage key. A default value is never written.
func Ingest(incoming []jsvalue.Member, fields []Linked, elementIDs []string) IngestResult {
	result := IngestResult{Data: map[string]any{}, Flags: []Flag{}, Dropped: []Drop{}}
	elements := make(map[string]bool, len(elementIDs))
	for _, id := range elementIDs {
		elements[id] = true
	}
	values := make(map[string]any, len(incoming))
	keys := make([]string, 0, len(incoming))
	var ingestable []string
	for _, m := range incoming {
		values[m.Key] = m.Value
		keys = append(keys, m.Key)
		if !elements[m.Key] {
			ingestable = append(ingestable, m.Key)
		}
	}

	for _, m := range incoming {
		if !elements[m.Key] {
			continue
		}
		if isResponseDataValue(m.Value) {
			result.Data[m.Key] = m.Value
		} else {
			result.Dropped = append(result.Dropped, Drop{Key: m.Key, Reason: DropUnsupportedValue})
		}
	}

	consumed := map[string]bool{}
	for _, f := range fields {
		if f.Source != SourceIngested {
			continue
		}
		if elements[f.StorageKey] {
			if colliding, ok := MatchDeclaredName(keys, f.StorageKey); ok {
				consumed[colliding] = true
				result.Dropped = append(result.Dropped, Drop{Key: f.StorageKey, Reason: DropElementIDCollision})
			}
			continue
		}
		matched, ok := MatchDeclaredName(ingestable, f.StorageKey)
		if !ok {
			continue
		}
		consumed[matched] = true
		if f.Locked {
			result.Dropped = append(result.Dropped, Drop{Key: f.StorageKey, Reason: DropLockedField})
			continue
		}
		value, flag, ok := NormalizeValue(values[matched], f.DataType)
		if !ok {
			result.Dropped = append(result.Dropped, Drop{Key: f.StorageKey, Reason: DropUnsupportedValue})
			continue
		}
		if flag != "" {
			result.Flags = append(result.Flags, Flag{Key: f.StorageKey, Reason: flag})
		}
		if s, isString := value.(string); isString {
			if cut := truncateUTF8(s, MaxIngestedValueBytes); cut != s {
				result.Flags = append(result.Flags, Flag{Key: f.StorageKey, Reason: FlagTruncated})
				value = cut
			}
		}
		result.Data[f.StorageKey] = value
	}

	for _, key := range ingestable {
		if !consumed[key] {
			result.Dropped = append(result.Dropped, Drop{Key: key, Reason: DropUnknownKey})
		}
	}
	return result
}

// MergeFlags is mergeIngestFlags, for the partial writes of an update: a key the payload rewrote takes its new flags, none included, and every other key keeps the flags it had.
func MergeFlags(existing []Flag, written map[string]any, fresh []Flag) []Flag {
	out := make([]Flag, 0, len(existing)+len(fresh))
	for _, f := range existing {
		if _, rewritten := written[f.Key]; !rewritten {
			out = append(out, f)
		}
	}
	return append(out, fresh...)
}
