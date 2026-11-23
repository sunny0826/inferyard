package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const installationCheckTimeout = 5 * time.Minute
const maxInstallationBytes = 1 << 20

func localInstallationReason(reason string) string {
	switch reason {
	case "local_engine_requires_darwin_arm64", "local_engine_platform_unsupported",
		"local_engine_json_unreadable",
		"local_engine_absolute_path_required", "local_engine_locator_missing",
		"local_engine_runtime_root_invalid", "local_engine_cwd_invalid",
		"local_engine_executable_invalid", "local_engine_models_root_required_for_lmstudio_only",
		"local_engine_data_dir_polystrata_only", "local_engine_data_dir_invalid",
		"local_engine_polystrata_server_missing", "local_engine_polystrata_layout_invalid",
		"local_engine_polystrata_engine_missing",
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
	for _, raw := range paths {
		if !filepath.IsAbs(raw) || strings.ContainsRune(raw, 0) {
			continue
		}
		path, err := canonical(raw)
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		family := "local"
		displayName := ""
		var display struct {
			Family           string `json:"family"`
			WorkingDirectory string `json:"working_directory"`
		}
		if rawBytes, err := installationBytes(path); err == nil && json.Unmarshal(rawBytes, &display) == nil &&
			slices.Contains([]string{"llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio", "polystrata"}, display.Family) {
			family = display.Family
			if family == "polystrata" {
				displayName = polystrataInstallationDisplayName(path, display.WorkingDirectory)
			}
		}
		entries = append(entries, registryEntry{
			path:         path,
			rawPath:      raw,
			installation: path,
			family:       family,
			displayName:  displayName,
			source:       "本地安装声明",
		})
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
		return SettingsResponse{}, cliProbeError(p)
	}
	cliKey, checkGen := s.captureCheckGeneration(p)
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
	var resp SettingsResponse
	var saveErr *ApiError
	switch {
	case ctx.Err() != nil:
		saveErr = Err("cancelled")
	case installationKey(path) != key:
		saveErr = Err("local_engine_installation_changed")
	default:
		u := s.userSettings()
		if slices.Contains(u.EngineInstallations, path) {
			resp = s.Settings()
		} else {
			u.EngineInstallations = append(u.EngineInstallations, path)
			resp, saveErr = s.saveSettings(u)
		}
	}
	s.settingsMu.Unlock()
	if saveErr != nil {
		return SettingsResponse{}, saveErr
	}
	s.cacheInstallationCheck(cliKey, checkGen, path, key, result)
	return resp, nil
}

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

func (s *Service) InvalidateEngineChecks() {
	s.checkMu.Lock()
	s.checkGeneration++
	s.checks = map[string]engineCheckEntry{}
	s.checkInflight = map[string]bool{}
	s.checkSupported = true
	s.checkMu.Unlock()
	s.VerifyEngines()
}

const engineDetectTimeout = 30 * time.Second

type InitLocalEngineRequest struct {
	Family           string   `json:"family"`
	Executable       string   `json:"executable"`
	WorkingDirectory string   `json:"working_directory,omitempty"`
	RuntimeRoots     []string `json:"runtime_roots,omitempty"`
	ModelsRoot       string   `json:"models_root,omitempty"`
	DataDir          string   `json:"data_dir,omitempty"`
}

func (s *Service) DetectLocalEngines(ctx context.Context) (inferyard.EngineDetectResult, *ApiError) {
	if ctx == nil {
		ctx = context.Background()
	}
	p := s.probe(s.userSettings())
	if p.Status != "ok" {
		return inferyard.EngineDetectResult{}, cliProbeError(p)
	}
	detectCtx, cancel := context.WithTimeout(ctx, engineDetectTimeout)
	defer cancel()
	out, err := s.exec(detectCtx, *p.Path, "engine", "detect")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) {
			return inferyard.EngineDetectResult{}, Err("cli_outdated")
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return inferyard.EngineDetectResult{}, Err("cancelled")
		}
		return inferyard.EngineDetectResult{}, &ApiError{
			Code:    "engine_detect_error",
			Message: "本地引擎探测失败，请检查环境或 CLI 状态后重试。",
		}
	}
	if out.ExitCode == inferyard.ExitBlocked {
		var header struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(out.Stdout, &header) != nil || header.Command != "engine detect" {
			return inferyard.EngineDetectResult{}, Err("cli_outdated")
		}
		return inferyard.EngineDetectResult{}, exitError(out, nil)
	}
	if out.ExitCode != 0 {
		return inferyard.EngineDetectResult{}, exitError(out, nil)
	}
	result, err := inferyard.ParseEngineDetect(out.Stdout)
	if err != nil {
		return inferyard.EngineDetectResult{}, &ApiError{
			Code:    "engine_detect_invalid_response",
			Message: "CLI 本地引擎探测结果格式无效，请升级 CLI 后重试。",
		}
	}
	return result, nil
}

