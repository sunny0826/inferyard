package core

import (
	"context"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"time"
)

func (s *Service) candidateFile(candidate string) (string, *ApiError) {
	workspace := workspaceRoot(s.userSettings())
	if workspace == nil {
		return "", Conflict("settings_incomplete")
	}
	path, err := Resolve(*workspace, candidate)
	if err != nil {
		return "", Err("invalid_request")
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		path = filepath.Join(path, "candidate.toml")
	}
	path, err = Resolve(*workspace, path)
	if err != nil {
		return "", Err("invalid_request")
	}
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return "", Err("not_found")
	}
	return path, nil
}

type candidateDescCacheEntry struct {
	cliKey  string
	size    int64
	modTime int64
	desc    inferyard.CandidateDescription
}

func (s *Service) clearDescribeCache() {
	s.describeMu.Lock()
	s.describeCache = nil
	s.describeMu.Unlock()
}

func (s *Service) DescribeCandidate(ctx context.Context, candidate string) (inferyard.CandidateDescription, *ApiError) {
	var result inferyard.CandidateDescription
	path, failure := s.candidateFile(candidate)
	if failure != nil {
		return result, failure
	}
	probe := s.probe(s.userSettings())
	if probe.Status != "ok" {
		return result, cliProbeError(probe)
	}
	cliKey := str(probe.Path) + "|" + str(probe.Version)
	st, statErr := os.Stat(path)
	if statErr == nil && st.Mode().IsRegular() {
		s.describeMu.Lock()
		cached, ok := s.describeCache[path]
		s.describeMu.Unlock()
		if ok && cached.cliKey == cliKey && cached.size == st.Size() && cached.modTime == st.ModTime().UnixNano() {
			return cached.desc, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := s.exec(ctx, *probe.Path, "config", "describe", "--candidate", path)
	if ctx.Err() != nil {
		return result, Err("cancelled")
	}
	if failure := exitError(out, err); failure != nil {
		return result, failure
	}
	result, err = inferyard.ParseCandidateDescription(out.Stdout, out.ExitCode)
	if err != nil {
		return result, Err("candidate_description_invalid")
	}
	if after, err := os.Stat(path); err == nil && after.Mode().IsRegular() && statErr == nil && after.Size() == st.Size() && after.ModTime().UnixNano() == st.ModTime().UnixNano() {
		s.describeMu.Lock()
		if s.describeCache == nil || len(s.describeCache) >= 64 {
			s.describeCache = map[string]candidateDescCacheEntry{}
		}
		s.describeCache[path] = candidateDescCacheEntry{
			cliKey:  cliKey,
			size:    after.Size(),
			modTime: after.ModTime().UnixNano(),
			desc:    result,
		}
		s.describeMu.Unlock()
	}
	return result, nil
}
