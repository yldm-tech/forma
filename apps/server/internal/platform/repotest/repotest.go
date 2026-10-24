// Package repotest locates the monorepo root for the tests that read files outside the Go module: parity tests that keep a Go copy identical to its source of truth elsewhere in the repository (apps/web, packages, deploy/docker, .github/workflows). They build their paths from Root, so moving this module or a test package edits the depth here instead of a hand-counted ../ chain in every test.
package repotest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// depth is the number of directories between this file's directory and the repository root: apps/server/internal/platform/repotest.
const depth = 5

// marker is a file only the repository root has.
const marker = "pnpm-workspace.yaml"

// Root returns the absolute path of the repository root. It is found from this source file's compile-time path, so it does not depend on the working directory go test runs a package in. It fails the test when that path is not absolute (a test binary built with -trimpath) or does not lead to the root.
func Root(tb testing.TB) string {
	tb.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		tb.Fatalf("repotest: no absolute source path (%q) to find the repository root from; run the tests without -trimpath", file)
	}
	root := filepath.Dir(file)
	for range depth {
		root = filepath.Dir(root)
	}
	if _, err := os.Stat(filepath.Join(root, marker)); err != nil {
		tb.Fatalf("repotest: %s has no %s, so it is not the repository root; update depth in repotest.go after moving the package: %v", root, marker, err)
	}
	return root
}
