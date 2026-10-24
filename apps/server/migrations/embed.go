// Package migrations holds the goose SQL migrations that define the database schema. They are embedded so the `forma migrate` binary needs nothing beside it.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
