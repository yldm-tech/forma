package buildinfo

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/repotest"
)

// The release image is the only build that stamps a version: deploy/docker/Dockerfile must set Version through this package's import path from its VERSION build argument, and the release workflow must pass that argument, or every published image reports 0.0.0 to telemetry, Sentry and the usage report.
func TestReleaseBuildStampsTheVersion(t *testing.T) {
	type marker struct{}
	flag := "-X " + reflect.TypeFor[marker]().PkgPath() + ".Version=${VERSION}"
	root := repotest.Root(t)
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

// The release image compiles the server with the Go of its GO_IMAGE build argument, while CI and local builds use go.mod's toolchain line (setup-go reads go-version-file), so the two must name the same release or the published binary is built by a Go no test ran with.
func TestReleaseImageUsesTheModuleToolchain(t *testing.T) {
	root := repotest.Root(t)
	gomod, err := os.ReadFile(filepath.Join(root, "apps/server/go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	toolchain := regexp.MustCompile(`(?m)^toolchain go(\S+)$`).FindSubmatch(gomod)
	if toolchain == nil {
		t.Fatal("apps/server/go.mod has no toolchain line")
	}
	dockerfile, err := os.ReadFile(filepath.Join(root, "deploy/docker/Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	image := regexp.MustCompile(`(?m)^ARG GO_IMAGE=golang:([^-@\s]+)[-@]`).FindSubmatch(dockerfile)
	if image == nil {
		t.Fatal("deploy/docker/Dockerfile has no ARG GO_IMAGE=golang:<version>")
	}
	if string(image[1]) != string(toolchain[1]) {
		t.Errorf("deploy/docker/Dockerfile builds with golang:%s, apps/server/go.mod pins toolchain go%s", image[1], toolchain[1])
	}
}
