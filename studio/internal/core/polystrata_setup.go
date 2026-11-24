package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	polystrataPinnedCommit        = "3e1642161198c50ff1abed4046f4e4f476099dbb"
	polystrataDefaultSourceURL    = "https://github.com/simplexai-labs/PolyStrata/archive/" + polystrataPinnedCommit + ".tar.gz"
	polystrataDefaultSourceBytes  = int64(25747544)
	polystrataDefaultSourceSHA256 = "aeced362c54ae3534a66646f1befffe4dc62f0f20cc4869c74ccfa2d90384a92"
)

type PolystrataSetupRequest struct {
	Family      string `json:"family,omitempty"`
	Model       string `json:"model,omitempty"`
	Context     int    `json:"context,omitempty"`
	Backend     string `json:"backend,omitempty"`
	ModelSource string `json:"model_source,omitempty"`
	GGUFPath    string `json:"gguf_path,omitempty"`
	Source      string `json:"source,omitempty"`
	OutName     string `json:"out_name,omitempty"`
	DataDir     string `json:"data_dir,omitempty"`
}

func validatePolystrataSetupRequest(req PolystrataSetupRequest) *ApiError {
	if req.OutName != "" && !ValidDownloadName(req.OutName) {
		return Err("invalid_request")
	}
	if req.Context != 0 && (req.Context < 1 || req.Context > 524288) {
		return Err("invalid_request")
	}
	if req.Backend != "" && !slices.Contains([]string{"auto", "cuda", "hip", "metal"}, req.Backend) {
		return Err("invalid_request")
	}
	if req.ModelSource != "" && !slices.Contains([]string{"auto", "huggingface", "modelscope"}, req.ModelSource) {
		return Err("invalid_request")
	}
	for _, value := range []string{req.Family, req.Model, req.Source} {
		if strings.ContainsAny(value, "\x00\r\n") {
			return Err("invalid_request")
		}
	}
	if req.GGUFPath != "" {
		if !filepath.IsAbs(req.GGUFPath) || strings.ContainsAny(req.GGUFPath, "\x00\r\n") {
			return Err("invalid_request")
		}
	}
	if req.DataDir != "" {
		if !filepath.IsAbs(req.DataDir) || strings.ContainsAny(req.DataDir, "\x00\r\n") {
			return Err("invalid_request")
		}
	}
	if req.Source != "" && strings.Contains(req.Source, "://") && !validEngineSource(req.Source) {
		return Err("invalid_request")
	}
	return nil
}

func polystrataDisplayName(req PolystrataSetupRequest) string {
	parts := []string{"PolyStrata"}
	if strings.TrimSpace(req.Family) != "" {
		parts = append(parts, strings.TrimSpace(req.Family))
	}
	if strings.TrimSpace(req.Model) != "" {
		parts = append(parts, strings.TrimSpace(req.Model))
	}
	return strings.Join(parts, " · ")
}

func samePolystrataRequest(a, b *PolystrataSetupRequest) bool {
	if a == nil || b == nil {
		return a == b
	}
	return strings.TrimSpace(a.Family) == strings.TrimSpace(b.Family) &&
		strings.TrimSpace(a.Model) == strings.TrimSpace(b.Model) &&
		a.Context == b.Context &&
		strings.TrimSpace(a.Backend) == strings.TrimSpace(b.Backend) &&
		strings.TrimSpace(a.ModelSource) == strings.TrimSpace(b.ModelSource) &&
		strings.TrimSpace(a.GGUFPath) == strings.TrimSpace(b.GGUFPath) &&
		strings.TrimSpace(a.Source) == strings.TrimSpace(b.Source) &&
		strings.TrimSpace(a.OutName) == strings.TrimSpace(b.OutName) &&
		strings.TrimSpace(a.DataDir) == strings.TrimSpace(b.DataDir)
}

func (s *Service) activePolystrataSetupLocked() (*downloadEntry, bool) {
	for _, d := range s.downloads {
		if d.Task.Kind == "polystrata" && d.Task.State == "downloading" {
			return d, true
		}
	}
	return nil, false
}

