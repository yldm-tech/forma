package v1api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// MaxBodyBytes is forma2's DEFAULT_REQUEST_BODY_LIMIT_BYTES, the largest body its API routes read.
const MaxBodyBytes = 2 * 1024 * 1024

// ErrBodyTooLarge is a declared or actual body over MaxBodyBytes; its text is forma2's, which clients see in the details.
var ErrBodyTooLarge = fmt.Errorf("Request body must not exceed %d bytes", MaxBodyBytes) //nolint:staticcheck // forma2's message, which clients see

// MalformedJSONMessage is what forma2 answered a body that is not JSON with.
const MalformedJSONMessage = "Malformed JSON input, please check your request body"

// ReadBody is forma2's parseJsonBodyWithLimit: a declared or actual body over MaxBodyBytes is ErrBodyTooLarge, and the body must be JSON (an empty body is not). It returns the decoded value: a string, float64, bool, nil, []any or map[string]any.
func ReadBody(r *http.Request) (any, error) {
	if r.ContentLength > MaxBodyBytes {
		return nil, ErrBodyTooLarge
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if len(b) > MaxBodyBytes {
		return nil, ErrBodyTooLarge
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// ReadJSONObject is ReadBody for a route that reads members of the body. A body that is JSON but not an object reads as an object without members, as forma2's property reads on it found nothing.
func ReadJSONObject(r *http.Request) (map[string]any, error) {
	v, err := ReadBody(r)
	if err != nil {
		return nil, err
	}
	obj, _ := v.(map[string]any)
	if obj == nil {
		obj = map[string]any{}
	}
	return obj, nil
}

// Truthy is JavaScript's Boolean(v) for a JSON value.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

// JSType is the type name zod's messages report for a JSON value; an absent one is "undefined".
func JSType(v any, present bool) string {
	switch v.(type) {
	case nil:
		if !present {
			return "undefined"
		}
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	}
	return "object"
}

// QuotedOptions is how zod's enum message lists the options: "a"|"b".
func QuotedOptions(values []string) string {
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = `"` + v + `"`
	}
	return strings.Join(q, "|")
}

// cuid2 is zod's cuid2 shape.
var cuid2 = regexp.MustCompile(`^[0-9a-z]+$`)

// IsID reports whether s passes the v1 APIs' id check, zod's z.cuid2() widened to UUIDs in their canonical form. Every row id is a UUID; a cuid2 passes the check but names no row, so it is not found.
func IsID(s string) bool {
	if _, err := uuid.Parse(s); err == nil && len(s) == 36 {
		return true
	}
	return cuid2.MatchString(s)
}

// IsURL is zod's z.url(): an absolute URL with a scheme and a host.
func IsURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && u.Host != ""
}