func engineSlugFromExecutable(executable string) string {
	parent := filepath.Base(filepath.Dir(executable))
	candidate := filepath.Base(executable)
	if parent == "bin" {
		grand := filepath.Base(filepath.Dir(filepath.Dir(executable)))
		if grand != "" && grand != "." && grand != string(filepath.Separator) {
			candidate = grand
		}
	}
	var b strings.Builder
	for _, r := range candidate {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-.")
	if slug == "" || !ValidDownloadName(slug) {
		return "engine"
	}
	return slug
}

func nextInstallationFilePath(workspace, family, executable string) (string, *ApiError) {
	if workspace == "" {
		return "", Conflict("settings_incomplete")
	}
	dir, err := Resolve(workspace, "engine-installations")
	if err != nil {
		return "", Err("invalid_request")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", Err("internal_error")
	}
	slug := engineSlugFromExecutable(executable)
	for i := 1; i <= 1000; i++ {
		name := family + "-" + slug + ".json"
		if i > 1 {
			name = family + "-" + slug + "-" + strconv.Itoa(i) + ".json"
		}
		path, err := Resolve(workspace, filepath.Join("engine-installations", name))
		if err != nil {
			return "", Err("invalid_request")
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", Err("internal_error")
		}
	}
	return "", Err("output_name_exhausted")
}

func (s *Service) InitAndRegisterLocalEngine(ctx context.Context, req InitLocalEngineRequest) (SettingsResponse, *ApiError) {
	if ctx == nil {
		ctx = context.Background()
	}
	family := strings.TrimSpace(req.Family)
	if !slices.Contains([]string{"llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio", "polystrata"}, family) {
		return SettingsResponse{}, Err("invalid_request")
	}
	executable := strings.TrimSpace(req.Executable)
	if executable == "" || !filepath.IsAbs(executable) || strings.ContainsRune(executable, 0) {
		return SettingsResponse{}, Err("invalid_request")
	}
	workingDir := strings.TrimSpace(req.WorkingDirectory)
	if workingDir != "" && (!filepath.IsAbs(workingDir) || strings.ContainsRune(workingDir, 0)) {
		return SettingsResponse{}, Err("invalid_request")
	}
	modelsRoot := strings.TrimSpace(req.ModelsRoot)
	if family == "lmstudio" {
		if modelsRoot == "" || !filepath.IsAbs(modelsRoot) || strings.ContainsRune(modelsRoot, 0) {
			return SettingsResponse{}, &ApiError{
				Code:    "local_engine_models_root_required_for_lmstudio_only",
				Message: "LM Studio 需要填写模型目录的绝对路径。",
			}
		}
	} else if modelsRoot != "" {
		return SettingsResponse{}, &ApiError{
			Code:    "local_engine_models_root_required_for_lmstudio_only",
			Message: "仅 LM Studio 安装声明需要指定模型目录。",
		}
	}
	dataDir := strings.TrimSpace(req.DataDir)
	if dataDir != "" {
		if family != "polystrata" {
			return SettingsResponse{}, &ApiError{
				Code:    "local_engine_data_dir_polystrata_only",
				Message: "仅 PolyStrata 安装声明支持指定数据目录。",
			}
		}
		if !filepath.IsAbs(dataDir) || strings.ContainsRune(dataDir, 0) {
			return SettingsResponse{}, Err("invalid_request")
		}
	}
	var runtimeRoots []string
	for _, root := range req.RuntimeRoots {
		trimmed := strings.TrimSpace(root)
		if trimmed == "" || !filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, 0) {
			return SettingsResponse{}, Err("invalid_request")
		}
		runtimeRoots = append(runtimeRoots, trimmed)
	}

	u := s.userSettings()
	workspace := workspaceRoot(u)
	if workspace == nil || strings.TrimSpace(*workspace) == "" {
		return SettingsResponse{}, Conflict("settings_incomplete")
	}
	outPath, failure := nextInstallationFilePath(*workspace, family, executable)
	if failure != nil {
		return SettingsResponse{}, failure
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return SettingsResponse{}, cliProbeError(p)
	}
	args := []string{
		"engine", "init-installation",
		"--family", family,
		"--executable", executable,
		"--out", outPath,
	}
	if workingDir != "" {
		args = append(args, "--working-directory", workingDir)
	}
	for _, root := range runtimeRoots {
		args = append(args, "--runtime-root", root)
	}
	if modelsRoot != "" {
		args = append(args, "--models-root", modelsRoot)
	}
	if dataDir != "" {
		args = append(args, "--data-dir", dataDir)
	}
	initCtx, cancel := context.WithTimeout(ctx, installationCheckTimeout)
	defer cancel()
	out, err := s.exec(initCtx, *p.Path, args...)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) {
			return SettingsResponse{}, Err("cli_outdated")
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return SettingsResponse{}, Err("cancelled")
		}
		return SettingsResponse{}, Err("internal_error")
	}
	if out.ExitCode == inferyard.ExitBlocked {
		var header struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(out.Stdout, &header) != nil || header.Command != "engine init-installation" {
			return SettingsResponse{}, Err("cli_outdated")
		}
		return SettingsResponse{}, exitError(out, nil)
	}
	if out.ExitCode != 0 {
		return SettingsResponse{}, exitError(out, nil)
	}
	parsed, err := inferyard.ParseInitInstallation(out.Stdout)
	if err != nil {
		return SettingsResponse{}, &ApiError{
			Code:    "engine_init_invalid_response",
			Message: "CLI 安装声明生成结果格式无效，请升级 CLI 后重试。",
		}
	}
	return s.RegisterEngineInstallation(ctx, parsed.Out)
}
