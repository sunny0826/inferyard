package core

import (
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func Err(code string) *ApiError {
	if code == "output_name_exhausted" {
		return &ApiError{Code: code, Message: l10n.TR("core.err.output_name_exhausted")}
	}
	var msg string
	switch code {
	case "invalid_request":
		msg = l10n.TR("core.err.invalid_request")
	case "not_found":
		msg = l10n.TR("core.err.not_found")
	case "conflict":
		msg = l10n.TR("core.err.conflict")
	case "cli_blocked":
		msg = l10n.TR("core.err.cli_blocked")
	case "internal_error":
		msg = l10n.TR("core.err.internal_error")
	case "cli_error":
		msg = l10n.TR("core.err.cli_error")
	case "cli_unavailable":
		msg = l10n.TR("core.err.cli_unavailable")
	case "cli_outdated":
		msg = l10n.TR("core.err.cli_outdated")
	}
	return &ApiError{Code: code, Message: msg, Limitations: []string{}}
}

func Conflict(reason string) *ApiError {
	e := Err("conflict")
	e.Details = map[string]any{"reason": reason}
	if reason == "output_exists" {
		e.Message = l10n.TR("core.err.output_exists")
	}
	return e
}

func (e *ApiError) Error() string { return e.Message }

func exitError(out inferyard.Execution, err error) *ApiError {
	if err != nil {
		return Err("internal_error")
	}
	if out.ExitCode == 0 || out.ExitCode == 3 || out.ExitCode == 130 {
		return nil
	}
	code := "cli_error"
	if out.ExitCode == 2 {
		code = "cli_blocked"
	}
	e := Err(code)
	if r, p := inferyard.Parse(out.Stdout); p == nil {
		if r.Limitations != nil {
			e.Limitations = r.Limitations
		}
		if len(r.Details) > 0 && string(r.Details) != "null" {
			var parsed map[string]any
			if json.Unmarshal(r.Details, &parsed) == nil {
				sanitized := map[string]any{}
				for _, key := range []string{"failure_stage", "failure_cause"} {
					if v, ok := parsed[key].(string); ok && v != "" {
						sanitized[key] = v
					}
				}
				if len(sanitized) > 0 {
					e.Details = sanitized
				}
			}
		}
	}
	return e
}

func (s *Service) Status() StatusResponse {
	p := s.probe(s.userSettings())
	s.mu.Lock()
	defer s.mu.Unlock()
	r := StatusResponse{Status: "ok", Version: Version, Inferyard: p}
	for _, t := range s.runs {
		if !terminal(t.Task.State) {
			r.ActiveRunTasks++
		}
	}
	for _, d := range s.downloads {
		if d.Task.State == "downloading" {
			r.ActiveDownloads++
		}
	}
	return r
}

func (s *Service) Device() DeviceResponse { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.device) }

func (s *Service) RefreshDevice() *ApiError {
	s.mu.Lock()
	if s.device.Refreshing {
		s.mu.Unlock()
		return Conflict("refresh_in_progress")
	}
	u := clone(s.settings)
	roots := s.allModelsRoots(u)
	if len(roots) == 0 {
		s.mu.Unlock()
		return Conflict("settings_incomplete")
	}
	s.device.Refreshing = true
	gen := s.deviceGeneration
	capturedAt := now()
	s.mu.Unlock()
	go func() {
		p := s.probe(u)
		var failure *ApiError
		var snapshot json.RawMessage
		if p.Status != "ok" {
			failure = cliProbeError(p)
		} else {
			deviceArgs := append([]string{"device-check", "--json"}, modelsRootCLIArgs(roots)...)
			out, e := s.exec(s.ctx, *p.Path, deviceArgs...)
			if e != nil {
				failure = Err("internal_error")
			} else if out.ExitCode != 0 && out.ExitCode != 2 {
				failure = exitError(out, nil)
				if failure == nil {
					failure = Err("cli_error")
				}
			} else {
				r, e := inferyard.Parse(out.Stdout)
				if e == nil {
					_, e = inferyard.ParseDeviceCheck(r)
				}
				if e != nil {
					failure = Err("internal_error")
				} else {
					snapshot = r.Details
				}
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if gen != s.deviceGeneration {
			return
		}
		s.device.Refreshing = false
		if failure == nil {
			s.device.Snapshot = snapshot
			s.device.CapturedAt = ptr(capturedAt)
		}
		s.device.LastError = failure
		s.Bus.Publish("device.updated", map[string]any{"snapshot": s.device.Snapshot, "captured_at": s.device.CapturedAt, "error": failure})
	}()
	return nil
}

func terminal(state string) bool { return state == "done" || state == "failed" || state == "cancelled" }
