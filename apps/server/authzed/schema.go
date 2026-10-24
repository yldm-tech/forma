// Package authzed holds the canonical SpiceDB schema (schema.zed) and its assertion suite (schema-validation.yaml, run by validate.sh). This directory is the schema's only copy: the server embeds it from here.
package authzed

import _ "embed"

// Schema is schema.zed, byte for byte.
//
//go:embed schema.zed
var Schema string
