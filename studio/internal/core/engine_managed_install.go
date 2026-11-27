package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	managedEngineDefinition      = "inferyard-managed-engine.v1"
	managedEngineReceiptName     = ".inferyard-managed-engine.json"
	managedEngineVenvInstallMode = "managed_venv"
)

type ManagedEngineInstallRequest struct {
	Family    string `json:"family"`
	ReleaseID string `json:"release_id"`
	BuildID   string `json:"build_id"`
}

type managedEngineReceipt struct {
	Definition  string `json:"definition"`
	Family      string `json:"family"`
	Version     string `json:"version"`
	ReleaseID   string `json:"release_id"`
	BuildID     string `json:"build_id"`
	InstallMode string `json:"install_mode"`
	SourceURL   string `json:"source_url"`
	InstalledAt string `json:"installed_at"`
}

var (
	managedEngineFindUV        = findCompatibleUV
	managedEngineDownloadUV    = downloadUV
	managedEngineCommandOutput = installCommandOutput
)

func managedEngineBuild(req ManagedEngineInstallRequest) (EngineFamilyCatalog, EngineRelease, EnginePlatformBuild, *ApiError) {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	family, ok := DefaultEngineCatalog().FamilyByID(strings.TrimSpace(req.Family))
	if !ok {
		return EngineFamilyCatalog{}, EngineRelease{}, EnginePlatformBuild{}, Err("invalid_request")
	}
	for _, release := range family.ReleasesForPlatform(platform, false) {
		if release.ID != strings.TrimSpace(req.ReleaseID) {
			continue
		}
		for _, build := range release.Builds {
			if build.ID == strings.TrimSpace(req.BuildID) && build.CanManagedVenvInstallOn(platform) {
				return family, release, build, nil
			}
		}
	}
	return EngineFamilyCatalog{}, EngineRelease{}, EnginePlatformBuild{}, Err("invalid_request")
}

func managedEngineOutputName(family EngineFamilyCatalog, release EngineRelease) string {
	raw := family.ID + "-" + strings.TrimPrefix(release.Version, "v")
	var b strings.Builder
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), ".-")
	if !ValidDownloadName(name) {
		return family.ID
	}
	return name
}

func managedEngineRequestFromTask(task DownloadTask) (ManagedEngineInstallRequest, bool) {
	parts := strings.Split(task.RequestedSourceUrl, ":")
	if task.Kind != "engine" || len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return ManagedEngineInstallRequest{}, false
	}
	return ManagedEngineInstallRequest{Family: parts[0], ReleaseID: parts[1], BuildID: parts[2]}, true
}

func managedEngineSourceURL(release EngineRelease, build EnginePlatformBuild) string {
	if build.ManagedSpec != nil && len(build.ManagedSpec.WheelURLs) > 0 {
		return build.ManagedSpec.WheelURLs[0]
	}
	if primary := build.PrimaryURL(); primary != "" {
		return primary
	}
	return release.ReleaseURL
}

func (s *Service) CreateManagedEngineInstall(ctx context.Context, req ManagedEngineInstallRequest) (DownloadTask, *ApiError) {
	family, release, build, failure := managedEngineBuild(req)
	if failure != nil {
		return DownloadTask{}, failure
	}
	u := s.userSettings()
	root, failure := s.downloadRoot("engine", u)
	if failure != nil {
		return DownloadTask{}, failure
	}
	probe := s.probe(u)
	if probe.Status != "ok" {
		return DownloadTask{}, cliProbeError(probe)
	}
	sourceURL := managedEngineSourceURL(release, build)
	identity := family.ID + ":" + release.ID + ":" + build.ID
	s.mu.Lock()
	defer s.mu.Unlock()
	if failure := s.admitDownloadLocked(); failure != nil {
		return DownloadTask{}, failure
	}
	if existing, found := s.activeDownloadLocked("engine", root, sourceURL, identity); found {
		return existing, nil
	}
	outName, outDir, failure := s.downloadOutputLocked(root, "", managedEngineOutputName(family, release))
	if failure != nil {
		return DownloadTask{}, failure
	}
	if ctx == nil {
		ctx = context.Background()
	}
	taskCtx, cancel := context.WithCancel(ctx)
	d := &downloadEntry{Task: DownloadTask{
		Kind: "engine", DisplayName: family.Name + " · " + release.Version,
		TaskId: uuid(), State: "downloading", Stage: "preparing_uv",
		SourceUrl: sourceURL, RequestedSourceUrl: identity, OutName: outName, OutDir: outDir,
		Step: ptr(1), StepTotal: ptr(4), Percent: ptr(5.0), StartedAt: now(),
	}, cancel: cancel}
	s.downloads = append(s.downloads, d)
	s.Bus.Publish("download.state", map[string]any{"task": clone(d.Task)})
	go s.runManagedEngineInstall(taskCtx, *probe.Path, d, family, release, build)
	return clone(d.Task), nil
}

