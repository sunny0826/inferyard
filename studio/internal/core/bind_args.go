package core

import (
	"path/filepath"
	"strconv"
)

func BindArgs(candidate, out string, pid int64, endpoint string) ([]string, *ApiError) {
	if candidate == "" || out == "" || pid <= 0 || endpoint == "" {
		return nil, Err("invalid_request")
	}
	return []string{"config", "bind", "--candidate", CandidateFile(candidate), "--pid", strconv.FormatInt(pid, 10), "--endpoint", endpoint, "--out", out}, nil
}

func CandidateFile(candidate string) string {
	if filepath.Ext(candidate) == ".toml" {
		return candidate
	}
	return filepath.Join(candidate, "candidate.toml")
}
