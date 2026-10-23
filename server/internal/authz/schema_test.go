package authz

import (
	"os"
	"strings"
	"testing"
)

// The server embeds a copy of authzed/schema.zed; the two must never diverge.
func TestEmbeddedSchemaMatchesRepository(t *testing.T) {
	repo, err := os.ReadFile("../../../authzed/schema.zed")
	if err != nil {
		t.Fatalf("read authzed/schema.zed: %v", err)
	}
	if string(repo) != canonicalSchema {
		t.Fatal("server/internal/authz/schema.zed differs from authzed/schema.zed; copy the repository schema over it")
	}
	if !strings.HasPrefix(schemaDigest(canonicalSchema), "sha256:") || len(schemaDigest(canonicalSchema)) != 71 {
		t.Fatal("digest format")
	}
}

// forma2SchemaDigest is the digest of forma2's authzed/schema.zed at 3db326f (`git show "3db326f:authzed/schema.zed" | shasum -a 256`), computed as forma2's schema.ts computes it.
const forma2SchemaDigest = "sha256:05cbe96751b71d3b78a5ba1419c10d6d78af4d2c254f82a8db416c7f62a76146"

// The cutover points this server at the SpiceDB forma2 wrote, and a rollback points forma2 back at it. Both implementations count a changed doc comment as drift, so `forma authz schema apply` without --expected-current-digest would refuse on cutover and forma2's schema hook would fail on rollback. The schema therefore stays byte for byte forma2's; a real model change needs its own cutover plan.
func TestSchemaIsForma2s(t *testing.T) {
	if got := schemaDigest(canonicalSchema); got != forma2SchemaDigest {
		t.Fatalf("schema digest %s, forma2's is %s: restore forma2's text with git show \"3db326f:authzed/schema.zed\"", got, forma2SchemaDigest)
	}
}
