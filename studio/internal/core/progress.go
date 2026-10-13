package core

import (
	"context"
	"inferyard/studio/internal/inferyard"
)

type ProgressResponse struct {
	Status   inferyard.ExitStatus         `json:"status"`
	ExitCode int                          `json:"exit_code"`
	Progress *inferyard.WorkspaceProgress `json:"progress"`
}

func (s *Service) Progress(ctx context.Context) (ProgressResponse, *ApiError) {
	u := s.userSettings()
	p := s.probe(u)
	if p.Status != "ok" {
		return ProgressResponse{}, Err("cli_unavailable")
	}
	w := workspaceRoot(u)
	if w == nil {
		return ProgressResponse{}, Conflict("settings_incomplete")
	}
	out, err := s.exec(ctx, *p.Path, "progress", "--workspace", *w)
	if err != nil {
		return ProgressResponse{}, Err("internal_error")
	}
	result := ProgressResponse{Status: inferyard.MapExitCode(out.ExitCode), ExitCode: out.ExitCode}
	if out.ExitCode == 2 {
		return result, nil
	}
	if out.ExitCode != 0 {
		return result, Err("cli_error")
	}
	value, err := inferyard.ParseProgress(out.Stdout)
	if err != nil {
		return result, Err("cli_error")
	}
	result.Progress = &value
	return result, nil
}
