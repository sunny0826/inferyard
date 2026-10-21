package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// CandidateAssets is a projection of the CLI's verified candidate description.
type CandidateAssets struct {
	ModelPath          string `json:"model_path"`
	EnginePath         string `json:"engine_path"`
	InstallationSHA256 string `json:"installation_sha256,omitempty"`
}

func (s *Service) CandidateAssets(candidate string) (CandidateAssets, *ApiError) {
	description, failure := s.DescribeCandidate(s.ctx, candidate)
	if failure != nil {
		return CandidateAssets{}, failure
	}
	result := CandidateAssets{ModelPath: description.Assets.Model.Path}
	if description.Assets.Installation != nil {
		result.InstallationSHA256 = description.Assets.Installation.DeclarationSHA256
	} else if description.Assets.Engine != nil {
		result.EnginePath = description.Assets.Engine.Path
	}
	return result, nil
}

func (s *Service) CandidateMatchesCombination(candidate, modelName, engineName string) (match, known bool) {
	assets, failure := s.CandidateAssets(candidate)
	if failure != nil {
		return false, false
	}
	candidatePath, _ := s.candidateFile(candidate)
	s.mu.Lock()
	origin, preparedHere := s.candidateOrigins[candidatePath]
	s.mu.Unlock()
	modelPath, failure := s.resourcePath(modelName, false)
	if failure != nil {
		return false, false
	}
	for _, entry := range s.registry().engines {
		if entry.engine.Name != engineName {
			continue
		}
		if entry.installation == "" {
			return assets.InstallationSHA256 == "" && assets.ModelPath == modelPath && assets.EnginePath == entry.path, true
		}
		if preparedHere && origin.installationPath == entry.installation && origin.installationKey == installationKey(entry.installation) {
			return assets.ModelPath == modelPath && assets.InstallationSHA256 == origin.declarationHash, true
		}
		s.checkMu.Lock()
		check, found := s.checks[entry.path]
		s.checkMu.Unlock()
		if !found || check.key != installationKey(entry.path) || check.result.DeclarationSHA256 == nil {
			return false, false
		}
		return assets.ModelPath == modelPath && assets.InstallationSHA256 == *check.result.DeclarationSHA256, true
	}
	return false, false
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
