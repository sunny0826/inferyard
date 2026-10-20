package core

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Startup struct {
	Path             string            `json:"path"`
	Args             []string          `json:"startup_args"`
	Kind             string            `json:"kind"`
	Family           string            `json:"family"`
	Local            bool              `json:"local"`
	WorkingDirectory string            `json:"working_directory"`
	Environment      map[string]string `json:"environment"`
	Endpoint         string            `json:"endpoint"`
}
type ProcessCandidate struct {
	PID  int64  `json:"pid"`
	Name string `json:"name"`
}

func (s *Service) Startup(candidate string) (Startup, *ApiError) {
	description, failure := s.DescribeCandidate(s.ctx, candidate)
	if failure != nil {
		return Startup{}, failure
	}
	launch := description.Launch
	result := Startup{Kind: launch.Kind, Family: description.Family,
		Local:            description.Definition == "local-engine-description.v2",
		WorkingDirectory: str(launch.WorkingDirectory), Environment: launch.Environment,
		Endpoint: description.Endpoint.URL, Args: []string{}}
	if launch.Kind == "direct_process" {
		result.Path, result.Args = launch.Argv[0], launch.Argv[1:]
	}
	return result, nil
}
func (s *Service) Processes(ctx context.Context) ([]ProcessCandidate, *ApiError) {
	// Only executable names are exposed; process argv may contain credentials.
	result := []ProcessCandidate{}
	if runtime.GOOS == "windows" {
		raw, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Get-Process | Select-Object Id,ProcessName | ConvertTo-Json -Compress").Output()
		if err != nil {
			return nil, Err("internal_error")
		}
		var entries []struct {
			ID   int64  `json:"Id"`
			Name string `json:"ProcessName"`
		}
		if json.Unmarshal(raw, &entries) != nil {
			return nil, Err("internal_error")
		}
		for _, p := range entries {
			if p.ID > 0 {
				result = append(result, ProcessCandidate{p.ID, p.Name})
			}
		}
	} else {
		raw, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,comm=").Output()
		if err != nil {
			return nil, Err("internal_error")
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			pid, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil && pid > 0 {
				result = append(result, ProcessCandidate{pid, filepath.Base(strings.Join(fields[1:], " "))})
			}
		}
	}
	return result, nil
}
