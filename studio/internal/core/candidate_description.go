package core

import (
	"context"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strings"
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

func installationsFingerprint(installations []string) string {
	if len(installations) == 0 {
		return ""
	}
	var b strings.Builder
	for _, raw := range installations {
		b.WriteString(raw)
		if st, err := os.Stat(raw); err == nil {
			fmt.Fprintf(&b, ":%d:%d;", st.Size(), st.ModTime().UnixNano())
		} else {
			b.WriteString(":missing;")
		}
	}
	return b.String()
}

func statPair(path string) (size, modTime int64, ok bool) {
	if strings.TrimSpace(path) == "" {
		return 0, 0, true
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, 0, false
	}
	return st.Size(), st.ModTime().UnixNano(), true
}

func (s *Service) DescribeCandidate(ctx context.Context, candidate string) (inferyard.CandidateDescription, *ApiError) {
	var result inferyard.CandidateDescription
	path, failure := s.candidateFile(candidate)
	if failure != nil {
		return result, failure
	}
	u := s.userSettings()
	probe := s.probe(u)
	if probe.Status != "ok" {
		return result, cliProbeError(probe)
	}
	st, statErr := os.Stat(path)
	cliKey := *probe.Path + "|" + str(probe.Version)
	instKey := installationsFingerprint(u.EngineInstallations)
	if statErr == nil {
		s.caches.mu.Lock()
		entry, ok := s.caches.candidates[path]
		s.caches.mu.Unlock()
		if ok && entry.size == st.Size() && entry.modTime == st.ModTime().UnixNano() && entry.cliKey == cliKey && entry.installsKey == instKey {
			engSz, engMT, engOK := statPair(entry.enginePath)
			modSz, modMT, modOK := statPair(entry.modelPath)
			if engOK && modOK && engSz == entry.engineSize && engMT == entry.engineMTime && modSz == entry.modelSize && modMT == entry.modelMTime {
				return clone(entry.desc), nil
			}
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
	if statErr == nil {
		engPath := ""
		if result.Assets.Engine != nil {
			engPath = result.Assets.Engine.Path
		}
		modPath := result.Assets.Model.Path
		engSz, engMT, engOK := statPair(engPath)
		modSz, modMT, modOK := statPair(modPath)
		if engOK && modOK {
			s.caches.mu.Lock()
			if s.caches.candidates == nil {
				s.caches.candidates = map[string]cachedCandidateDesc{}
			}
			s.caches.candidates[path] = cachedCandidateDesc{
				size:        st.Size(),
				modTime:     st.ModTime().UnixNano(),
				cliKey:      cliKey,
				installsKey: instKey,
				enginePath:  engPath,
				engineSize:  engSz,
				engineMTime: engMT,
				modelPath:   modPath,
				modelSize:   modSz,
				modelMTime:  modMT,
				desc:        clone(result),
			}
			s.caches.mu.Unlock()
		}
	}
	return result, nil
}
