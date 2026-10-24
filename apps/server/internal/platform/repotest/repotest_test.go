package repotest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootHoldsThisModule(t *testing.T) {
	if _, err := os.Stat(filepath.Join(Root(t), "apps/server/internal/platform/repotest/repotest.go")); err != nil {
		t.Fatal(err)
	}
}
