package workflow

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/v3api"
)

// Sort orders of the workflow list, forma2's ZWorkflowSortBy.
const (
	sortCreatedAt = "createdAt"
	sortUpdatedAt = "updatedAt"
	sortName      = "name"
)

const cursorVersion = 1

// Cursor problems, forma2's WorkflowInvalidInputError messages.
const (
	invalidCursor    = "The cursor is invalid."
	mismatchedCursor = "The cursor does not match the requested sort order."
)

// listCursor is forma2's workflow list cursor: base64url JSON {version, sortBy, value, id}, bound to the sort it was issued for. Dates are written with microseconds, PostgreSQL's precision, so a page boundary never falls between rows that share a millisecond.
type listCursor struct {
	Version int    `json:"version"`
	SortBy  string `json:"sortBy"`
	Value   string `json:"value"`
	ID      string `json:"id"`
	at      time.Time
	id      uuid.UUID
}

// runCursor is forma2's run list cursor: base64url JSON {version, value, id} over created_at.
type runCursor struct {
	Version int    `json:"version"`
	Value   string `json:"value"`
	ID      string `json:"id"`
	at      time.Time
	id      uuid.UUID
}

func cursorTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000Z07:00")
}

func encodeCursor(v any) string {
	return base64.RawURLEncoding.EncodeToString(mustJSON(v))
}

// decodeCursorJSON reads a base64url token, padded or not, as Node's Buffer did.
func decodeCursorJSON(token string, dst any) bool {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(token, "="))
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

func cursorProblem(detail string) *v3api.Problem {
	return v3api.BadRequest(detail, v3api.InvalidParam{Name: "cursor", Reason: detail})
}

// parseCursorTime reads zod's iso.datetime({offset: true}).
func parseCursorTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil && strings.Contains(s, "T")
}

func nextListCursor(w Workflow, sortBy string) string {
	c := listCursor{Version: cursorVersion, SortBy: sortBy, ID: w.ID.String()}
	switch sortBy {
	case sortName:
		c.Value = w.Name
	case sortCreatedAt:
		c.Value = cursorTime(w.CreatedAt)
	default:
		c.Value = cursorTime(w.UpdatedAt)
	}
	return encodeCursor(c)
}

func decodeListCursor(token, sortBy string) (*listCursor, error) {
	var c listCursor
	if !decodeCursorJSON(token, &c) || c.Version != cursorVersion || c.ID == "" {
		return nil, cursorProblem(invalidCursor)
	}
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return nil, cursorProblem(invalidCursor)
	}
	c.id = id
	switch c.SortBy {
	case sortName:
	case sortCreatedAt, sortUpdatedAt:
		at, ok := parseCursorTime(c.Value)
		if !ok {
			return nil, cursorProblem(invalidCursor)
		}
		c.at = at
	default:
		return nil, cursorProblem(invalidCursor)
	}
	if c.SortBy != sortBy {
		return nil, cursorProblem(mismatchedCursor)
	}
	return &c, nil
}

func nextRunCursor(r Run) string {
	return encodeCursor(runCursor{Version: cursorVersion, Value: cursorTime(r.CreatedAt), ID: r.ID.String()})
}

func decodeRunCursor(token string) (*runCursor, error) {
	var c runCursor
	if !decodeCursorJSON(token, &c) || c.Version != cursorVersion {
		return nil, cursorProblem(invalidCursor)
	}
	at, ok := parseCursorTime(c.Value)
	id, err := uuid.Parse(c.ID)
	if !ok || err != nil {
		return nil, cursorProblem(invalidCursor)
	}
	c.at, c.id = at, id
	return &c, nil
}
