package v3api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// MaxBodyBytes is forma2's DEFAULT_REQUEST_BODY_LIMIT_BYTES.
const MaxBodyBytes = 2 << 20

// ReadJSON reads a JSON request body of at most MaxBodyBytes as raw JSON. A body over the limit is 413, one that is not a single JSON value 400 "Invalid request body".
func ReadJSON(c *gin.Context) (json.RawMessage, error) {
	tooLarge := PayloadTooLarge(fmt.Sprintf("Request body must not exceed %d bytes", MaxBodyBytes))
	if c.Request.ContentLength > MaxBodyBytes {
		return nil, tooLarge
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, tooLarge
		}
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if !json.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("Invalid request body", InvalidParam{Name: "body", Reason: "Malformed JSON input, please check your request body"})
	}
	return raw, nil
}

// NoQuery is zod's strict empty query: any parameter is refused, 400 "Invalid query parameters".
func NoQuery(c *gin.Context) error {
	return AllowQuery(c)
}

// AllowQuery refuses query parameters other than the allowed ones, as a strict zod query object does.
func AllowQuery(c *gin.Context, allowed ...string) error {
	var unknown []string
	for key := range c.Request.URL.Query() {
		known := false
		for _, a := range allowed {
			if a == key {
				known = true
				break
			}
		}
		if !known {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	quoted := make([]string, len(unknown))
	for i, k := range unknown {
		quoted[i] = `"` + k + `"`
	}
	reason := "Unrecognized key: " + quoted[0]
	if len(quoted) > 1 {
		reason = "Unrecognized keys: " + strings.Join(quoted, ", ")
	}
	return BadRequest("Invalid query parameters", InvalidParam{Name: "query", Reason: reason})
}
