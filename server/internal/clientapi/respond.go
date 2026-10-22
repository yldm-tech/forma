package clientapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
)

// forma2's v1 API responses (apps/web/lib/api/response.ts): a body is {"data": ...} or {"code", "message", "details"}, sent as application/json and, unless the route says otherwise, with Cache-Control "private, no-store".

const (
	noStore = "private, no-store"
	// preflightCache is the Cache-Control most of forma2's OPTIONS handlers sent.
	preflightCache = "public, s-maxage=3600, max-age=3600"
)

// Client API error codes.
const (
	codeBadRequest      = "bad_request"
	codeForbidden       = "forbidden"
	codeNotFound        = "not_found"
	codeConflict        = "conflict"
	codePayloadTooLarge = "payload_too_large"
	codeTooManyRequests = "too_many_requests"
	codeInternal        = "internal_server_error"
)

// Generic 500 messages, which differ by route in forma2.
const (
	// msgGeneric is handleApiError's and the v2 routes' message.
	msgGeneric = "Something went wrong. Please try again."
	// msgEnvironment is the environment state route's.
	msgEnvironment = "An error occurred while processing your request."
	// msgUserState is the identify route's.
	msgUserState = "Unable to fetch user state"
)

// field is one entry of an ordered JSON object.
type field struct {
	Key   string
	Value any
}

// fields is a JSON object whose keys keep the order they were set in, as a JavaScript object's do; setting a key again replaces its value in place.
type fields []field

func (f *fields) set(key string, value any) {
	for i := range *f {
		if (*f)[i].Key == key {
			(*f)[i].Value = value
			return
		}
	}
	*f = append(*f, field{Key: key, Value: value})
}

func (f fields) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, kv := range f {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := marshal(kv.Key)
		if err != nil {
			return nil, err
		}
		v, err := marshal(kv.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshal is JSON.stringify's encoding: no HTML escaping.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// clientError is a client API failure as forma2 answered it.
type clientError struct {
	Status  int
	Code    string
	Message string
	// Details is a JSON object: fields, a map, or nil for {}.
	Details any
}

func (e *clientError) Error() string { return e.Code + ": " + e.Message }

func newError(status int, code, message string, details any) *clientError {
	return &clientError{Status: status, Code: code, Message: message, Details: details}
}

func badRequest(message string, details any) *clientError {
	return newError(http.StatusBadRequest, codeBadRequest, message, details)
}

func forbidden(message string, details any) *clientError {
	return newError(http.StatusForbidden, codeForbidden, message, details)
}

func internal(message string) *clientError {
	return newError(http.StatusInternalServerError, codeInternal, message, nil)
}

// notFound is forma2's notFoundResponse: "<Type> not found" naming the resource as the caller did; a nil id is reported as null.
func notFound(resourceType string, id *string) *clientError {
	var rid any
	if id != nil {
		rid = *id
	}
	return newError(http.StatusNotFound, codeNotFound, resourceType+" not found", fields{{"resource_id", rid}, {"resource_type", resourceType}})
}

func tooManyRequests() *clientError {
	return newError(http.StatusTooManyRequests, codeTooManyRequests, apperr.TooManyRequestsMessage, nil)
}

// writeJSON writes a client API body with its Cache-Control.
func writeJSON(c *gin.Context, status int, body any, cache string) {
	b, err := marshal(body)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", cache)
	c.Data(status, "application/json", b)
	c.Abort()
}

// writeData is forma2's successResponse.
func writeData(c *gin.Context, data any, cache string) {
	writeJSON(c, http.StatusOK, fields{{"data", data}}, cache)
}

func writeError(c *gin.Context, e *clientError) {
	details := e.Details
	if details == nil {
		details = fields{}
	}
	writeJSON(c, e.Status, fields{{"code", e.Code}, {"message", e.Message}, {"details", details}}, noStore)
}

// fail answers err: a clientError as it is, an expected failure of another module (*apperr.Error) the way forma2's handleApiError mapped its error classes, anything else as a 500 with the route's generic message, logged here.
func (h *Handler) fail(c *gin.Context, err error, generic string) {
	var ce *clientError
	if errors.As(err, &ce) {
		writeError(c, ce)
		return
	}
	if ae, ok := apperr.As(err); ok && ae.Status < http.StatusInternalServerError {
		writeError(c, fromAppErr(ae))
		return
	}
	h.log.ErrorContext(c.Request.Context(), "client API request failed", "route", c.FullPath(), "err", err)
	writeError(c, internal(generic))
}

// fromAppErr maps an expected failure by its status onto forma2's client codes. A not found from the response module already carries forma2's details; one from elsewhere gets the message alone.
func fromAppErr(ae *apperr.Error) *clientError {
	var details any
	if len(ae.Details) > 0 {
		details = ae.Details
	}
	if ae.Status == http.StatusNotFound {
		if rt, ok := ae.Details["resource_type"]; ok {
			return newError(http.StatusNotFound, codeNotFound, ae.Message, fields{{"resource_id", ae.Details["resource_id"]}, {"resource_type", rt}})
		}
	}
	code := map[int]string{
		http.StatusBadRequest: codeBadRequest, http.StatusForbidden: codeForbidden, http.StatusNotFound: codeNotFound,
		http.StatusConflict: codeConflict, http.StatusTooManyRequests: codeTooManyRequests,
	}[ae.Status]
	if code == "" {
		code = codeBadRequest
	}
	if ae.Status == http.StatusTooManyRequests {
		details = nil
	}
	return newError(ae.Status, code, ae.Message, details)
}
