package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// CandidateAssets holds the canonical model and engine paths frozen into one
// candidate.toml. It answers "does this candidate belong to the selected
// combination" without exposing paths to the pages layer.
type CandidateAssets struct {
	ModelPath  string `json:"model_path"`
	EnginePath string `json:"engine_path"`
}

// CandidateAssets reads [model] local_path and [engine] binary_path from a
// frozen candidate inside the workspace, following the same line-oriented
// parsing as Startup (no TOML dependency; values are JSON strings).
func (s *Service) CandidateAssets(candidate string) (CandidateAssets, *ApiError) {
	w := workspaceRoot(s.userSettings())
	if w == nil {
		return CandidateAssets{}, Conflict("settings_incomplete")
	}
	path, err := Resolve(*w, candidate)
	if err != nil {
		return CandidateAssets{}, Err("invalid_request")
	}
	if st, e := os.Stat(path); e == nil && st.IsDir() {
		path = filepath.Join(path, "candidate.toml")
	}
	if _, err = Resolve(*w, path); err != nil {
		return CandidateAssets{}, Err("invalid_request")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return CandidateAssets{}, Err("not_found")
	}
	values := map[string]string{}
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		var want string
		switch {
		case section == "model" && k == "local_path":
			want = "model"
		case section == "engine" && k == "binary_path":
			want = "engine"
		default:
			continue
		}
		if values[want] != "" {
			return CandidateAssets{}, Err("cli_error")
		}
		var decoded string
		if json.Unmarshal([]byte(strings.TrimSpace(v)), &decoded) != nil || decoded == "" {
			return CandidateAssets{}, Err("cli_error")
		}
		values[want] = decoded
	}
	if values["model"] == "" || values["engine"] == "" {
		return CandidateAssets{}, Err("cli_error")
	}
	assets := CandidateAssets{}
	for _, pair := range [][2]string{{"model", values["model"]}, {"engine", values["engine"]}} {
		canonical, err := canonical(pair[1])
		if err != nil || !filepath.IsAbs(canonical) {
			return CandidateAssets{}, Err("cli_error")
		}
		if pair[0] == "model" {
			assets.ModelPath = canonical
		} else {
			assets.EnginePath = canonical
		}
	}
	return assets, nil
}

// CandidateMatchesCombination reports whether the candidate froze exactly the
// registry resources behind modelName and engineName. known is false when the
// candidate cannot be read or a name no longer resolves, so callers present
// "cannot attribute" instead of guessing.
func (s *Service) CandidateMatchesCombination(candidate, modelName, engineName string) (match, known bool) {
	assets, e := s.CandidateAssets(candidate)
	if e != nil {
		return false, false
	}
	modelPath, e := s.resourcePath(modelName, false)
	if e != nil {
		return false, false
	}
	enginePath, e := s.resourcePath(engineName, true)
	if e != nil {
		return false, false
	}
	return assets.ModelPath == modelPath && assets.EnginePath == enginePath, true
}

// PreflightModelPath returns the canonical model path recommended by one
// device-preflight.json. A preflight without a runnable recommendation maps
// to device_preflight_blocked, carrying the fixed recommendation reason as
// the first limitation; the file must stay inside the workspace.
func (s *Service) PreflightModelPath(preflight string) (string, *ApiError) {
	w := workspaceRoot(s.userSettings())
	if w == nil {
		return "", Conflict("settings_incomplete")
	}
	path, err := Resolve(*w, preflight)
	if err != nil {
		return "", Err("invalid_request")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", Err("not_found")
	}
	var report struct {
		Details        json.RawMessage `json:"details"`
		Recommendation struct {
			Status   string `json:"status"`
			Reason   string `json:"reason"`
			Selected struct {
				ModelPath string `json:"model_path"`
			} `json:"selected"`
		} `json:"recommendation"`
	}
	if json.Unmarshal(raw, &report) != nil {
		return "", Err("cli_error")
	}
	if len(report.Details) > 0 && string(report.Details) != "null" {
		if json.Unmarshal(report.Details, &report) != nil {
			return "", Err("cli_error")
		}
	}
	if report.Recommendation.Status != "recommended" {
		e := Err("device_preflight_blocked")
		if fixedReason.MatchString(report.Recommendation.Reason) {
			e.Limitations = []string{report.Recommendation.Reason}
		}
		return "", e
	}
	if report.Recommendation.Selected.ModelPath == "" {
		return "", Err("cli_error")
	}
	model, err := canonical(report.Recommendation.Selected.ModelPath)
	if err != nil || !filepath.IsAbs(model) {
		return "", Err("cli_error")
	}
	return model, nil
}

// PreflightMatchesModel reports whether the preflight recommends exactly the
// model behind modelName. Preflight failures propagate so the start chain can
// stop with the real reason.
func (s *Service) PreflightMatchesModel(preflight, modelName string) (bool, *ApiError) {
	recommended, e := s.PreflightModelPath(preflight)
	if e != nil {
		return false, e
	}
	selected, e := s.resourcePath(modelName, false)
	if e != nil {
		return false, e
	}
	return recommended == selected, nil
}
