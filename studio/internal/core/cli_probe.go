package core

import (
	"context"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const probeTTL = 10 * time.Second

func parseCLISemver(raw string) (ver semver, prerelease bool, ok bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if trimmed == "" {
		return semver{}, false, false
	}
	if base, build, hasBuild := strings.Cut(trimmed, "+"); hasBuild {
		if !validBuildMetadata(build) {
			return semver{}, false, false
		}
		trimmed = base
	}
	if base, pre, hasPre := strings.Cut(trimmed, "-"); hasPre {
		if pre == "" {
			return semver{}, false, false
		}
		prerelease = true
		trimmed = base
	} else {
		parts := strings.Split(trimmed, ".")
		if len(parts) == 4 && strings.HasPrefix(parts[3], "dev") && allDigits(parts[3][3:]) {
			prerelease = true
			trimmed = strings.Join(parts[:3], ".")
		} else if len(parts) == 3 {
			if corePatch, okSuffix := splitPEP440Prerelease(parts[2]); okSuffix {
				prerelease = true
				parts[2] = corePatch
				trimmed = strings.Join(parts, ".")
			}
		}
	}
	ver, ok = parseSemver(trimmed)
	return ver, prerelease, ok
}

func validBuildMetadata(s string) bool {
	if s == "" {
		return false
	}
	for _, seg := range strings.Split(s, ".") {
		if seg == "" {
			return false
		}
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-') {
				return false
			}
		}
	}
	return true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func splitPEP440Prerelease(patch string) (string, bool) {
	idx := 0
	for idx < len(patch) && patch[idx] >= '0' && patch[idx] <= '9' {
		idx++
	}
	if idx == 0 || idx == len(patch) {
		return "", false
	}
	suffix := patch[idx:]
	for _, tag := range []string{"rc", "a", "b"} {
		if strings.HasPrefix(suffix, tag) && allDigits(suffix[len(tag):]) {
			return patch[:idx], true
		}
	}
	return "", false
}

func cliOutdatedError(cliVersion *string) *ApiError {
	details := map[string]any{}
	cur := strings.TrimSpace(str(cliVersion))
	req := strings.TrimPrefix(strings.TrimSpace(Version), "v")
	if cur != "" {
		details["cli_version"] = cur
	}
	if _, ok := parseSemver(req); ok {
		details["required_version"] = req
	} else {
		req = ""
	}
	msg := "CLI 版本过旧，请升级 CLI。"
	if cur != "" && req != "" {
		msg = fmt.Sprintf("CLI 版本过旧（当前 %s，需要 %s），请升级 CLI。", cur, req)
	} else if cur != "" {
		msg = fmt.Sprintf("CLI 版本过旧（当前 %s），请升级 CLI。", cur)
	}
	var outDetails map[string]any
	if len(details) > 0 {
		outDetails = details
	}
	return &ApiError{
		Code:        "cli_outdated",
		Message:     msg,
		Limitations: []string{},
		Details:     outDetails,
	}
}

func cliProbeError(p InferyardProbe) *ApiError {
	if p.Status == "outdated" {
		return cliOutdatedError(p.Version)
	}
	return Err("cli_unavailable")
}

func (s *Service) probe(u UserSettings) InferyardProbe {
	path := str(u.InferyardPath)
	if s.paths != nil {
		path = s.paths.CLI
	}
	if path == "" {
		path = "inferyard"
	}
	return s.probeCLI(path)
}

func (s *Service) probeCLI(path string) InferyardProbe {
	p, e := exec.LookPath(path)
	if e != nil {
		status := "not_found"
		if _, e := os.Stat(path); e == nil {
			status = "not_executable"
		}
		return InferyardProbe{Status: status}
	}
	p, _ = filepath.Abs(p)
	st, statErr := os.Stat(p)
	if cached, ok := s.cachedProbe(p, st, statErr); ok {
		return cached
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	out, e := s.exec(ctx, p, "--version")
	if e != nil || out.ExitCode != 0 {
		return InferyardProbe{Path: &p, Status: "version_failed"}
	}
	v, source, e := inferyard.ParseVersion(out.Stdout)
	if e != nil {
		return InferyardProbe{Path: &p, Status: "version_failed"}
	}
	if studioVer, ok := parseSemver(Version); ok {
		cliVer, pre, ok := parseCLISemver(v)
		if !ok {
			return InferyardProbe{Path: &p, Version: &v, Source: source, Status: "version_failed"}
		}
		if cmp := compareSemver(cliVer, studioVer); cmp < 0 || (cmp == 0 && pre) {
			return InferyardProbe{Path: &p, Version: &v, Source: source, Status: "outdated"}
		}
	}
	result := InferyardProbe{Path: &p, Version: &v, Source: source, Status: "ok"}
	s.rememberProbe(p, st, statErr, result)
	return result
}

func (s *Service) cachedProbe(p string, st os.FileInfo, statErr error) (InferyardProbe, bool) {
	if statErr != nil {
		return InferyardProbe{}, false
	}
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if s.probeResult.Status != "ok" || s.probePath != p || s.probeVersion != Version || s.probeSize != st.Size() || s.probeModTime != st.ModTime().UnixNano() {
		return InferyardProbe{}, false
	}
	if time.Since(s.probeCheckedAt) >= probeTTL {
		return InferyardProbe{}, false
	}
	return copyProbe(s.probeResult), true
}

func (s *Service) rememberProbe(p string, st os.FileInfo, statErr error, result InferyardProbe) {
	if statErr != nil || result.Status != "ok" {
		return
	}
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	s.probePath = p
	s.probeVersion = Version
	s.probeSize = st.Size()
	s.probeModTime = st.ModTime().UnixNano()
	s.probeCheckedAt = time.Now()
	s.probeResult = copyProbe(result)
}

func (s *Service) clearProbeCache() {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	s.probePath = ""
	s.probeVersion = ""
	s.probeSize = 0
	s.probeModTime = 0
	s.probeCheckedAt = time.Time{}
	s.probeResult = InferyardProbe{}
}

func copyProbe(v InferyardProbe) InferyardProbe {
	c := InferyardProbe{Source: v.Source, Status: v.Status}
	if v.Path != nil {
		c.Path = ptr(*v.Path)
	}
	if v.Version != nil {
		c.Version = ptr(*v.Version)
	}
	return c
}
