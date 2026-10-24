package v3api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MaxBodyBytes is forma2's DEFAULT_REQUEST_BODY_LIMIT_BYTES.
const MaxBodyBytes int64 = 2 << 20

// The details forma2 answered for each part of a request that failed validation.
const (
	invalidBody   = "Invalid request body"
	invalidQuery  = "Invalid query parameters"
	invalidParams = "Invalid route parameters"
)

var errTrailingData = errors.New("trailing data after the JSON body")

// InvalidBody is a body that decoded but failed a field rule.
func InvalidBody(params ...InvalidParam) *Problem {
	return BadRequest(invalidBody, params...)
}

// ReadBody decodes a JSON object body strictly, as forma2's `.strict()` body schemas did: unknown keys, a body that is not one object and a mistyped field all fail with 400 and the offending names. The handler checks its own field rules afterwards and reports them through InvalidBody. Bodies over MaxBodyBytes fail with 413.
func ReadBody(c *gin.Context, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		// Anything after the object, even a second valid value, makes the body malformed.
		if extra := dec.Decode(&json.RawMessage{}); !errors.Is(extra, io.EOF) {
			err = errTrailingData
			var tooLarge *http.MaxBytesError
			if errors.As(extra, &tooLarge) {
				err = extra
			}
		}
	}
	if err == nil {
		return nil
	}
	var tooLarge *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooLarge):
		detail := fmt.Sprintf("Request body must not exceed %d bytes", MaxBodyBytes)
		return &Problem{Status: http.StatusRequestEntityTooLarge, Title: "Payload Too Large", Detail: detail, Code: "payload_too_large"}
	case errors.As(err, &typeErr):
		name := typeErr.Field
		if name == "" {
			name = "body"
		}
		return InvalidBody(InvalidParam{Name: name, Reason: fmt.Sprintf("Invalid input: expected %s, received %s", jsonType(typeErr.Type.Kind().String()), typeErr.Value)})
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return InvalidBody(InvalidParam{Name: "body", Reason: "Unrecognized key: " + strings.TrimPrefix(err.Error(), "json: unknown field ")})
	}
	return InvalidBody(InvalidParam{Name: "body", Reason: "Malformed JSON input, please check your request body"})
}

// jsonType names a Go kind the way the JSON input is described to the caller.
func jsonType(kind string) string {
	switch kind {
	case "struct", "map":
		return "object"
	case "slice", "array":
		return "array"
	case "int", "int64", "float64":
		return "number"
	case "bool":
		return "boolean"
	}
	return kind
}

// RequiredString reports a missing body field the way zod did.
func RequiredString(name string) InvalidParam {
	return InvalidParam{Name: name, Reason: "Invalid input: expected string, received undefined"}
}

// ParamUUID reads a route parameter that must be a UUID (forma2's ids were cuid2; this rewrite's are UUIDs).
func ParamUUID(c *gin.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, BadRequest(invalidParams, InvalidParam{Name: name, Reason: "Invalid UUID"})
	}
	return id, nil
}

// Query reads a strict query string: every key must be one the route declares, each at most once.
type Query struct {
	values url.Values
	issues []InvalidParam
}

// ReadQuery starts reading the request's query string; known lists the keys the route declares.
func ReadQuery(c *gin.Context, known ...string) *Query {
	q := &Query{values: c.Request.URL.Query()}
	var unknown []string
	for key := range q.values {
		if !slices.Contains(known, key) {
			unknown = append(unknown, strconv.Quote(key))
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		reason := "Unrecognized key: " + unknown[0]
		if len(unknown) > 1 {
			reason = "Unrecognized keys: " + strings.Join(unknown, ", ")
		}
		q.issues = append(q.issues, InvalidParam{Name: "query", Reason: reason})
	}
	return q
}

// single returns the key's only value, or reports a repeated key.
func (q *Query) single(name string) (string, bool) {
	vs, ok := q.values[name]
	if !ok {
		return "", false
	}
	if len(vs) > 1 {
		q.issues = append(q.issues, InvalidParam{Name: name, Reason: "Invalid input: expected string, received array"})
		return "", false
	}
	return vs[0], true
}

// UUID reads a required UUID.
func (q *Query) UUID(name string) uuid.UUID {
	v, ok := q.single(name)
	if !ok {
		if _, present := q.values[name]; !present {
			q.issues = append(q.issues, RequiredString(name))
		}
		return uuid.Nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		q.issues = append(q.issues, InvalidParam{Name: name, Reason: "Invalid UUID"})
	}
	return id
}

// Int reads an optional integer within [minimum, maximum], def when absent (zod's coerce.number().int().min().max().default()).
func (q *Query) Int(name string, def, minimum, maximum int) int {
	v, ok := q.single(name)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	switch {
	case err != nil:
		q.issues = append(q.issues, InvalidParam{Name: name, Reason: "Invalid input: expected int, received number"})
	case n < minimum:
		q.issues = append(q.issues, InvalidParam{Name: name, Reason: fmt.Sprintf("Too small: expected number to be >=%d", minimum)})
	case n > maximum:
		q.issues = append(q.issues, InvalidParam{Name: name, Reason: fmt.Sprintf("Too big: expected number to be <=%d", maximum)})
	}
	return n
}

// OptionalString reads an optional non-empty string; "" when absent.
func (q *Query) OptionalString(name string) string {
	v, ok := q.single(name)
	if ok && v == "" {
		q.issues = append(q.issues, InvalidParam{Name: name, Reason: "Too small: expected string to have >=1 characters"})
	}
	return v
}

// Err is the 400 for every issue found, or nil.
func (q *Query) Err() error {
	if len(q.issues) == 0 {
		return nil
	}
	return BadRequest(invalidQuery, q.issues...)
}