func (s *Service) setManagedEngineStage(d *downloadEntry, stage string, step int, percent float64) {
	s.mu.Lock()
	d.Task.Stage, d.Task.Step, d.Task.Percent = stage, ptr(step), ptr(percent)
	s.Bus.Publish("download.state", map[string]any{"task": clone(d.Task)})
	s.mu.Unlock()
}

func managedEnginePython(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python3")
}

func runManagedEngineCommand(ctx context.Context, path string, args ...string) *ApiError {
	out, err := managedEngineCommandOutput(ctx, path, args...)
	if err == nil {
		return nil
	}
	tail := strings.TrimSpace(string(out))
	if len(tail) > 600 {
		tail = tail[len(tail)-600:]
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return Err("cancelled")
	}
	apiErr := Err("engine_install_failed")
	apiErr.Message = l10n.TR("core.managed_engine.err.command_failed")
	apiErr.Details = map[string]any{"output_tail": tail}
	return apiErr
}

func (s *Service) initManagedEngineInstallation(ctx context.Context, cliPath, family, installDir, venvDir string) (string, *ApiError) {
	installationPath := filepath.Join(installDir, "installation.json")
	args := []string{
		"engine", "init-installation", "--family", family,
		"--executable", managedEnginePython(venvDir), "--working-directory", installDir,
		"--runtime-root", venvDir, "--out", installationPath,
	}
	initCtx, cancel := context.WithTimeout(ctx, installationCheckTimeout)
	defer cancel()
	out, err := s.exec(initCtx, cliPath, args...)
	if err != nil {
		if initCtx.Err() != nil {
			return "", Err("cancelled")
		}
		return "", Err("internal_error")
	}
	if out.ExitCode != inferyard.ExitComplete {
		if out.ExitCode == inferyard.ExitCancelled {
			return "", Err("cancelled")
		}
		if apiErr := exitError(out, nil); apiErr != nil {
			return "", apiErr
		}
		return "", Err("cli_error")
	}
	parsed, err := inferyard.ParseInitInstallation(out.Stdout)
	if err != nil || parsed.Out != installationPath {
		return "", Err("engine_init_invalid_response")
	}
	if _, failure := s.RegisterEngineInstallation(ctx, installationPath); failure != nil {
		return "", failure
	}
	return installationPath, nil
}

func (s *Service) runManagedEngineInstall(ctx context.Context, cliPath string, d *downloadEntry, family EngineFamilyCatalog, release EngineRelease, build EnginePlatformBuild) {
	defer d.cancel()
	ctx = withInstallProxy(ctx, s.downloadProxy())
	toolsDir := filepath.Join(filepath.Dir(s.settingsPath), "tools")
	uvPath, _ := managedEngineFindUV(ctx, toolsDir)
	if uvPath == "" {
		var failure *ApiError
		uvPath, failure = managedEngineDownloadUV(ctx, toolsDir)
		if failure != nil {
			s.finishManagedEngineInstall(ctx, d, "", failure)
			return
		}
	}
	if err := os.MkdirAll(d.Task.OutDir, 0o700); err != nil {
		s.finishManagedEngineInstall(ctx, d, "", Err("internal_error"))
		return
	}
	s.setManagedEngineStage(d, "creating_venv", 2, 25)
	venvDir := filepath.Join(d.Task.OutDir, "venv")
	python := build.ManagedSpec.Python
	if strings.TrimSpace(python) == "" {
		python = "3.12"
	}
	if failure := runManagedEngineCommand(ctx, uvPath, "venv", "--python", python, venvDir); failure != nil {
		s.finishManagedEngineInstall(ctx, d, "", failure)
		return
	}
	s.setManagedEngineStage(d, "installing_packages", 3, 50)
	args := []string{"pip", "install", "--python", managedEnginePython(venvDir)}
	args = append(args, build.ManagedSpec.WheelURLs...)
	args = append(args, build.ManagedSpec.Packages...)
	installCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	failure := runManagedEngineCommand(installCtx, uvPath, args...)
	cancel()
	if failure != nil {
		s.finishManagedEngineInstall(ctx, d, "", failure)
		return
	}
	sourceURL := managedEngineSourceURL(release, build)
	receipt := managedEngineReceipt{
		Definition: managedEngineDefinition, Family: family.ID, Version: release.Version,
		ReleaseID: release.ID, BuildID: build.ID, InstallMode: managedEngineVenvInstallMode,
		SourceURL: sourceURL, InstalledAt: now(),
	}
	if failure := writeManagedEngineReceipt(d.Task.OutDir, receipt); failure != nil {
		s.finishManagedEngineInstall(ctx, d, "", failure)
		return
	}
	s.setManagedEngineStage(d, "registering_engine", 4, 85)
	installationPath, failure := s.initManagedEngineInstallation(ctx, cliPath, family.ID, d.Task.OutDir, venvDir)
	s.finishManagedEngineInstall(ctx, d, installationPath, failure)
	if failure == nil {
		s.VerifyEngines()
		_ = s.RefreshAssets()
		s.publishEngineUpdatesEvent(false)
	}
}

