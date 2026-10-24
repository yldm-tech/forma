package buildinfo

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The release image is the only build that stamps a version: deploy/docker/Dockerfile must set Version through this package's import path from its VERSION build argument, and the release workflow must pass that argument, or every published image reports 0.0.0 to telemetry, Sentry and the usage report.
func TestReleaseBuildStampsTheVersion(t *testing.T) {
	type marker struct{}
	flag := "-X " + reflect.TypeFor[marker]().PkgPath() + ".Version=${VERSION}"
	root := filepath.Join("..", "..", "..", "..", "..")
	dockerfile, err := os.ReadFile(filepath.Join(root, "deploy", "docker", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dockerfile), "ARG VERSION=") || !strings.Contains(string(dockerfile), flag) {
		t.Errorf("deploy/docker/Dockerfile does not stamp the version with %s from ARG VERSION", flag)
	}
	for _, workflow := range []string{"release.yml", "docker-build-validation.yml"} {
		raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", workflow))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "VERSION=") || !strings.Contains(string(raw), "build-args:") {
			t.Errorf("%s builds the image without a VERSION build argument", workflow)
		}
	}
}
