package core

import (
	"runtime/debug"
	"strings"
)

var (
	// BuildTime is optionally injected at link time (RFC3339 UTC, e.g. 2026-11-25T03:30:00Z).
	BuildTime = ""
	// GitCommit is optionally injected at link time; when empty, CurrentBuildInfo falls back to Go VCS metadata.
	GitCommit = ""

	readBuildInfo = debug.ReadBuildInfo
)

// BuildInfo describes static build metadata for the running Studio binary.
type BuildInfo struct {
	Version   string
	BuildTime string
	GitCommit string
	Dirty     bool
}

// CurrentBuildInfo returns version, build time, and Git commit metadata without inventing missing fields.
func CurrentBuildInfo() BuildInfo {
	info := BuildInfo{
		Version:   strings.TrimSpace(Version),
		BuildTime: strings.TrimSpace(BuildTime),
		GitCommit: strings.TrimSpace(GitCommit),
	}
	if info.GitCommit != "" {
		return info
	}
	bi, ok := readBuildInfo()
	if !ok || bi == nil {
		return info
	}
	var modified bool
	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.GitCommit = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			modified = strings.EqualFold(strings.TrimSpace(setting.Value), "true")
		}
	}
	if info.GitCommit != "" && modified {
		info.Dirty = true
	}
	return info
}
