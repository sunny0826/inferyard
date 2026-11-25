package core

import (
	"runtime/debug"
	"testing"
)

func TestCurrentBuildInfoPrefersInjectedValues(t *testing.T) {
	origVer, origTime, origCommit, origRead := Version, BuildTime, GitCommit, readBuildInfo
	t.Cleanup(func() {
		Version, BuildTime, GitCommit, readBuildInfo = origVer, origTime, origCommit, origRead
	})

	Version = "0.0.14"
	BuildTime = "2026-11-25T03:30:00Z"
	GitCommit = "1234567890abcdef1234567890abcdef12345678"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	}

	got := CurrentBuildInfo()
	if got.Version != "0.0.14" || got.BuildTime != "2026-11-25T03:30:00Z" ||
		got.GitCommit != "1234567890abcdef1234567890abcdef12345678" || got.Dirty {
		t.Fatalf("unexpected injected build info: %+v", got)
	}
}

func TestCurrentBuildInfoFallsBackToVCSAndPreservesMissingBuildTime(t *testing.T) {
	origVer, origTime, origCommit, origRead := Version, BuildTime, GitCommit, readBuildInfo
	t.Cleanup(func() {
		Version, BuildTime, GitCommit, readBuildInfo = origVer, origTime, origCommit, origRead
	})

	Version = "0.0.14"
	BuildTime = ""
	GitCommit = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef0123456789abcdef0123456789abcdef01"},
				{Key: "vcs.time", Value: "2026-11-20T00:00:00Z"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	}

	got := CurrentBuildInfo()
	if got.Version != "0.0.14" {
		t.Fatalf("Version = %q, want 0.0.14", got.Version)
	}
	if got.BuildTime != "" {
		t.Fatalf("BuildTime must stay empty rather than reusing vcs.time, got %q", got.BuildTime)
	}
	if got.GitCommit != "abcdef0123456789abcdef0123456789abcdef01" || !got.Dirty {
		t.Fatalf("unexpected VCS fallback: %+v", got)
	}
}

func TestCurrentBuildInfoHandlesUnavailableVCS(t *testing.T) {
	origVer, origTime, origCommit, origRead := Version, BuildTime, GitCommit, readBuildInfo
	t.Cleanup(func() {
		Version, BuildTime, GitCommit, readBuildInfo = origVer, origTime, origCommit, origRead
	})

	Version = "dev"
	BuildTime = ""
	GitCommit = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) { return nil, false }

	got := CurrentBuildInfo()
	if got.Version != "dev" || got.BuildTime != "" || got.GitCommit != "" || got.Dirty {
		t.Fatalf("unexpected build info without VCS: %+v", got)
	}
}
