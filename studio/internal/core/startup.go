package core

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Startup struct {
	Path string   `json:"path"`
	Args []string `json:"startup_args"`
}
type ProcessCandidate struct {
	PID  int64  `json:"pid"`
	Name string `json:"name"`
}

func (s *Service) Startup(candidate string) (Startup, *ApiError) {
	w := workspaceRoot(s.userSettings())
	if w == nil {
		return Startup{}, Conflict("settings_incomplete")
	}
	path, err := Resolve(*w, candidate)
	if err != nil {
		return Startup{}, Err("invalid_request")
	}
	if st, e := os.Stat(path); e == nil && st.IsDir() {
		path = filepath.Join(path, "candidate.toml")
	}
	if _, err = Resolve(*w, path); err != nil {
		return Startup{}, Err("invalid_request")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Startup{}, Err("not_found")
	}
	result := Startup{Args: []string{}}
	section := ""
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "engine" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k != "binary_path" && k != "startup_args" {
			continue
		}
		if seen[k] {
			return Startup{}, Err("cli_error")
		}
		seen[k] = true
		var err error
		if k == "binary_path" {
			err = json.Unmarshal([]byte(strings.TrimSpace(v)), &result.Path)
		} else {
			err = json.Unmarshal([]byte(strings.TrimSpace(v)), &result.Args)
		}
		if err != nil {
			return Startup{}, Err("cli_error")
		}
	}
	if result.Path == "" || !seen["startup_args"] || result.Args == nil {
		return Startup{}, Err("cli_error")
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
