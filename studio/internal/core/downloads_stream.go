package core

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

type transferProgressLine struct {
	Event           string   `json:"event"`
	Stage           string   `json:"stage"`
	DownloadedBytes *int64   `json:"downloaded_bytes"`
	TotalBytes      *int64   `json:"total_bytes"`
	BytesPerSecond  *float64 `json:"bytes_per_second"`
	Step            *int     `json:"step,omitempty"`
	StepTotal       *int     `json:"step_total,omitempty"`
	Percent         *float64 `json:"percent,omitempty"`
	Detail          *string  `json:"detail,omitempty"`
}

func parseTransferProgress(line string) (transferProgressLine, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") {
		return transferProgressLine{}, false
	}
	var ev transferProgressLine
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		return transferProgressLine{}, false
	}
	if ev.Event != "transfer.progress" {
		return transferProgressLine{}, false
	}
	if ev.DownloadedBytes != nil && *ev.DownloadedBytes < 0 {
		return transferProgressLine{}, false
	}
	if ev.TotalBytes != nil && *ev.TotalBytes <= 0 {
		ev.TotalBytes = nil
	}
	if ev.BytesPerSecond != nil && (math.IsNaN(*ev.BytesPerSecond) || math.IsInf(*ev.BytesPerSecond, 0) || *ev.BytesPerSecond < 0) {
		ev.BytesPerSecond = nil
	}
	if ev.Step != nil && *ev.Step < 0 {
		ev.Step = nil
	}
	if ev.StepTotal != nil && *ev.StepTotal <= 0 {
		ev.StepTotal = nil
	}
	if ev.Percent != nil {
		if math.IsNaN(*ev.Percent) || math.IsInf(*ev.Percent, 0) {
			ev.Percent = nil
		} else if *ev.Percent < 0 {
			ev.Percent = ptr(0.0)
		} else if *ev.Percent > 100 {
			ev.Percent = ptr(100.0)
		}
	}
	return ev, true
}

func (s *Service) newStderrProgressHandler(d *downloadEntry, streamSeen *atomic.Bool) func(string) {
	return func(line string) {
		ev, ok := parseTransferProgress(line)
		if !ok {
			return
		}
		streamSeen.Store(true)
		s.applyTransferProgress(d, ev)
	}
}

func (s *Service) applyTransferProgress(d *downloadEntry, ev transferProgressLine) {
	s.mu.Lock()
	stageChanged := ev.Stage != "" && ev.Stage != d.Task.Stage
	stepChanged := ev.Step != nil && (d.Task.Step == nil || *ev.Step != *d.Task.Step)
	if d.Task.Kind == "polystrata" && (stageChanged || stepChanged) {
		if ev.Percent == nil {
			d.Task.Percent = nil
		}
		if ev.TotalBytes == nil {
			d.Task.TotalBytes = nil
		}
		if ev.Detail == nil {
			d.Task.Detail = nil
		}
		if ev.BytesPerSecond == nil {
			d.Task.BytesPerSecond = nil
		}
	}
	if ev.Stage != "" {
		d.Task.Stage = ev.Stage
	}
	if ev.DownloadedBytes != nil {
		d.Task.DownloadedBytes = *ev.DownloadedBytes
	}
	if ev.TotalBytes != nil {
		d.Task.TotalBytes = ptr(*ev.TotalBytes)
	}
	if ev.BytesPerSecond != nil {
		d.Task.BytesPerSecond = ptr(*ev.BytesPerSecond)
	} else if ev.Stage != "" && ev.Stage != "downloading" && ev.Stage != "downloading_runtime" && ev.Stage != "downloading_model" {
		d.Task.BytesPerSecond = nil
	}
	if ev.Step != nil {
		d.Task.Step = ptr(*ev.Step)
	}
	if ev.StepTotal != nil {
		d.Task.StepTotal = ptr(*ev.StepTotal)
	}
	if ev.Percent != nil {
		d.Task.Percent = ptr(*ev.Percent)
	}
	if ev.Detail != nil {
		detail := strings.TrimSpace(*ev.Detail)
		if detail != "" {
			d.Task.Detail = ptr(detail)
		}
	}
	if ev.DownloadedBytes != nil || ev.TotalBytes != nil || ev.BytesPerSecond != nil || ev.Percent != nil {
		d.Task.ProgressReason = nil
	}
	if d.Task.FileName == nil {
		if name := detectPartFileName(d.Task.OutDir); name != "" {
			d.Task.FileName = ptr(name)
		}
	}
	payload := map[string]any{
		"task_id":          d.Task.TaskId,
		"stage":            d.Task.Stage,
		"downloaded_bytes": d.Task.DownloadedBytes,
		"total_bytes":      d.Task.TotalBytes,
		"bytes_per_second": d.Task.BytesPerSecond,
		"utc":              now(),
	}
	if d.Task.Step != nil {
		payload["step"] = *d.Task.Step
	}
	if d.Task.StepTotal != nil {
		payload["step_total"] = *d.Task.StepTotal
	}
	if d.Task.Percent != nil {
		payload["percent"] = *d.Task.Percent
	}
	if d.Task.Detail != nil {
		payload["detail"] = *d.Task.Detail
	}
	s.Bus.Publish("download.progress", payload)
	s.mu.Unlock()
}

