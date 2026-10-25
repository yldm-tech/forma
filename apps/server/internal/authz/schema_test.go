package authz

import (
	"strings"
	"testing"
)

func TestSchemaDigestFormat(t *testing.T) {
	if !strings.HasPrefix(schemaDigest(canonicalSchema), "sha256:") || len(schemaDigest(canonicalSchema)) != 71 {
		t.Fatal("digest format")
	}
}

// pinnedSchemaDigest is the digest of the schema every instance's SpiceDB holds.
const pinnedSchemaDigest = "sha256:05cbe96751b71d3b78a5ba1419c10d6d78af4d2c254f82a8db416c7f62a76146"

// The initialization Job refuses to replace a schema that differs from the release's unless its digest is supplied, and a changed doc comment counts as a difference, so any edit to schema.zed needs a manual `forma authz schema apply --expected-current-digest` on every instance (deploy/helm/forma/README.md). Change the digest here together with the schema, and plan that apply.
func TestSchemaIsPinned(t *testing.T) {
	if got := schemaDigest(canonicalSchema); got != pinnedSchemaDigest {
		t.Fatalf("schema digest %s, pinned %s: an edited schema needs a manual apply on every instance", got, pinnedSchemaDigest)
	}
}
