package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
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
		return ProgressResponse{}, cliProbeError(p)
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
	if out.ExitCode == inferyard.ExitBlocked {
		var header struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(out.Stdout, &header) != nil || header.Command != "progress" {
			return result, &ApiError{Code: "cli_outdated", Message: l10n.TR("core.cli.err.missing_progress_cmd")}
		}
		return result, nil
	}
	if out.ExitCode != inferyard.ExitComplete {
		return result, Err("cli_error")
	}
	value, err := inferyard.ParseProgress(out.Stdout)
	if err != nil {
		var verErr inferyard.ProgressVersionError
		if errors.As(err, &verErr) && verErr.Version > 0 && verErr.Version < 2 {
			return result, &ApiError{Code: "cli_outdated", Message: l10n.TR("core.cli.err.outdated")}
		}
		return result, Err("cli_error")
	}
	result.Progress = &value
	return result, nil
}