func detectPartFileName(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".part") && !strings.HasSuffix(name, ".json.part") {
			return name
		}
	}
	return ""
}

func catalogTotalBytes(sourceURL string) *int64 {
	entry, ok := CatalogEntryByURL(sourceURL)
	if !ok || entry.Bytes <= 0 {
		return nil
	}
	total := entry.Bytes
	if entry.RuntimeBytes > 0 {
		total += entry.RuntimeBytes
	}
	return ptr(total)
}

func inspectResumeDirectory(dir string) (bool, int64) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return true, 0
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, 0
	}
	var totalPartBytes int64
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".part") {
			return false, 0
		}
		p := filepath.Join(dir, name)
		st, err := os.Lstat(p)
		if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
			return false, 0
		}
		if !strings.HasSuffix(name, ".json.part") {
			totalPartBytes += st.Size()
		}
	}
	return true, totalPartBytes
}

func (s *Service) downloadRoot(kind string, u UserSettings) (string, *ApiError) {
	if kind == "engine" || kind == "polystrata" {
		var root string
		var err error
		if s.paths != nil {
			root, err = Resolve(s.paths.Engines, s.paths.Engines)
		} else {
			workspace := str(u.WorkspaceRoot)
			if workspace == "" {
				return "", Conflict("settings_incomplete")
			}
			root, err = Resolve(workspace, "engines")
		}
		if err != nil {
			return "", Err("invalid_request")
		}
		if err := os.MkdirAll(root, 0755); err != nil {
			return "", Err("internal_error")
		}
		return root, nil
	}
	if u.ModelsRoot == nil {
		return "", Conflict("settings_incomplete")
	}
	if err := os.MkdirAll(*u.ModelsRoot, 0700); err != nil {
		return "", Err("internal_error")
	}
	return *u.ModelsRoot, nil
}

