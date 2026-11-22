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
	} else if ev.Stage != "" && ev.Stage != "downloading" {
		d.Task.BytesPerSecond = nil
	}
	if ev.DownloadedBytes != nil || ev.TotalBytes != nil || ev.BytesPerSecond != nil {
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
	if kind == "engine" {
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
	if s.closing {
		s.mu.Unlock()
		return DownloadTask{}, Err("service_closing")
	}
	kind := orig.Task.Kind
	sourceURL := orig.Task.SourceUrl
	requestedSourceURL := orig.Task.RequestedSourceUrl
	if existing, found := s.activeRetryOrDownloadLocked(taskID, kind, sourceURL, requestedSourceURL); found {
		s.mu.Unlock()
		return existing, nil
	}
	tokenEnv := orig.tokenEnv
	prevOutName := orig.Task.OutName
	prevOutDir := orig.Task.OutDir
	displayName := orig.Task.DisplayName
	totalBytes := orig.Task.TotalBytes
	s.mu.Unlock()

	u := downloadNameDefaults(s.userSettings())
	root, apiErr := s.downloadRoot(kind, u)
	if apiErr != nil {
		return DownloadTask{}, apiErr
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return DownloadTask{}, Err("cli_unavailable")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return DownloadTask{}, Err("service_closing")
	}
	if existing, found := s.activeRetryOrDownloadLocked(taskID, kind, sourceURL, requestedSourceURL); found {
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
		canReuse, partBytes = inspectResumeDirectory(resolvedPrev)
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
		base := resolveAutoDownloadName(kind, configured, sourceURL)
		var failure *ApiError
		outName, outDir, failure = s.downloadOutputLocked(root, "", base)
		if failure != nil {
			return DownloadTask{}, failure
		}
	}
	if displayName == "" {
		displayName = ResolveDownloadDisplayName(kind, sourceURL)
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
		OutDir:          outDir,
		DownloadedBytes: partBytes,
		TotalBytes:      totalBytes,
		StartedAt:       now(),
	}
	if kind == "engine" && partBytes == 0 {
		task.ProgressReason = ptr("engine_transfer_progress_unavailable")
	}
	entry := &downloadEntry{Task: task, cancel: cancel, tokenEnv: tokenEnv, resume: resume, retryOf: taskID}
	s.downloads = append(s.downloads, entry)
	s.Bus.Publish("download.state", map[string]any{"task": entry.Task})
	if kind == "engine" {
		go s.downloadEngine(ctx, *p.Path, entry)
	} else {
		go s.download(ctx, *p.Path, entry)
	}
	return clone(entry.Task), nil
}
