package core

import (
	"path/filepath"
	"strconv"
)

// BindArgs builds the `config bind` argv shared by manual binding, the
// benchmark chain and the specialist flow. The CLI owns the argument
// semantics; Studio only fills the four values it learns at bind time.
func BindArgs(candidate, out string, pid int64, endpoint string) ([]string, *ApiError) {
	if candidate == "" || out == "" || pid <= 0 || endpoint == "" {
		return nil, Err("invalid_request")
	}
	return []string{"config", "bind", "--candidate", CandidateFile(candidate), "--pid", strconv.FormatInt(pid, 10), "--endpoint", endpoint, "--out", out}, nil
}

// CandidateFile accepts either a candidate directory or its candidate.toml.
func CandidateFile(candidate string) string {
	if filepath.Ext(candidate) == ".toml" {
		return candidate
	}
	return filepath.Join(candidate, "candidate.toml")
}
