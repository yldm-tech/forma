package authz

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// canonicalSchema is authzed/schema.zed, embedded because go:embed cannot reach outside the Go module. TestEmbeddedSchemaMatchesRepository keeps the copy identical.
//
//go:embed schema.zed
var canonicalSchema string

// Schema check statuses.
const (
	SchemaMatched   = "matched"
	SchemaDrifted   = "drifted"
	SchemaApplied   = "applied"
	SchemaUnchanged = "unchanged"
)

// SchemaState compares the canonical schema with SpiceDB's by digest and by SpiceDB's semantic diff; it never carries schema text or the names that changed.
type SchemaState struct {
	Status          string         `json:"status"`
	SourceDigest    string         `json:"sourceDigest"`
	RemoteDigest    *string        `json:"remoteDigest"`
	RemoteState     string         `json:"remoteState"`
	DifferenceCount int            `json:"differenceCount"`
	DifferenceKinds map[string]int `json:"differenceKinds,omitempty"`
}

func schemaDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CheckSchema reports whether SpiceDB holds the canonical schema. An empty SpiceDB is drifted with one schema_missing difference.
func CheckSchema(ctx context.Context, c *Client) (SchemaState, error) {
	state := SchemaState{SourceDigest: schemaDigest(canonicalSchema)}
	remote, err := c.ReadSchema(ctx)
	if err != nil {
		return state, err
	}
	if remote == "" {
		state.Status, state.RemoteState, state.DifferenceCount, state.DifferenceKinds = SchemaDrifted, "empty", 1, map[string]int{"schema_missing": 1}
		return state, nil
	}
	digest := schemaDigest(remote)
	state.RemoteDigest, state.RemoteState = &digest, "present"
	diff, err := c.DiffSchema(ctx, canonicalSchema)
	if err != nil {
		return state, err
	}
	state.Status, state.DifferenceCount = SchemaMatched, diff.Count
	if diff.Count > 0 {
		state.Status, state.DifferenceKinds = SchemaDrifted, diff.Kinds
	}
	return state, nil
}

// ApplySchema writes the canonical schema when SpiceDB's differs, guarded by a digest precondition: an empty SpiceDB needs no expectedDigest, a populated one must match expectedDigest exactly, otherwise authzed_schema_changed, so an operator never overwrites a schema they have not looked at. The write is then verified.
func ApplySchema(ctx context.Context, c *Client, expectedDigest string) (SchemaState, error) {
	current, err := CheckSchema(ctx, c)
	if err != nil {
		return current, err
	}
	if current.Status == SchemaMatched {
		current.Status = SchemaUnchanged
		return current, nil
	}
	satisfied := expectedDigest == ""
	if current.RemoteState == "present" {
		satisfied = expectedDigest == *current.RemoteDigest
	}
	if !satisfied {
		return current, newError(CodeSchemaChanged, "write_schema_precondition", false, 0)
	}
	if err := c.WriteSchema(ctx, canonicalSchema); err != nil {
		return current, err
	}
	verified, err := CheckSchema(ctx, c)
	if err != nil {
		return verified, err
	}
	if verified.Status != SchemaMatched {
		return verified, newError(CodeSchemaVerificationFailed, "verify_written_schema", false, 1)
	}
	verified.Status = SchemaApplied
	return verified, nil
}
