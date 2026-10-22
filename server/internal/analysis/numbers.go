package analysis

import (
	"encoding/json"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// Num is a number of the summary as forma2 computed it in JavaScript: NaN, which a bucket opened by an out-of-range answer holds, is serialized as JSON.stringify writes it, null.
type Num float64

func (n Num) MarshalJSON() ([]byte, error) {
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return []byte("null"), nil
	}
	return json.Marshal(f)
}

// jsRound is Math.round: the nearest integer, a half rounded towards positive infinity.
func jsRound(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return f
	}
	r := math.Floor(f)
	if f-r >= 0.5 {
		r++
	}
	return r
}

// round2 is forma2's convertFloatTo2Decimal, Math.round(x * 100) / 100. The conversion rounds the product before jsRound subtracts from it: Go may otherwise fuse the two into one multiply-subtract and round differently from JavaScript (2.675 would give 2.67, not 2.68).
func round2(f float64) float64 {
	return jsRound(float64(f*100)) / 100
}

// percent is round2(part / whole * 100), 0 when whole is 0.
func percent(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return round2(part / whole * 100)
}

// toFixed2 is Number(x.toFixed(2)): x rounded on its exact binary value to two decimals, a tie going to the larger of the two candidates.
func toFixed2(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return f
	}
	// A float64 below 2^63 has at most 1074 significant fraction digits; 1100 keeps the decimal expansion exact.
	exact := new(big.Float).SetFloat64(math.Abs(f)).Text('f', 1100)
	intPart, frac, _ := strings.Cut(exact, ".")
	frac += "000"
	digits := intPart + frac[:2]
	n := new(big.Int)
	n.SetString(digits, 10)
	if frac[2] >= '5' {
		n.Add(n, big.NewInt(1))
	}
	s := n.String()
	for len(s) < 3 {
		s = "0" + s
	}
	out, _ := strconv.ParseFloat(s[:len(s)-2]+"."+s[len(s)-2:], 64)
	if f < 0 {
		out = -out
	}
	return out
}

// parseIntJS is parseInt(s): the leading integer of s after white space, NaN when there is none.
func parseIntJS(s string) float64 {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	sign := 1.0
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return math.NaN()
	}
	n, _ := strconv.ParseFloat(s[:end], 64)
	return sign * n
}

// arrayIndex reports whether key is an array index, which JavaScript enumerates before every other property key: the canonical decimal form of an integer below 2^32 - 1.
func arrayIndex(key string) (uint64, bool) {
	if key == "" || (len(key) > 1 && key[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(key, 10, 64)
	if err != nil || n >= 1<<32-1 {
		return 0, false
	}
	return n, true
}

// jsKeyOrder sorts keys, given in insertion order, into the order Object.keys enumerates them: array indices ascending, then the rest as inserted.
func jsKeyOrder(keys []string) {
	sort.SliceStable(keys, func(i, j int) bool {
		a, aIndex := arrayIndex(keys[i])
		b, bIndex := arrayIndex(keys[j])
		if aIndex && bIndex {
			return a < b
		}
		return aIndex && !bIndex
	})
}

// jsonbKeys are the keys of an object read from a jsonb column in the order JavaScript enumerated them after Prisma decoded it: jsonb stores keys shorter first, then bytewise, and Object.keys moves array indices to the front.
func jsonbKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) < len(keys[j])
		}
		return keys[i] < keys[j]
	})
	jsKeyOrder(keys)
	return keys
}

// truthy is JavaScript's Boolean(v) for a decoded JSON value; an absent key is jsvalue.Undefined.
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

// member is m[key], jsvalue.Undefined when absent.
func member(m map[string]any, key string) any {
	if v, ok := m[key]; ok {
		return v
	}
	return jsvalue.Undefined
}

// positive reports whether v is a number above 0, the `ttc[id] > 0` test.
func positive(v any) bool {
	n, ok := v.(float64)
	return ok && n > 0
}
