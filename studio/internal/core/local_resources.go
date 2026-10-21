package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const installationCheckTimeout = 5 * time.Minute
const maxInstallationBytes = 1 << 20

func localInstallationReason(reason string) string {
	switch reason {
	case "local_engine_requires_darwin_arm64", "local_engine_json_unreadable",
		"local_engine_absolute_path_required", "local_engine_locator_missing",
		"local_engine_runtime_root_invalid", "local_engine_cwd_invalid",
		"local_engine_executable_invalid", "local_engine_models_root_required_for_lmstudio_only",
		"local_engine_installation_unreadable", "local_engine_non_regular_asset",
		"local_engine_directory_symlink", "local_engine_python_venv_required",
		"local_engine_python_probe_failed", "local_engine_runtime_changed_during_hash",
		"local_engine_runtime_unreadable", "local_engine_runtime_invalid", "local_engine_runtime_changed",
		"local_engine_python_import_path_invalid", "local_engine_import_root_undeclared",
		"local_engine_python_entrypoint_invalid", "local_engine_python_entrypoint_missing",
		"local_engine_mapping_unverified", "local_engine_macho_unreadable",
		"local_engine_mapped_library_closure_unverified":
		return reason
	default:
		return "local_engine_installation_ineligible"
	}
}

// Only display metadata is read here. CLI validation owns runtime eligibility.
func installationBytes(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxInstallationBytes {
		return nil, errors.New("invalid installation file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxInstallationBytes {
		return nil, errors.New("invalid installation file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxInstallationBytes+1))
	if err != nil || len(raw) > maxInstallationBytes {
		return nil, errors.New("invalid installation file")
	}
	return raw, nil
}

func installationKey(path string) string {
	raw, err := installationBytes(path)
	if err != nil {
		return "installation|unreadable"
	}
	hash := sha256.Sum256(raw)
	return "installation|" + hex.EncodeToString(hash[:])
}

func installationEntries(paths []string) []registryEntry {
	entries := []registryEntry{}
	seen := map[string]bool{}
	for _, path := range paths {
		if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
			continue
		}
		path, err := canonical(path)
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		family := "local"
		var display struct {
			Family string `json:"family"`
		}
		if raw, err := installationBytes(path); err == nil && json.Unmarshal(raw, &display) == nil &&
			slices.Contains([]string{"llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"}, display.Family) {
			family = display.Family
		}
		entries = append(entries, registryEntry{path: path, installation: path, family: family, source: "本地安装声明"})
	}
	return entries
}

func installationPath(path string) (string, *ApiError) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", Err("invalid_request")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", Err("invalid_request")
	}
	resolved, err := canonical(absolute)
	if err != nil {
		return "", Err("local_engine_installation_unreadable")
	}
	return resolved, nil
}

func (s *Service) RegisterEngineInstallation(ctx context.Context, path string) (SettingsResponse, *ApiError) {
	path, failure := installationPath(path)
	if failure != nil {
		return SettingsResponse{}, failure
	}
	key := installationKey(path)
	if key == "installation|unreadable" {
		return SettingsResponse{}, Err("local_engine_installation_unreadable")
	}
	p := s.probe(s.userSettings())
	if p.Status != "ok" {
		return SettingsResponse{}, Err("cli_unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, installationCheckTimeout)
	defer cancel()
	result, failure := s.checkInstallationWithCLI(ctx, str(p.Path), path)
	if failure != nil {
		return SettingsResponse{}, failure
	}
	if !result.Eligible {
		return SettingsResponse{}, Err(str(result.Reason))
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if installationKey(path) != key {
		return SettingsResponse{}, Err("local_engine_installation_changed")
	}
	u := s.userSettings()
	if slices.Contains(u.EngineInstallations, path) {
		return s.Settings(), nil
	}
	u.EngineInstallations = append(u.EngineInstallations, path)
	return s.saveSettings(u)
}

// RemoveEngineInstallation removes only the reference, never runtime files.
func (s *Service) RemoveEngineInstallation(path string) (SettingsResponse, *ApiError) {
	path, failure := installationPath(path)
	if failure != nil {
		return SettingsResponse{}, failure
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	u.EngineInstallations = slices.DeleteFunc(u.EngineInstallations, func(p string) bool { return p == path })
	return s.saveSettings(u)
}

// InvalidateEngineChecks explicitly rechecks runtime changes that cannot be
// inferred from a declaration edit. A verdict never grants model support.
func (s *Service) InvalidateEngineChecks() {
	s.checkMu.Lock()
	s.checkGeneration++
	s.checks = map[string]engineCheckEntry{}
	s.checkInflight = map[string]bool{}
	s.checkSupported = true
	s.checkMu.Unlock()
	s.VerifyEngines()
}