func (s *Service) CreatePolystrataSetup(req PolystrataSetupRequest) (DownloadTask, *ApiError) {
	if failure := validatePolystrataSetupRequest(req); failure != nil {
		return DownloadTask{}, failure
	}
	u := downloadNameDefaults(s.userSettings())
	root, apiErr := s.downloadRoot("engine", u)
	if apiErr != nil {
		return DownloadTask{}, apiErr
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return DownloadTask{}, cliProbeError(p)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if apiErr := s.admitDownloadLocked(); apiErr != nil {
		return DownloadTask{}, apiErr
	}
	if active, found := s.activePolystrataSetupLocked(); found {
		if samePolystrataRequest(active.polystrata, &req) {
			return clone(active.Task), nil
		}
		return DownloadTask{}, Conflict("polystrata_setup_active")
	}
	var outName, outDir string
	var resume bool
	sourceURL := strings.TrimSpace(req.Source)
	if sourceURL == "" {
		sourceURL = polystrataDefaultSourceURL
	}
	if strings.TrimSpace(req.OutName) == "" {
		if existingName, existingDir, ok := existingManagedPolystrataDir(root, u, sourceURL); ok {
			outName, outDir, resume = existingName, existingDir, true
		}
	}
	if outDir == "" {
		var failure *ApiError
		outName, outDir, failure = s.downloadOutputLocked(root, req.OutName, "polystrata")
		if failure != nil {
			return DownloadTask{}, failure
		}
	}
	dataDir := strings.TrimSpace(req.DataDir)
	if dataDir == "" && resume {
		if saved := existingPolystrataDataDir(outDir); saved != "" {
			dataDir = saved
		}
	}
	if dataDir == "" {
		if u.ModelsRoot != nil && strings.TrimSpace(*u.ModelsRoot) != "" {
			dataDir = filepath.Join(*u.ModelsRoot, "polystrata-data")
		} else {
			dataDir = filepath.Join(filepath.Dir(outDir), "PolyStrata-data")
		}
	}
	reqCopy := req
	ctx, cancel := context.WithCancel(s.ctx)
	d := &downloadEntry{
		Task: DownloadTask{
			Kind:        "polystrata",
			DisplayName: polystrataDisplayName(req),
			TaskId:      uuid(),
			State:       "downloading",
			Stage:       "preflight",
			ModelSource: strings.TrimSpace(req.ModelSource),
			SourceUrl:   sourceURL,
			OutName:     outName,
			OutDir:      outDir,
			LogPath:     ptr(filepath.Join(outDir, ".inferyard-setup.log")),
			StartedAt:   now(),
		},
		cancel:     cancel,
		resume:     resume,
		polystrata: &reqCopy,
		dataDir:    dataDir,
	}
	s.downloads = append(s.downloads, d)
	s.Bus.Publish("download.state", map[string]any{"task": clone(d.Task)})
	go s.runPolystrataSetup(ctx, *p.Path, d)
	return clone(d.Task), nil
}

func isRegularNonSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func existingPolystrataDataDir(dir string) string {
	for _, rel := range []string{
		filepath.Join(".inferyard-config", "polystrata", "settings.json"),
		filepath.Join(".inferyard-config", "PolyStrata", "settings.json"),
		"installation.json",
	} {
		candidate := filepath.Join(dir, rel)
		if !isRegularNonSymlink(candidate) {
			continue
		}
		raw, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		var payload struct {
			DataDir string `json:"data_dir"`
		}
		if json.Unmarshal(raw, &payload) != nil {
			continue
		}
		trimmed := strings.TrimSpace(payload.DataDir)
		if trimmed != "" && filepath.IsAbs(trimmed) {
			return trimmed
		}
	}
	return ""
}

func isManagedPolystrataDir(root string, u UserSettings, dir, sourceURL string) bool {
	if sourceURL != polystrataDefaultSourceURL || len(u.EngineInstallations) == 0 {
		return false
	}
	leafInfo, err := os.Lstat(dir)
	if err != nil || !leafInfo.IsDir() || leafInfo.Mode()&os.ModeSymlink != 0 {
		return false
	}
	canonicalRoot, err := canonical(root)
	if err != nil {
		canonicalRoot = filepath.Clean(root)
	}
	targetDir, err := canonical(dir)
	if err != nil {
		targetDir = filepath.Clean(dir)
	}
	if filepath.Dir(targetDir) != canonicalRoot {
		return false
	}
	registered := false
	for _, rawInst := range u.EngineInstallations {
		instPath, err := canonical(rawInst)
		if err != nil {
			instPath = filepath.Clean(rawInst)
		}
		if filepath.Dir(instPath) == targetDir {
			registered = true
			break
		}
	}
	if !registered {
		return false
	}
	dirInfo, err := os.Lstat(targetDir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if !isRegularNonSymlink(filepath.Join(targetDir, "setup.py")) ||
		!isRegularNonSymlink(filepath.Join(targetDir, "serve", "server.py")) ||
		!isRegularNonSymlink(filepath.Join(targetDir, "installation.json")) {
		return false
	}
	receiptPath := filepath.Join(targetDir, ".inferyard-runtime.json")
	if !isRegularNonSymlink(receiptPath) {
		return false
	}
	rawReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		return false
	}
	var receipt struct {
		Definition    string `json:"definition"`
		Source        string `json:"source"`
		ArchiveSHA256 string `json:"archive_sha256"`
	}
	if json.Unmarshal(rawReceipt, &receipt) != nil {
		return false
	}
	return receipt.Definition == "polystrata-runtime.v1" &&
		receipt.Source == sourceURL &&
		receipt.ArchiveSHA256 == polystrataDefaultSourceSHA256
}

func existingManagedPolystrataDir(root string, u UserSettings, sourceURL string) (string, string, bool) {
	if sourceURL != polystrataDefaultSourceURL || len(u.EngineInstallations) == 0 {
		return "", "", false
	}
	for _, rawInst := range u.EngineInstallations {
		dir := filepath.Dir(rawInst)
		if !isManagedPolystrataDir(root, u, dir, sourceURL) {
			continue
		}
		canonicalDir, err := canonical(dir)
		if err != nil {
			canonicalDir = filepath.Clean(dir)
		}
		return filepath.Base(canonicalDir), canonicalDir, true
	}
	return "", "", false
}

func polystrataPrepareArgs(outDir, dataDir string, req *PolystrataSetupRequest, resume bool, proxy string) []string {
	args := []string{
		"engine", "prepare-polystrata",
		"--out", outDir,
		"--data-dir", dataDir,
		"--progress-stream",
	}
	if req != nil {
		if v := strings.TrimSpace(req.Family); v != "" {
			args = append(args, "--family", v)
		}
		if v := strings.TrimSpace(req.Model); v != "" {
			args = append(args, "--model", v)
		}
		if req.Context > 0 {
			args = append(args, "--context", strconv.Itoa(req.Context))
		}
		if v := strings.TrimSpace(req.Backend); v != "" {
			args = append(args, "--backend", v)
		}
		if v := strings.TrimSpace(req.ModelSource); v != "" {
			args = append(args, "--model-source", v)
		}
		if v := strings.TrimSpace(req.GGUFPath); v != "" {
			args = append(args, "--gguf-path", v)
		}
	}
	src := ""
	if req != nil {
		src = strings.TrimSpace(req.Source)
	}
	if src == "" || src == polystrataDefaultSourceURL {
		args = append(
			args,
			"--source", polystrataDefaultSourceURL,
			"--expected-sha256", polystrataDefaultSourceSHA256,
		)
	} else {
		args = append(args, "--source", src)
	}
	if resume {
		args = append(args, "--resume")
	}
	if proxy != "" {
		args = append(args, "--proxy-origin", proxy)
	}
	return args
}

func (s *Service) polystrataUVDir() string {
	toolsDir := ""
	if strings.TrimSpace(s.settingsPath) != "" {
		toolsDir = filepath.Join(filepath.Dir(s.settingsPath), "tools")
	}
	uvPath := FindUV(toolsDir)
	if uvPath == "" {
		return ""
	}
	uvDir := filepath.Dir(uvPath)
	if inferyard.PathContainsDir(os.Getenv("PATH"), uvDir) {
		return ""
	}
	return uvDir
}

func (s *Service) runPolystrataSetup(ctx context.Context, binary string, d *downloadEntry) {
	defer d.cancel()
	var streamSeen atomic.Bool
	args := polystrataPrepareArgs(d.Task.OutDir, d.dataDir, d.polystrata, d.resume, s.downloadProxy())
	execCtx := inferyard.WithDrainGrace(ctx, 8*time.Second)
	if uvDir := s.polystrataUVDir(); uvDir != "" {
		execCtx = inferyard.WithPrependedPath(execCtx, uvDir)
	}
	out, execErr := s.execStream(execCtx, s.newStderrProgressHandler(d, &streamSeen), binary, args...)

	if out.ExitCode == inferyard.ExitComplete && execErr == nil && ctx.Err() == nil {
		cr, parseErr := inferyard.Parse(out.Stdout)
		var details struct {
			InstallationPath string `json:"installation_path"`
			ModelConfigPath  string `json:"model_config_path"`
			LogPath          string `json:"log_path"`
		}
		if parseErr == nil && json.Unmarshal(cr.Details, &details) == nil && details.InstallationPath != "" {
			s.applyTransferProgress(d, transferProgressLine{
				Stage:     "verifying_installation",
				Step:      ptr(4),
				StepTotal: ptr(4),
				Detail:    ptr(l10n.TR("core.polystrata.setup.verifying_installation")),
			})
			_, regErr := s.RegisterEngineInstallation(ctx, details.InstallationPath)
			s.mu.Lock()
			d.Task.FinishedAt = ptr(now())
			if details.LogPath != "" {
				d.Task.LogPath = ptr(details.LogPath)
			}
			instPath := details.InstallationPath
			if c, err := canonical(instPath); err == nil {
				instPath = c
			}
			d.Task.InstallationPath = ptr(instPath)
			if details.ModelConfigPath != "" {
				cfgPath := details.ModelConfigPath
				if c, err := canonical(cfgPath); err == nil {
					cfgPath = c
				}
				d.Task.ModelConfigPath = ptr(cfgPath)
			}
			switch {
			case regErr == nil:
				d.Task.State = "done"
				d.Task.Stage = ""
				d.Task.Percent = ptr(100.0)
				d.Task.EnginePath = ptr(instPath)
			case ctx.Err() != nil:
				d.Task.State = "cancelled"
			default:
				d.Task.State = "failed"
				d.Task.Error = regErr
			}
			doneOK := d.Task.State == "done"
			taskSnap := clone(d.Task)
			s.mu.Unlock()
			if doneOK {
				s.VerifyEngines()
			}
			s.Bus.Publish("download.state", map[string]any{"task": taskSnap})
			if doneOK {
				go func() { _ = s.RefreshAssets() }()
			}
			return
		}
	}

	s.mu.Lock()
	var parsedBlockers []map[string]any
	if cr, err := inferyard.Parse(out.Stdout); err == nil {
		var errDetails struct {
			LogPath  string `json:"log_path"`
			Blockers []struct {
				Code          string  `json:"code"`
				Message       string  `json:"message"`
				FixCommand    *string `json:"fix_command"`
				RequiredBytes *int64  `json:"required_bytes"`
				FreeBytes     *int64  `json:"free_bytes"`
			} `json:"blockers"`
		}
		if json.Unmarshal(cr.Details, &errDetails) == nil {
			if errDetails.LogPath != "" {
				d.Task.LogPath = ptr(errDetails.LogPath)
			}
			for _, b := range errDetails.Blockers {
				if strings.TrimSpace(b.Code) == "" {
					continue
				}
				entry := map[string]any{"code": b.Code, "message": b.Message}
				if b.FixCommand != nil && strings.TrimSpace(*b.FixCommand) != "" {
					entry["fix_command"] = *b.FixCommand
				}
				if b.RequiredBytes != nil {
					entry["required_bytes"] = *b.RequiredBytes
				}
				if b.FreeBytes != nil {
					entry["free_bytes"] = *b.FreeBytes
				}
				parsedBlockers = append(parsedBlockers, entry)
			}
		}
	}
	if d.Task.LogPath == nil {
		candidateLog := filepath.Join(d.Task.OutDir, ".inferyard-setup.log")
		if info, err := os.Stat(candidateLog); err == nil && info.Mode().IsRegular() {
			d.Task.LogPath = ptr(candidateLog)
		}
	}
	s.finishDownloadLocked(ctx, d, out, execErr, func(inferyard.CommandResult, error) bool {
		return false
	})
	if len(parsedBlockers) > 0 && d.Task.Error != nil {
		if d.Task.Error.Details == nil {
			d.Task.Error.Details = map[string]any{}
		}
		d.Task.Error.Details["blockers"] = parsedBlockers
		s.Bus.Publish("download.state", map[string]any{"task": clone(d.Task)})
	}
	s.mu.Unlock()
}
