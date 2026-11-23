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

func cliVersionOutdated(cliVersion, studioVersion string) bool {
	studioVer, ok := parseSemver(studioVersion)
	if !ok {
		return false
	}
	cliVer, ok := parseSemver(cliVersion)
	if !ok {
		return false
	}
	return compareSemver(cliVer, studioVer) < 0
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
	if cliVersionOutdated(v, Version) {
		return InferyardProbe{Path: &p, Version: &v, Source: source, Status: "outdated"}
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
