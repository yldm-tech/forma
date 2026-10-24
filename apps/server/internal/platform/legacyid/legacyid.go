// Package legacyid maps forma2's ids onto the rewrite's UUIDs. A row migrated from forma2 gets uuidv5(Namespace, <its forma2 id>) as its id, so every foreign key is rewritten without a mapping table and an old id that arrives from outside (a survey link, an SDK snippet, a signed token) resolves by computing the same function.
package legacyid

import "github.com/google/uuid"

// Namespace is the forma legacy namespace, uuidv5(URL namespace, "https://forma.ylam.ai/ns/legacy-id"). The data migration must use this exact value; changing it orphans every migrated id.
var Namespace = uuid.MustParse("027acbf8-3f2c-52c4-9593-7683c120c88b")

// ID is the rewrite's id of the row forma2 knew as old.
func ID(old string) uuid.UUID {
	return uuid.NewSHA1(Namespace, []byte(old))
}

// Resolve reads an id that arrived from outside: a UUID is taken as it is, anything else is a forma2 id and resolves through ID. An empty string resolves to nothing.
func Resolve(raw string) (uuid.UUID, bool) {
	if raw == "" {
		return uuid.Nil, false
	}
	if id, err := uuid.Parse(raw); err == nil && len(raw) == 36 {
		return id, true
	}
	return ID(raw), true
}

// Matches reports whether claimed names id: the same UUID, or the forma2 id it was migrated from.
func Matches(claimed string, id uuid.UUID) bool {
	resolved, ok := Resolve(claimed)
	return ok && resolved == id
}
