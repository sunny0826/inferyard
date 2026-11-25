package core

import (
	"context"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func sanitizeEngineUpdateOutName(prefix, version string) string {
	var b strings.Builder
	b.WriteString(prefix)
	if trimmed := strings.TrimSpace(version); trimmed != "" {
		b.WriteByte('-')
		for _, r := range trimmed {
			switch {
			case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-':
				b.WriteRune(r)
			default:
				b.WriteByte('-')
			}
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" || !ValidDownloadName(out) {
		return prefix
	}
	return out
}

func engineUpdateDisplayTitle(familyID, engineName string) string {
	switch familyID {
	case "llama-cpp":
		return "llama.cpp"
	case "polystrata":
		return "PolyStrata"
	case "mlx-lm":
		return "mlx-lm"
	case "vllm":
		return "vLLM"
	case "sglang":
		return "SGLang"
	default:
		if strings.TrimSpace(engineName) != "" {
			return engineName
		}
		return familyID
	}
}

func readPolystrataBackend(dir string) string {
	for _, sub := range polystrataBuildJSONSubpaths() {
		raw, err := os.ReadFile(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		var doc struct {
			Backend string `json:"backend"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			switch strings.ToLower(strings.TrimSpace(doc.Backend)) {
			case "cuda", "hip", "metal":
				return strings.ToLower(strings.TrimSpace(doc.Backend))
			}
		}
	}
	return "auto"
}

func (s *Service) isEngineActiveLocked(engineName string, entry registryEntry) bool {
	entryExe := filepath.Clean(strings.TrimSpace(resolveEngineExecutablePath(entry)))
	entryPath := filepath.Clean(strings.TrimSpace(entry.path))
	entryInst := filepath.Clean(strings.TrimSpace(entry.installation))
	entryVenv := filepath.Clean(strings.TrimSpace(venvRootFromEntry(entry)))
	matchesPath := func(raw string) bool {
		cand := filepath.Clean(strings.TrimSpace(raw))
		if cand == "" || cand == "." {
			return false
		}
		if (entryExe != "" && cand == entryExe) || (entryPath != "" && cand == entryPath) || (entryInst != "" && cand == entryInst) {
			return true
		}
		if entryVenv != "" && entryVenv != "." && filepath.IsAbs(cand) {
			if rel, err := filepath.Rel(entryVenv, cand); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	matchesEntry := func(e *engineEntry) bool {
		if e == nil || !isActiveServiceState(e.state.State) {
			return false
		}
		if strings.TrimSpace(e.state.EngineName) == engineName {
			return true
		}
		if matchesPath(e.state.Executable) {
			return true
		}
		if e.cmd != nil && matchesPath(e.cmd.Path) {
			return true
		}
		return false
	}
	if matchesEntry(s.engine) {
		return true
	}
	for _, svc := range s.services {
		if matchesEntry(svc) {
			return true
		}
	}
	if s.runsActiveLocked() {
		for _, r := range s.runs {
			if r != nil && !terminal(r.Task.State) {
				if r.Task.CandidatePath != nil {
					cand := filepath.Clean(strings.TrimSpace(*r.Task.CandidatePath))
					if (entryPath != "" && cand == entryPath) || (entryInst != "" && cand == entryInst) {
						return true
					}
				}
				if strings.TrimSpace(r.Task.EngineFamily) != "" && strings.EqualFold(r.Task.EngineFamily, entry.engine.Family) {
					return true
				}
			}
		}
	}
	return false
}

func (s *Service) hasActiveVenvUpgradeLocked() bool {
	for _, d := range s.downloads {
		if d != nil && d.Task.Kind == "engine_update" && d.Task.State == "downloading" {
			return true
		}
	}
	return false
}

func (s *Service) isEngineUpgradingLocked(engineName, exePath string) bool {
	cleanExe := filepath.Clean(strings.TrimSpace(exePath))
	for _, d := range s.downloads {
		if d == nil || d.Task.Kind != "engine_update" || d.Task.State != "downloading" {
			continue
		}
		if engineName != "" && d.Task.OutName == engineName {
			return true
		}
		if cleanExe != "" && d.Task.OutDir != "" {
			if rel, err := filepath.Rel(filepath.Clean(d.Task.OutDir), cleanExe); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

func engineUpdatingError() *ApiError {
	return &ApiError{
		Code:    "engine_updating",
		Message: l10n.TR("core.engine_update.err.engine_updating"),
	}
}

func (s *Service) UpdateEngine(engineName string) (DownloadTask, *ApiError) {
	name := strings.TrimSpace(engineName)
	if name == "" {
		return DownloadTask{}, Err("invalid_request")
	}
	updates := s.EngineUpdates()
	info, ok := updates[name]
	if !ok {
		return DownloadTask{}, Err("not_found")
	}
	if !info.HasUpdate || info.LatestInstalled {
		return DownloadTask{}, &ApiError{
			Code:    "engine_up_to_date",
			Message: l10n.TR("core.engine_update.err.up_to_date"),
		}
	}

	reg := s.registry()
	var entry registryEntry
	var hasEntry bool
	for _, e := range reg.engines {
		if e.engine.Name == name {
			entry = e
			hasEntry = true
			break
		}
	}

	switch info.Action {
	case EngineUpdateActionDownload:
		if strings.TrimSpace(info.DownloadURL) == "" {
			return DownloadTask{}, &ApiError{
				Code:    "engine_update_manual_required",
				Message: l10n.TR("core.engine_update.err.manual_required"),
			}
		}
		if existing, found := s.activeEngineDownloadForURL(info.DownloadURL); found {
			return existing, nil
		}
		prefix := "llama-cpp"
		if strings.EqualFold(info.Distribution, "PrismML") {
			prefix = "llama-cpp-prism"
		}
		outName := sanitizeEngineUpdateOutName(prefix, info.LatestVersion)
		displayName := fmt.Sprintf("%s · %s", engineUpdateDisplayTitle(info.Family, name), info.LatestVersion)
		return s.createEngineDownloadTask(info.DownloadURL, outName, displayName, info.AssetBytes)

	case EngineUpdateActionManaged:
		if strings.TrimSpace(info.DownloadURL) == "" {
			return DownloadTask{}, &ApiError{
				Code:    "engine_update_manual_required",
				Message: l10n.TR("core.engine_update.err.manual_required"),
			}
		}
		var dataDir, backend, family, model, ggufPath string
		var contextLen int
		if hasEntry && entry.installation != "" {
			declWorkDir, declDataDir := readPolystrataInstallDirs(entry.installation)
			instDir := strings.TrimSpace(declWorkDir)
			if instDir == "" {
				instDir = filepath.Dir(entry.installation)
			}
			dataDir = strings.TrimSpace(declDataDir)
			if dataDir == "" || !filepath.IsAbs(dataDir) {
				dataDir = existingPolystrataDataDir(instDir)
			}
			backend = readPolystrataBackend(instDir)
			family, model, ggufPath, contextLen = inferPolystrataUpdateSpec(instDir, dataDir)
		}
		outName := sanitizeEngineUpdateOutName("polystrata", info.LatestVersion)
		return s.CreatePolystrataSetup(PolystrataSetupRequest{
			Family:   family,
			Model:    model,
			Context:  contextLen,
			GGUFPath: ggufPath,
			Source:   info.DownloadURL,
			OutName:  outName,
			DataDir:  dataDir,
			Backend:  backend,
		})

	case EngineUpdateActionVenv:
		if !hasEntry {
			return DownloadTask{}, Err("not_found")
		}
		venvPython := venvPythonPath(entry)
		pkg := venvPackageInstallName(info.Family)
		if venvPython == "" || pkg == "" {
			return DownloadTask{}, &ApiError{
				Code:    "engine_update_manual_required",
				Message: l10n.TR("core.engine_update.err.manual_required"),
			}
		}
		return s.startVenvEngineUpgrade(name, entry, info, venvPython, pkg)

	default:
		return DownloadTask{}, &ApiError{
			Code:    "engine_update_manual_required",
			Message: l10n.TR("core.engine_update.err.manual_required"),
		}
	}
}

func (s *Service) activeEngineDownloadForURL(sourceURL string) (DownloadTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.downloads {
		if d.Task.Kind == "engine" && d.Task.State == "downloading" && d.Task.SourceUrl == sourceURL {
			return clone(d.Task), true
		}
	}
	return DownloadTask{}, false
}

func (s *Service) startVenvEngineUpgrade(engineName string, entry registryEntry, info EngineUpdateInfo, venvPython, pkg string) (DownloadTask, *ApiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if apiErr := s.admitDownloadLocked(); apiErr != nil {
		return DownloadTask{}, apiErr
	}
	if s.isEngineActiveLocked(engineName, entry) {
		return DownloadTask{}, &ApiError{
			Code:    "engine_in_use",
			Message: l10n.TR("core.engine_update.err.engine_in_use"),
		}
	}
	for _, existing := range s.downloads {
		if existing.Task.Kind == "engine_update" && existing.Task.State == "downloading" && existing.Task.OutName == engineName {
			return clone(existing.Task), nil
		}
	}

	displayName := fmt.Sprintf("%s · %s", engineUpdateDisplayTitle(info.Family, engineName), info.LatestVersion)
	if info.InstalledVersion != "" && info.LatestVersion != "" {
		displayName = fmt.Sprintf("%s · %s -> %s", engineUpdateDisplayTitle(info.Family, engineName), info.InstalledVersion, info.LatestVersion)
	}
	ctx, cancel := context.WithCancel(s.ctx)
	d := &downloadEntry{
		Task: DownloadTask{
			Kind:           "engine_update",
			DisplayName:    displayName,
			TaskId:         uuid(),
			State:          "downloading",
			Stage:          "upgrading",
			SourceUrl:      info.ReleaseURL,
			OutName:        engineName,
			OutDir:         venvRootFromEntry(entry),
			StartedAt:      now(),
			ProgressReason: ptr("engine_upgrade_in_progress"),
		},
		cancel: cancel,
	}
	s.downloads = append(s.downloads, d)
	s.Bus.Publish("download.state", map[string]any{"task": clone(d.Task)})
	go s.runVenvEngineUpgrade(ctx, d, entry, info, venvPython, pkg)
	return clone(d.Task), nil
}

func (s *Service) runVenvEngineUpgrade(ctx context.Context, d *downloadEntry, entry registryEntry, info EngineUpdateInfo, venvPython, pkg string) {
	defer d.cancel()
	targetSpec := pkg
	if ver := strings.TrimPrefix(strings.TrimSpace(info.LatestVersion), "v"); ver != "" {
		if _, ok := parseSemverVersion(ver); ok {
			targetSpec = pkg + "==" + ver
		}
	}

	out, execErr := s.executeVenvUpgrade(ctx, venvPython, targetSpec)

	s.mu.Lock()
	d.Task.FinishedAt = ptr(now())
	switch {
	case ctx.Err() != nil || out.ExitCode == inferyard.ExitCancelled:
		d.Task.State = "cancelled"
		d.Task.Stage = ""
	case execErr != nil || out.ExitCode != 0:
		d.Task.State = "failed"
		d.Task.Stage = ""
		msg := strings.TrimSpace(string(out.Stderr))
		if msg == "" {
			msg = strings.TrimSpace(string(out.Stdout))
		}
		if msg == "" && execErr != nil {
			msg = execErr.Error()
		}
		details := map[string]any{}
		if msg != "" {
			if len(msg) > 400 {
				msg = msg[len(msg)-400:]
			}
			details["output"] = msg
		}
		d.Task.Error = &ApiError{
			Code:    "engine_upgrade_failed",
			Message: l10n.TR("core.engine_update.err.upgrade_failed"),
			Details: details,
		}
	default:
		d.Task.State = "done"
		d.Task.Stage = ""
	}
	doneOK := d.Task.State == "done"
	taskSnap := clone(d.Task)
	s.mu.Unlock()

	if doneOK {
		if s.engineUpdater != nil {
			s.engineUpdater.clearBinaryVersion(resolveEngineExecutablePath(entry))
		}
		s.InvalidateEngineChecks()
		_ = s.RefreshAssets()
		s.publishEngineUpdatesEvent(false)
	}
	s.Bus.Publish("download.state", map[string]any{"task": taskSnap})
}

func (s *Service) executeVenvUpgrade(ctx context.Context, venvPython, targetSpec string) (inferyard.Execution, error) {
	toolsDir := ""
	if strings.TrimSpace(s.settingsPath) != "" {
		toolsDir = filepath.Join(filepath.Dir(s.settingsPath), "tools")
	}
	uvPath := FindUV(toolsDir)
	var cmdName string
	var args []string
	if uvPath != "" {
		cmdName = uvPath
		args = []string{"pip", "install", "--python", venvPython, "--upgrade", targetSpec}
	} else {
		cmdName = venvPython
		args = []string{"-m", "pip", "install", "--upgrade", targetSpec}
	}
	if s.engineUpdater != nil && s.engineUpdater.opts.ExecUpgrade != nil {
		return s.engineUpdater.opts.ExecUpgrade(ctx, cmdName, args...)
	}
	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Env = os.Environ()
	if proxy := strings.TrimSpace(s.downloadProxy()); proxy != "" {
		cmd.Env = append(cmd.Env,
			"HTTPS_PROXY="+proxy,
			"HTTP_PROXY="+proxy,
			"https_proxy="+proxy,
			"http_proxy="+proxy,
		)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			err = nil
		} else {
			exitCode = 1
		}
	}
	return inferyard.Execution{
		Stdout:   []byte(stdout.String()),
		Stderr:   []byte(stderr.String()),
		ExitCode: exitCode,
	}, err
}
