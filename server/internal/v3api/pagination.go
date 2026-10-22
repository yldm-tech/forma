package v3api

import (
	"encoding/base64"
	"slices"
	"strings"
	"unicode/utf8"
)

// Page sizes of the workspace-scoped reference lists (action classes, contact attribute keys), forma2's V3_LIST_DEFAULT_LIMIT and V3_LIST_MAX_LIMIT.
const (
	ListDefaultLimit = 50
	ListMaxLimit     = 250
)

const invalidCursor = "Invalid pagination cursor"

// ReadListQuery reads forma2's ZV3WorkspaceListQuery plus the workspace id: workspaceId, limit (1..250, default 50) and an optional cursor, nothing else.
func ReadListQuery(q *Query) (limit int, cursor string) {
	return q.Int("limit", ListDefaultLimit, 1, ListMaxLimit), q.OptionalString("cursor")
}

func encodeCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

// decodeCursor accepts only a cursor this server could have issued: one that decodes to a non-empty UTF-8 id and encodes back to itself.
func decodeCursor(cursor string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) == 0 || !utf8.Valid(raw) || encodeCursor(string(raw)) != cursor {
		return "", false
	}
	return string(raw), true
}

// Paginate is forma2's paginateByIdCursor over a bounded collection read whole: items ordered by id, the page after the cursor's id, and the next cursor when more remain. A cursor this server could not have issued is a 400 naming the cursor.
func Paginate[T any](items []T, id func(T) string, limit int, cursor string) ([]T, *string, error) {
	ordered := slices.Clone(items)
	slices.SortStableFunc(ordered, func(a, b T) int { return strings.Compare(id(a), id(b)) })
	if cursor != "" {
		after, ok := decodeCursor(cursor)
		if !ok {
			return nil, nil, BadRequest(invalidCursor, InvalidParam{Name: "cursor", Reason: invalidCursor})
		}
		start, _ := slices.BinarySearchFunc(ordered, after, func(item T, target string) int {
			if strings.Compare(id(item), target) <= 0 {
				return -1
			}
			return 1
		})
		ordered = ordered[start:]
	}
	if len(ordered) <= limit {
		return ordered, nil, nil
	}
	page := ordered[:limit]
	next := encodeCursor(id(page[len(page)-1]))
	return page, &next, nil
}
