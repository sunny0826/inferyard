package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// BindingAlive is a read-only liveness hint, never an identity acceptance gate
// or a claim of ownership. CLI bind/run still perform all identity checks.
func (s *Service) BindingAlive(bound string) (bool, *ApiError) {
	w := workspaceRoot(s.userSettings())
	if w == nil {
		return false, Conflict("settings_incomplete")
	}
	path, err := Resolve(*w, bound)
	if err != nil {
		return false, Err("invalid_request")
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		path = filepath.Join(path, "config.toml")
	}
	path, err = Resolve(*w, path)
	if err != nil {
		return false, Err("invalid_request")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, Err("not_found")
	}
	section := ""
	fields := map[string]int64{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "endpoint" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || (key != "server_pid" && key != "process_start_ticks") {
			continue
		}
		var n int64
		if _, seen := fields[key]; seen {
			return false, Err("cli_error")
		}
		if json.Unmarshal([]byte(strings.TrimSpace(value)), &n) != nil || n <= 0 {
			return false, Err("cli_error")
		}
		fields[key] = n
	}
	if len(fields) != 2 {
		return false, Err("cli_error")
	}
	ticks, err := processStartTicks(fields["server_pid"])
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, Err("service_identity_unreadable")
	}
	return ticks == fields["process_start_ticks"], nil
}