func (s *Service) RetryDownload(taskID string) (DownloadTask, *ApiError) {
	s.mu.Lock()
	var orig *downloadEntry
	for _, d := range s.downloads {
		if d.Task.TaskId == taskID {
			orig = d
			break
		}
	}
	if orig == nil {
		s.mu.Unlock()
		return DownloadTask{}, Err("not_found")
	}
	if orig.Task.State != "failed" && orig.Task.State != "cancelled" {
		s.mu.Unlock()
		return DownloadTask{}, Conflict("invalid_state")
	}
	if apiErr := s.admitDownloadLocked(); apiErr != nil {
		s.mu.Unlock()
		return DownloadTask{}, apiErr
	}
	kind := orig.Task.Kind
	sourceURL := orig.Task.SourceUrl
	requestedSourceURL := orig.Task.RequestedSourceUrl
	var polystrataCopy *PolystrataSetupRequest
	if orig.polystrata != nil {
		copied := *orig.polystrata
		polystrataCopy = &copied
	}
	if kind == "polystrata" {
		if active, found := s.activePolystrataSetupLocked(); found {
			if active.retryOf == taskID || samePolystrataRequest(active.polystrata, polystrataCopy) {
				cloned := clone(active.Task)
				s.mu.Unlock()
				return cloned, nil
			}
			s.mu.Unlock()
			return DownloadTask{}, Conflict("polystrata_setup_active")
		}
	} else if existing, found := s.activeRetryOrDownloadLocked(taskID, kind, sourceURL); found {
		s.mu.Unlock()
		return existing, nil
	}
	tokenEnv := orig.tokenEnv
	prevOutName := orig.Task.OutName
	prevOutDir := orig.Task.OutDir
	displayName := orig.Task.DisplayName
	totalBytes := orig.Task.TotalBytes
	prevDataDir := orig.dataDir
	s.mu.Unlock()

	u := downloadNameDefaults(s.userSettings())
	root, apiErr := s.downloadRoot(kind, u)
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
	if kind == "polystrata" {
		if active, found := s.activePolystrataSetupLocked(); found {
			if active.retryOf == taskID || samePolystrataRequest(active.polystrata, polystrataCopy) {
				return clone(active.Task), nil
			}
			return DownloadTask{}, Conflict("polystrata_setup_active")
		}
	} else if existing, found := s.activeRetryOrDownloadLocked(taskID, kind, sourceURL); found {
		return existing, nil
	}
	resolvedPrev, resolveErr := Resolve(root, prevOutDir)
	inUse := false
	for _, other := range s.downloads {
		if other.Task.OutDir == resolvedPrev && (other.Task.State == "downloading" || other.Task.State == "done") {
			inUse = true
			break
		}
	}
	canReuse, partBytes := false, int64(0)
	if resolveErr == nil && !inUse {
		if kind == "polystrata" {
			info, statErr := os.Lstat(resolvedPrev)
			canReuse = os.IsNotExist(statErr) || (statErr == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir())
		} else {
			canReuse, partBytes = inspectResumeDirectory(resolvedPrev)
		}
	}
	var outName, outDir string
	var resume bool
	if canReuse && ValidDownloadName(prevOutName) {
		outName, outDir, resume = prevOutName, resolvedPrev, true
	} else {
		configured := u.ModelDownloadName
		if kind == "engine" {
			configured = u.EngineDownloadName
		}
		base := "polystrata"
		if kind != "polystrata" {
			base = resolveAutoDownloadName(kind, configured, sourceURL)
		}
		var failure *ApiError
		outName, outDir, failure = s.downloadOutputLocked(root, "", base)
		if failure != nil {
			return DownloadTask{}, failure
		}
	}
	if displayName == "" {
		if kind == "polystrata" && polystrataCopy != nil {
			displayName = polystrataDisplayName(*polystrataCopy)
		} else {
			displayName = ResolveDownloadDisplayName(kind, sourceURL)
		}
	}
	if totalBytes == nil {
		totalBytes = catalogTotalBytes(sourceURL)
	} else {
		totalBytes = ptr(*totalBytes)
	}
	ctx, cancel := context.WithCancel(s.ctx)
	task := DownloadTask{
		Kind:               kind,
		DisplayName:        displayName,
		TaskId:             uuid(),
		State:              "downloading",
		SourceUrl:          sourceURL,
		RequestedSourceUrl: requestedSourceURL,
		OutName:            outName,
		OutDir:             outDir,
		DownloadedBytes:    partBytes,
		TotalBytes:         totalBytes,
		StartedAt:          now(),
	}
	if kind == "engine" && partBytes == 0 {
		task.ProgressReason = ptr("engine_transfer_progress_unavailable")
	}
	if kind == "polystrata" {
		task.Stage = "preflight"
		task.LogPath = ptr(filepath.Join(outDir, ".inferyard-setup.log"))
	}
	entry := &downloadEntry{
		Task:       task,
		cancel:     cancel,
		tokenEnv:   tokenEnv,
		resume:     resume,
		retryOf:    taskID,
		polystrata: polystrataCopy,
		dataDir:    prevDataDir,
	}
	s.downloads = append(s.downloads, entry)
	s.Bus.Publish("download.state", map[string]any{"task": clone(entry.Task)})
	switch kind {
	case "polystrata":
		go s.runPolystrataSetup(ctx, *p.Path, entry)
	case "engine":
		go s.downloadEngine(ctx, *p.Path, entry)
	default:
		go s.download(ctx, *p.Path, entry)
	}
	return clone(entry.Task), nil
}
