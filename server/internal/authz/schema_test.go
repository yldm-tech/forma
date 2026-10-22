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
