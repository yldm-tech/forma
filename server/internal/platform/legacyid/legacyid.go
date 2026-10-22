// Package legacyid maps forma2's ids (cuids) onto the uuids the rows migrated from forma2 carry. A migrated row's id is uuidv5(Namespace, <old id>), so every foreign key can be rewritten without a mapping table, and an old id that arrives from outside (a survey link, an SDK snippet, a stored /storage URL) is resolved by computing the same function.
package legacyid

import "github.com/google/uuid"

// Namespace is the forma legacy namespace: uuidv5 of the URL namespace and "https://github.com/yldm-tech/forma/legacy-id". The data migration derives every migrated id from it, so it must never change.
var Namespace = uuid.MustParse("6dd0d3d2-fd59-54ae-974c-0182b745d06c")

// ID is the id a row migrated from forma2 carries for its old id.
func ID(old string) uuid.UUID {
	return uuid.NewSHA1(Namespace, []byte(old))
}

// Resolve reads an id that arrives from outside: a UUID is taken as it is, and anything else is a forma2 id, mapped through ID.
func Resolve(raw string) uuid.UUID {
	if id, err := uuid.Parse(raw); err == nil {
		return id
	}
	return ID(raw)
}