func (s *Service) finishManagedEngineInstall(ctx context.Context, d *downloadEntry, installationPath string, failure *ApiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d.Task.FinishedAt = ptr(now())
	d.Task.Stage = ""
	switch {
	case ctx.Err() != nil || failure != nil && failure.Code == "cancelled":
		d.Task.State = "cancelled"
	case failure != nil:
		d.Task.State, d.Task.Error = "failed", failure
	default:
		d.Task.State, d.Task.Percent = "done", ptr(100.0)
		d.Task.InstallationPath = ptr(installationPath)
		d.Task.EnginePath = ptr(installationPath)
	}
	s.Bus.Publish("download.state", map[string]any{"task": clone(d.Task)})
}

func writeManagedEngineReceipt(installDir string, receipt managedEngineReceipt) *ApiError {
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return Err("internal_error")
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		return Err("internal_error")
	}
	target := filepath.Join(installDir, managedEngineReceiptName)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return Err("internal_error")
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return Err("internal_error")
	}
	return nil
}

func readManagedEngineReceipt(installDir string) (managedEngineReceipt, bool) {
	path := filepath.Join(installDir, managedEngineReceiptName)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxInstallationBytes {
		return managedEngineReceipt{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return managedEngineReceipt{}, false
	}
	var receipt managedEngineReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Definition != managedEngineDefinition ||
		strings.TrimSpace(receipt.Family) == "" || strings.TrimSpace(receipt.InstallMode) == "" {
		return managedEngineReceipt{}, false
	}
	return receipt, true
}

func discoverManagedEngineInstallations(root string) []string {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		path := filepath.Join(dir, "installation.json")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		receipt, managed := readManagedEngineReceipt(dir)
		if managed && receipt.InstallMode == managedEngineVenvInstallMode || hasPolystrataRuntimeReceipt(dir) {
			out = append(out, path)
		}
	}
	return out
}

