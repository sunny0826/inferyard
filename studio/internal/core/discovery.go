package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
)

func (s *Service) Discover(ctx context.Context) (inferyard.ServiceDiscovery, *ApiError) {
	p := s.probe(s.userSettings())
	if p.Status != "ok" {
		return inferyard.ServiceDiscovery{}, Err("cli_outdated")
	}
	out, err := s.exec(ctx, *p.Path, "service", "discover")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) {
			return inferyard.ServiceDiscovery{}, Err("cli_outdated")
		}
		return inferyard.ServiceDiscovery{}, Err("service_discovery_error")
	}
	if out.ExitCode == 2 {
		var header struct {
			Command string `json:"command"`
		}
		// Like progress, old CLIs reject unknown arguments with command "unknown".
		if json.Unmarshal(out.Stdout, &header) != nil || header.Command != "service discover" {
			return inferyard.ServiceDiscovery{}, Err("cli_outdated")
		}
	}
	result, err := inferyard.ParseServiceDiscovery(out.Stdout, out.ExitCode)
	if err != nil {
		return inferyard.ServiceDiscovery{}, Err("service_discovery_invalid_response")
	}
	if out.ExitCode == 2 || out.ExitCode == 130 {
		code := "cli_blocked"
		if out.ExitCode == 130 {
			code = "cancelled"
		}
		e := Err(code)
		e.Limitations = result.Limitations
		return result, e
	}
	if out.ExitCode == 4 {
		return result, &ApiError{Code: "service_discovery_error", Limitations: result.Limitations}
	}
	return result, nil
}
