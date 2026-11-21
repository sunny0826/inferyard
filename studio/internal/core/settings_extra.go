package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

func allModelsRoots(u UserSettings) []string {
	return activeModelRoots(u)
}

func modelsRootCLIArgs(roots []string) []string {
	args := make([]string, 0, len(roots)*2)
	for _, root := range roots {
		args = append(args, "--models-root", root)
	}
	return args
}

func normalizeExtraModelsRoots(primary *string, raw []string) ([]string, *ApiError) {
	if raw == nil {
		return nil, Err("invalid_request")
	}
	var primaryKey string
	if primary != nil && strings.TrimSpace(*primary) != "" {
		primaryKey = filepath.Clean(strings.TrimSpace(*primary))
		if c, err := canonical(primaryKey); err == nil {
			primaryKey = c
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || !filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, 0) {
			return nil, Err("invalid_request")
		}
		clean := filepath.Clean(trimmed)
		key := clean
		if c, err := canonical(clean); err == nil {
			key = c
		}
		if key == primaryKey || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, clean)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func slicesEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func applySettingsPatchExtra(u *UserSettings, key string, raw json.RawMessage) (bool, *ApiError) {
	switch key {
	case "download_proxy":
		var proxy *string
		if json.Unmarshal(raw, &proxy) != nil {
			return true, Err("invalid_request")
		}
		if proxy == nil || strings.TrimSpace(*proxy) == "" {
			u.DownloadProxy = nil
			return true, nil
		}
		trimmed := strings.TrimRight(strings.TrimSpace(*proxy), "/")
		if !ValidateDownloadProxy(trimmed) {
			return true, Err("invalid_request")
		}
		u.DownloadProxy = &trimmed
		return true, nil
	case "extra_models_roots":
		var roots []string
		if json.Unmarshal(raw, &roots) != nil || roots == nil {
			return true, Err("invalid_request")
		}
		normalized, err := normalizeExtraModelsRoots(u.ModelsRoot, roots)
		if err != nil {
			return true, err
		}
		u.ExtraModelsRoots = normalized
		return true, nil
	default:
		return false, nil
	}
}

func (s *Service) AddExtraModelsRoot(dir string) (SettingsResponse, *ApiError) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" || !filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, 0) {
		return SettingsResponse{}, Err("invalid_request")
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.paths != nil {
		return SettingsResponse{}, Conflict("paths_bound")
	}
	u := s.userSettings()
	combined := append([]string(nil), u.ExtraModelsRoots...)
	combined = append(combined, trimmed)
	normalized, err := normalizeExtraModelsRoots(u.ModelsRoot, combined)
	if err != nil {
		return SettingsResponse{}, err
	}
	u.ExtraModelsRoots = normalized
	return s.saveSettings(u)
}

func (s *Service) RemoveExtraModelsRoot(dir string) (SettingsResponse, *ApiError) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" || strings.ContainsRune(trimmed, 0) {
		return SettingsResponse{}, Err("invalid_request")
	}
	targetClean := filepath.Clean(trimmed)
	targetKey := targetClean
	if c, err := canonical(targetClean); err == nil {
		targetKey = c
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.paths != nil {
		return SettingsResponse{}, Conflict("paths_bound")
	}
	u := s.userSettings()
	kept := make([]string, 0, len(u.ExtraModelsRoots))
	for _, root := range u.ExtraModelsRoots {
		key := filepath.Clean(root)
		if c, err := canonical(key); err == nil {
			key = c
		}
		if key != targetKey {
			kept = append(kept, root)
		}
	}
	if len(kept) == 0 {
		u.ExtraModelsRoots = nil
	} else {
		u.ExtraModelsRoots = kept
	}
	return s.saveSettings(u)
}