func managedVenvInstallDir(rawInstPath, canonInstPath, wsEnginesRoot string, resultsRoot *string) (string, managedEngineReceipt, bool) {
	empty := managedEngineReceipt{}
	if wsEnginesRoot == "" || filepath.Base(canonInstPath) != "installation.json" ||
		isSymlinkPath(rawInstPath) || isSymlinkPath(canonInstPath) || isSymlinkPath(filepath.Dir(rawInstPath)) {
		return "", empty, false
	}
	root, err := canonical(wsEnginesRoot)
	if err != nil {
		return "", empty, false
	}
	installDir := filepath.Dir(canonInstPath)
	if sameFileOrPath(installDir, root) || !sameFileOrPath(filepath.Dir(installDir), root) {
		return "", empty, false
	}
	info, err := os.Lstat(installDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		overlapsResultsRoot(installDir, resultsRoot) {
		return "", empty, false
	}
	receipt, ok := readManagedEngineReceipt(installDir)
	if !ok || receipt.InstallMode != managedEngineVenvInstallMode || receipt.Version == "" {
		return "", empty, false
	}
	raw, err := installationBytes(canonInstPath)
	if err != nil {
		return "", empty, false
	}
	var declaration struct {
		Family string `json:"family"`
	}
	if json.Unmarshal(raw, &declaration) != nil || declaration.Family != receipt.Family {
		return "", empty, false
	}
	return installDir, receipt, true
}

func verifyManagedVenvTreeSafe(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && isSealedEvidenceMarker(entry.Name()) {
			return errUnsafeAssetTree
		}
		if entry.Type()&os.ModeSymlink != 0 && entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

func (s *Service) inspectManagedVenvEngineDelete(u UserSettings, paths *BoundPaths, eng EngineAsset) (AssetDeleteInfo, *ApiError) {
	rawPath := s.findAssetRawPath("engine", eng.Name, eng.Path)
	installDir, _, ok := managedVenvInstallDir(rawPath, eng.Path, workspaceEnginesRoot(u, paths), derivedResultsRoot(u, paths))
	if !ok || verifyManagedVenvTreeSafe(installDir) != nil {
		return AssetDeleteInfo{}, errAssetNotDeletable()
	}
	if otherEngineSharesInstallDir(installDir, eng.Path, s.registry().engines) {
		return AssetDeleteInfo{}, errAssetNotDeletable()
	}
	return AssetDeleteInfo{
		Kind: "engine", Name: eng.Name, TargetPath: installDir,
		Fingerprint:      standardAssetDeleteFingerprint("managed_venv_engine", installDir),
		ReclaimableBytes: ptr(reclaimableDiskBytes([]string{installDir}, nil)), Scope: "managed_venv_engine",
	}, nil
}

var removeStagedManagedEngineDir = os.RemoveAll

func (s *Service) deleteManagedVenvEngine(eng EngineAsset, expectedTarget, expectedFP string) *ApiError {
	failure := s.runDeleteManagedVenvEngine(eng, expectedTarget, expectedFP)
	if failure == nil {
		s.VerifyEngines()
		_ = s.RefreshAssets()
		s.publishEngineUpdatesEvent(false)
	}
	return failure
}

func (s *Service) runDeleteManagedVenvEngine(eng EngineAsset, expectedTarget, expectedFP string) *ApiError {
	rawPath := s.findAssetRawPath("engine", eng.Name, eng.Path)
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.mu.Lock()
	if s.assetWorkspaceBusyLocked() {
		s.mu.Unlock()
		return workspaceBusyError()
	}
	s.deletingAssets++
	u, paths := clone(s.settings), s.paths
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.deletingAssets--
		s.mu.Unlock()
	}()

	wsEngines := workspaceEnginesRoot(u, paths)
	installDir, _, ok := managedVenvInstallDir(rawPath, eng.Path, wsEngines, derivedResultsRoot(u, paths))
	if !ok || otherEngineSharesInstallDir(installDir, eng.Path, s.registry().engines) || verifyManagedVenvTreeSafe(installDir) != nil {
		return errAssetNotDeletable()
	}
	if expectedTarget != "" && (!sameFileOrPath(installDir, expectedTarget) ||
		expectedFP != "" && standardAssetDeleteFingerprint("managed_venv_engine", installDir) != expectedFP) {
		return deleteTargetChangedError()
	}
	removeRaw := map[string]bool{}
	for _, rawInst := range u.EngineInstallations {
		if canon, err := canonical(rawInst); err == nil && pathWithinDirSameFS(installDir, canon) {
			removeRaw[rawInst] = true
		}
	}
	stagedDir := filepath.Join(filepath.Dir(installDir), ".inferyard-uninstall-"+filepath.Base(installDir)+"-"+uuid())
	if err := os.Rename(installDir, stagedDir); err != nil {
		return Err("internal_error")
	}
	nextU := clone(u)
	nextU.EngineInstallations = slices.DeleteFunc(slices.Clone(nextU.EngineInstallations), func(raw string) bool { return removeRaw[raw] })
	if len(removeRaw) > 0 {
		if _, saveErr := s.saveSettings(nextU); saveErr != nil {
			_ = os.Rename(stagedDir, installDir)
			return saveErr
		}
	}
	removeErr := removeStagedManagedEngineDir(stagedDir)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		_ = os.Rename(stagedDir, installDir)
		if len(removeRaw) > 0 {
			_, _ = s.saveSettings(u)
		}
		return Err("internal_error")
	}
	s.mu.Lock()
	s.pruneAssetSnapshotLocked("engine", installDir)
	s.mu.Unlock()
	s.checkMu.Lock()
	for path := range s.checks {
		if pathWithinDirSameFS(installDir, path) {
			delete(s.checks, path)
		}
	}
	for path := range s.checkInflight {
		if pathWithinDirSameFS(installDir, path) {
			delete(s.checkInflight, path)
		}
	}
	s.checkMu.Unlock()
	return nil
}

func managedBinaryReceipt(sourceURL string) managedEngineReceipt {
	receipt := managedEngineReceipt{
		Definition: managedEngineDefinition, Family: "llama-cpp", InstallMode: "binary",
		SourceURL: sourceURL, InstalledAt: now(),
	}
	for _, family := range DefaultEngineCatalog().Families() {
		for _, release := range family.Releases {
			for _, build := range release.Builds {
				if build.AcquireMode == EngineAcquireDirect && build.PrimaryURL() == sourceURL {
					receipt.Family, receipt.Version = family.ID, release.Version
					receipt.ReleaseID, receipt.BuildID = release.ID, build.ID
					return receipt
				}
			}
		}
	}
	return receipt
}
