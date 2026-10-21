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

func (s *Service) DescribeCandidate(ctx context.Context, candidate string) (inferyard.CandidateDescription, *ApiError) {
	var result inferyard.CandidateDescription
	path, failure := s.candidateFile(candidate)
	if failure != nil {
		return result, failure
	}
	probe := s.probe(s.userSettings())
	if probe.Status != "ok" {
		return result, Err("cli_unavailable")
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
	return result, nil
}
