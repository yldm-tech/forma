// Package legacyid maps forma2's ids onto this schema's. Rows migrated from forma2 get uuidv5(Namespace, <old id>) as their id, so every foreign key can be rewritten without a mapping table, and an old id that arrives from outside (a survey link, an SDK snippet, a stored client state) is resolved by computing the same function.
package legacyid

import (
	"github.com/google/uuid"
)

// Namespace is the forma legacy namespace: the uuidv5 namespace of every id the data migration carries over from forma2. Changing it orphans every migrated id, so it is fixed for good.
var Namespace = uuid.MustParse("58533b22-6caf-4ca2-814e-e74cb461e35d")

// FromLegacy is the id a forma2 row with id old was migrated to.
func FromLegacy(old string) uuid.UUID {
	return uuid.NewSHA1(Namespace, []byte(old))
}

// Resolve reads an id that arrived from outside: a UUID is taken as it is, and anything else is a forma2 id resolved through FromLegacy. ok is false only for the empty string, which names nothing.
func Resolve(id string) (resolved uuid.UUID, ok bool) {
	if id == "" {
		return uuid.Nil, false
	}
	if u, err := uuid.Parse(id); err == nil && len(id) == 36 {
		return u, true
	}
	return FromLegacy(id), true
}
