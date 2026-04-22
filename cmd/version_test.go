package cmd

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestCurrentVersionUsesTaggedBuildVersion(t *testing.T) {
	prev := readBuildInfo
	defer func() { readBuildInfo = prev }()

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v0.4.0"}}, true
	}

	if got := currentVersion(); got != "v0.4.0" {
		t.Fatalf("expected tagged version, got %q", got)
	}
}

func TestCurrentVersionFallsBackToRevisionForLocalBuilds(t *testing.T) {
	prev := readBuildInfo
	defer func() { readBuildInfo = prev }()

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "(devel)"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef1234567890"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	}

	if got := currentVersion(); got != "devel (abcdef123456, dirty)" {
		t.Fatalf("expected revision-based devel version, got %q", got)
	}
}

func TestCurrentVersionReturnsUnknownWithoutBuildInfo(t *testing.T) {
	prev := readBuildInfo
	defer func() { readBuildInfo = prev }()

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	if got := currentVersion(); got != "unknown" {
		t.Fatalf("expected unknown version, got %q", got)
	}
}

func TestVersionCommandPrintsCurrentVersion(t *testing.T) {
	buf := &bytes.Buffer{}
	versionCmd.SetOut(buf)
	versionCmd.SetErr(buf)
	versionCmd.Run(versionCmd, nil)

	got := strings.TrimSpace(buf.String())
	if got != currentVersion() {
		t.Fatalf("expected version command output %q, got %q", currentVersion(), got)
	}
}
