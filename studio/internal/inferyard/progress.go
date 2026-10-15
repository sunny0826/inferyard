package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"unicode/utf8"
)

type WorkspaceProgress struct {
	Kind                 string            `json:"kind"`
	ProgressVersion      int               `json:"progress_version"`
	Workspace            string            `json:"workspace"`
	DevicePreflight      ProgressPreflight `json:"device_preflight"`
	Bundles              []BundleProgress  `json:"bundles"`
	Report               ProgressReport    `json:"report"`
	NextAction           string            `json:"next_action"`
	NextCommand          *string           `json:"next_command"`
	DefaultScopeComplete bool              `json:"default_scope_complete"`
}
type ProgressPreflight struct {
	Status string  `json:"status"`
	Path   *string `json:"path"`
}
type ProgressRun struct {
	Path     string `json:"path"`
	Complete bool   `json:"complete"`
	Sealed   bool   `json:"sealed"`
}
type BundleProgress struct {
	Bundle        string        `json:"bundle"`
	Phase         string        `json:"phase"`
	CandidatePath *string       `json:"candidate_path"`
	BoundPath     *string       `json:"bound_path"`
	Runs          []ProgressRun `json:"runs"`
	NextAction    string        `json:"next_action"`
	NextCommand   *string       `json:"next_command"`
	BlockedReason *string       `json:"blocked_reason"`
}
type ProgressReport struct {
	Status string   `json:"status"`
	Path   *string  `json:"path"`
	Runs   []string `json:"runs"`
}

// Check the wire structure, including required nullable fields, before decoding.
// This adapter does not derive phases or interpret benchmark evidence.
func ParseProgress(raw []byte) (WorkspaceProgress, error) {
	var result WorkspaceProgress
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return result, errors.New("stdout must be one JSON object")
	}
	if !utf8.Valid(raw) {
		return result, errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSON(d); err != nil {
		return result, err
	}
	if _, err := d.Token(); err != io.EOF {
		return result, errors.New("trailing JSON")
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if result.Kind != "progress" {
		return result, errors.New("unsupported progress kind")
	}
	if result.ProgressVersion != 1 {
		return result, errors.New("unsupported progress version")
	}
	if result.Workspace == "" {
		return result, errors.New("workspace is empty")
	}
	if result.Bundles == nil {
		return result, errors.New("bundles must be an array")
	}
	for _, b := range result.Bundles {
		if b.Bundle == "" || b.Phase == "" || b.NextAction == "" {
			return result, errors.New("missing required fields")
		}
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || require(obj, "kind", "progress_version", "workspace", "device_preflight", "bundles", "report", "next_action", "next_command", "default_scope_complete") != nil {
		return result, errors.New("missing progress fields")
	}
	if !slices.Contains([]string{"device_check", "prepare_candidate", "bind", "run", "report", "verify", "done"}, result.NextAction) {
		return result, errors.New("invalid progress action")
	}
	var pre, report map[string]json.RawMessage
	json.Unmarshal(obj["device_preflight"], &pre)
	json.Unmarshal(obj["report"], &report)
	if require(pre, "status", "path") != nil || require(report, "status", "path", "runs") != nil || result.Report.Runs == nil || !slices.Contains([]string{"ok", "missing"}, result.DevicePreflight.Status) || !slices.Contains([]string{"missing", "present", "verified"}, result.Report.Status) {
		return result, errors.New("invalid progress state")
	}
	var bundles []map[string]json.RawMessage
	json.Unmarshal(obj["bundles"], &bundles)
	for i, b := range result.Bundles {
		if require(bundles[i], "bundle", "phase", "candidate_path", "bound_path", "runs", "next_action", "next_command", "blocked_reason") != nil || b.Bundle == "" || b.Runs == nil || !slices.Contains([]string{"preflight_missing", "candidate_missing", "bound_missing", "run_missing", "run_incomplete", "run_complete"}, b.Phase) || !slices.Contains([]string{"device_check", "prepare_candidate", "bind", "run", "resume", "none"}, b.NextAction) {
			return result, errors.New("invalid bundle progress")
		}
		var runs []map[string]json.RawMessage
		json.Unmarshal(bundles[i]["runs"], &runs)
		for j, r := range b.Runs {
			if require(runs[j], "path", "complete", "sealed") != nil || r.Path == "" {
				return result, errors.New("invalid run progress")
			}
		}
	}
	return result, nil
}
func require(obj map[string]json.RawMessage, names ...string) error {
	for _, name := range names {
		value, ok := obj[name]
		nullable := slices.Contains([]string{"path", "candidate_path", "bound_path", "next_command", "blocked_reason"}, name)
		if !ok || (!nullable && bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
			return errors.New("missing field")
		}
	}
	return nil
}
func uniqueJSON(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			name, ok := k.(string)
			if !ok || seen[name] {
				return errors.New("duplicate JSON key")
			}
			seen[name] = true
			if e = uniqueJSON(d); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e := uniqueJSON(d); e != nil {
				return e
			}
		}
	default:
		return errors.New("invalid JSON")
	}
	_, err = d.Token()
	return err
}
