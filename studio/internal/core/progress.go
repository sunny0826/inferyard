package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
	"path/filepath"
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
	args := []string{"progress", "--workspace", *w}
	if u.ResultsRoot != nil && !sameDirPath(*u.ResultsRoot, filepath.Join(*w, "results")) {
		args = append(args, "--results", *u.ResultsRoot)
	}
	if rp := reportsRoot(u); rp != nil && !sameDirPath(*rp, filepath.Join(*w, "reports")) {
		args = append(args, "--reports", *rp)
	}
	out, err := s.exec(ctx, *p.Path, args...)
	if err != nil {
		return ProgressResponse{}, Err("internal_error")
	}
	result := ProgressResponse{Status: inferyard.MapExitCode(out.ExitCode), ExitCode: out.ExitCode}
	if out.ExitCode == inferyard.ExitBlocked {
		var header struct {
			Command     string   `json:"command"`
			Limitations []string `json:"limitations"`
		}
		if json.Unmarshal(out.Stdout, &header) != nil || header.Command != "progress" {
			return result, &ApiError{Code: "cli_outdated", Message: l10n.TR("core.cli.err.missing_progress_cmd")}
		}
		if len(header.Limitations) > 0 {
			return result, &ApiError{Code: header.Limitations[0], Limitations: header.Limitations}
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

func sameDirPath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	ra, errA := canonical(ca)
	rb, errB := canonical(cb)
	return errA == nil && errB == nil && ra == rb
}
