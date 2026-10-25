// Package rowid reads the id of a row as it arrives from outside: a path or query parameter, a request body, a stored link, a signed token. Every row's id is a UUID, so only the canonical 36-character form names one; anything else names nothing.
package rowid

import "github.com/google/uuid"

// Parse reads raw as a row id: the UUID in its canonical 36-character form (in either case), or nothing. The braced and urn forms uuid.Parse also reads are not row ids.
func Parse(raw string) (uuid.UUID, bool) {
	if len(raw) != 36 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// Matches reports whether claimed names id.
func Matches(claimed string, id uuid.UUID) bool {
	parsed, ok := Parse(claimed)
	return ok && parsed == id
}
