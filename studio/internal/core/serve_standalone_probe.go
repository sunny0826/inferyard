package core

import (
	"context"
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func (s *Service) configuredCLIPath(u UserSettings) string {
	path := str(u.InferyardPath)
	if s.paths != nil {
		path = s.paths.CLI
	}
	if path == "" {
		path = "inferyard"
	}
	return path
}

func (s *Service) clearServeSupport() {
	s.probeMu.Lock()
	s.probeGen++
	s.serveProbePath = ""
	s.serveProbeVersion = ""
	s.serveProbeSize = 0
	s.serveProbeModTime = 0
	s.serveProbeChecked = time.Time{}
	s.serveSupported = false
	s.probeMu.Unlock()
}

func (s *Service) cachedServeSupportLocked(cliPath string, st os.FileInfo) (bool, bool) {
	if s.serveProbeChecked.IsZero() ||
		s.serveProbePath != cliPath ||
		s.serveProbeVersion != Version ||
		s.serveProbeSize != st.Size() ||
		s.serveProbeModTime != st.ModTime().UnixNano() {
		return false, false
	}
	if !s.serveSupported && time.Since(s.serveProbeChecked) >= probeTTL {
		return false, false
	}
	return s.serveSupported, true
}

func (s *Service) ServeSupported(ctx context.Context) bool {
	u := s.userSettings()
	rawPath := s.configuredCLIPath(u)
	found, lookErr := exec.LookPath(rawPath)
	if lookErr != nil {
		s.clearServeSupport()
		return false
	}
	absPath, _ := filepath.Abs(found)
	st, statErr := os.Stat(absPath)
	if statErr == nil {
		s.probeMu.Lock()
		ok, cached := s.cachedServeSupportLocked(absPath, st)
		s.probeMu.Unlock()
		if cached {
			return ok
		}
	}
	p := s.probe(u)
	if p.Status != "ok" || p.Path == nil || *p.Path == "" {
		if statErr == nil && ctx.Err() == nil {
			s.probeMu.Lock()
			s.serveProbePath = absPath
			s.serveProbeVersion = Version
			s.serveProbeSize = st.Size()
			s.serveProbeModTime = st.ModTime().UnixNano()
			s.serveProbeChecked = time.Now()
			s.serveSupported = false
			s.probeMu.Unlock()
		} else {
			s.clearServeSupport()
		}
		return false
	}
	return s.checkServeSupported(ctx, *p.Path)
}

func (s *Service) checkServeSupported(ctx context.Context, cliPath string) bool {
	st, statErr := os.Stat(cliPath)
	if statErr == nil {
		s.probeMu.Lock()
		ok, cached := s.cachedServeSupportLocked(cliPath, st)
		s.probeMu.Unlock()
		if cached {
			return ok
		}
	}
	s.serveProbeExecMu.Lock()
	defer s.serveProbeExecMu.Unlock()
	st, statErr = os.Stat(cliPath)
	if statErr == nil {
		s.probeMu.Lock()
		ok, cached := s.cachedServeSupportLocked(cliPath, st)
		s.probeMu.Unlock()
		if cached {
			return ok
		}
	}
	s.probeMu.Lock()
	gen := s.probeGen
	s.probeMu.Unlock()
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := s.exec(probeCtx, cliPath, "--schema", "serve_plan")
	supported := err == nil && out.ExitCode == 0 && inferyard.SupportsServePlanSchema(out.Stdout)
	if probeCtx.Err() == nil && statErr == nil {
		s.probeMu.Lock()
		if s.probeGen == gen {
			s.serveProbePath = cliPath
			s.serveProbeVersion = Version
			s.serveProbeSize = st.Size()
			s.serveProbeModTime = st.ModTime().UnixNano()
			s.serveProbeChecked = time.Now()
			s.serveSupported = supported
		}
		s.probeMu.Unlock()
	}
	return supported
}

// resolveServeCLI verifies the CLI supports standalone serve; when the binary
// fingerprint matches a previously confirmed positive probe, it skips spawning
// CLI subprocesses even after probeTTL expires.
func (s *Service) resolveServeCLI(ctx context.Context, u UserSettings) (string, *ApiError) {
	rawPath := s.configuredCLIPath(u)
	found, err := exec.LookPath(rawPath)
	if err != nil {
		s.clearServeSupport()
		return "", Err("cli_unavailable")
	}
	absPath, _ := filepath.Abs(found)
	if st, statErr := os.Stat(absPath); statErr == nil {
		s.probeMu.Lock()
		fingerprintOK := s.serveSupported &&
			s.serveProbePath == absPath &&
			s.serveProbeVersion == Version &&
			s.serveProbeSize == st.Size() &&
			s.serveProbeModTime == st.ModTime().UnixNano()
		s.probeMu.Unlock()
		if fingerprintOK {
			return absPath, nil
		}
	}
	p := s.probe(u)
	if p.Status != "ok" || p.Path == nil || *p.Path == "" {
		s.clearServeSupport()
		return "", cliProbeError(p)
	}
	if !s.checkServeSupported(ctx, *p.Path) {
		if ctx.Err() != nil {
			return "", Err("cancelled")
		}
		return "", cliOutdatedError(p.Version)
	}
	return *p.Path, nil
}
