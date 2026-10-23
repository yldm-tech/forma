// Package buildinfo holds what the release build stamps into the binary.
package buildinfo

// Version is the release version, set at build time with -ldflags "-X github.com/yldm-tech/forma/server/internal/platform/buildinfo.Version=<version>". A development build reports 0.0.0, forma2's version of an unreleased package.
var Version = "0.0.0"

// Released reports whether the binary was stamped with a release version.
func Released() bool { return Version != "0.0.0" }
