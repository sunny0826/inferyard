package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

type downloadEntry struct {
	Task       DownloadTask
	cancel     context.CancelFunc
	tokenEnv   string
	resume     bool
	retryOf    string
	polystrata *PolystrataSetupRequest
	dataDir    string
}

var outputName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var revision = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

func parsePinnedSourceParts(source string) ([]string, bool) {
	u, e := url.Parse(source)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, false
	}
	if u.Host != "huggingface.co" && u.Host != "modelscope.cn" {
		return nil, false
	}
	for _, c := range source {
		if c < 33 || c >= 127 {
			return nil, false
		}
	}
	p := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	for i, part := range p {
		decoded, e := url.PathUnescape(part)
		if e != nil {
			return nil, false
		}
		p[i] = decoded
		for _, c := range decoded {
			if c < 32 || c >= 127 || strings.ContainsRune("/\\:", c) {
				return nil, false
			}
		}
		if part == "" || part == "." || part == ".." || decoded == "" || decoded == "." || decoded == ".." || strings.TrimSpace(decoded) != decoded || strings.Contains(decoded, "\\") {
			return nil, false
		}
	}
	owner := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	if len(p) < 4 || !owner.MatchString(p[0]) || !owner.MatchString(p[1]) || !revision.MatchString(p[3]) {
		return nil, false
	}
	return p, true
}

func validSource(source string) bool {
	p, ok := parsePinnedSourceParts(source)
	return ok && len(p) >= 5 && p[2] == "blob" && strings.HasSuffix(p[len(p)-1], ".gguf")
}

func validDirectorySource(source string) bool {
	p, ok := parsePinnedSourceParts(source)
	return ok && len(p) >= 4 && p[2] == "tree" && (len(p) == 4 || !strings.HasSuffix(p[len(p)-1], ".gguf"))
}

func (s *Service) activeDownloadLocked(kind, sourceURL, requestedSource string) (DownloadTask, bool) {
	for _, d := range s.downloads {
		if d.Task.Kind != kind || d.Task.State != "downloading" {
			continue
		}
		for _, want := range []string{sourceURL, requestedSource} {
			if want == "" {
				continue
			}
			if d.Task.SourceUrl == want || (d.Task.RequestedSourceUrl != "" && d.Task.RequestedSourceUrl == want) {
				return clone(d.Task), true
			}
		}
	}
	return DownloadTask{}, false
}

func (s *Service) activeRetryOrDownloadLocked(retryOf, kind, sourceURL string) (DownloadTask, bool) {
	if retryOf != "" {
		for _, d := range s.downloads {
			if d.Task.State == "downloading" && d.retryOf == retryOf {
				return clone(d.Task), true
			}
		}
	}
	if sourceURL != "" {
		for _, d := range s.downloads {
			if d.Task.Kind == kind && d.Task.State == "downloading" && d.Task.SourceUrl == sourceURL {
				return clone(d.Task), true
			}
		}
	}
	return DownloadTask{}, false
}

func (s *Service) CreateModelDownload(req CreateDownloadRequest, tokenEnv string) (DownloadTask, *ApiError) {
	return s.createModelDownload(req, "", tokenEnv)
}

func (s *Service) createModelDownload(req CreateDownloadRequest, requestedSource, tokenEnv string) (DownloadTask, *ApiError) {
	if (req.OutName != "" && !ValidDownloadName(req.OutName)) || (!validSource(req.SourceUrl) && !validDirectorySource(req.SourceUrl)) || (tokenEnv != "" && !envName.MatchString(tokenEnv)) {
		return DownloadTask{}, Err("invalid_request")
	}
	u := downloadNameDefaults(s.userSettings())
	root, apiErr := s.downloadRoot("model", u)
	if apiErr != nil {
		return DownloadTask{}, apiErr
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return DownloadTask{}, Err("cli_unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if apiErr := s.admitDownloadLocked(); apiErr != nil {
		return DownloadTask{}, apiErr
	}
	if req.OutName == "" {
		if existing, found := s.activeDownloadLocked("model", req.SourceUrl, requestedSource); found {
			return existing, nil
		}
	}
	base := resolveAutoDownloadName("model", u.ModelDownloadName, req.SourceUrl)
	name, dir, failure := s.downloadOutputLocked(root, req.OutName, base)
	if failure != nil {
		return DownloadTask{}, failure
	}
	ctx, cancel := context.WithCancel(s.ctx)
	d := &downloadEntry{Task: DownloadTask{
		Kind:               "model",
		DisplayName:        ResolveDownloadDisplayName("model", req.SourceUrl),
		TaskId:             uuid(),
		State:              "downloading",
		SourceUrl:          req.SourceUrl,
		RequestedSourceUrl: requestedSource,
		OutName:            name,
		OutDir:             dir,
		TotalBytes:         catalogTotalBytes(req.SourceUrl),
		StartedAt:          now(),
	}, cancel: cancel, tokenEnv: tokenEnv}
	s.downloads = append(s.downloads, d)
	s.Bus.Publish("download.state", map[string]any{"task": d.Task})
	go s.download(ctx, *p.Path, d)
	return clone(d.Task), nil
}
func (s *Service) Downloads() []DownloadTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []DownloadTask{}
	for i := len(s.downloads) - 1; i >= 0; i-- {
		out = append(out, clone(s.downloads[i].Task))
	}
	return out
}
func (s *Service) CancelDownload(id string) (DownloadTask, *ApiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.downloads {
		if d.Task.TaskId == id {
			if d.Task.State != "downloading" {
				return DownloadTask{}, Conflict("invalid_state")
			}
			d.cancel()
			return clone(d.Task), nil
		}
	}
	return DownloadTask{}, Err("not_found")
}
func (s *Service) download(ctx context.Context, binary string, d *downloadEntry) {
	defer d.cancel()
	type result struct {
		out inferyard.Execution
		err error
	}
	var streamSeen atomic.Bool
	done := make(chan result, 1)
	go func() {
		args := []string{"model", "acquire", "--progress-stream"}
		if validDirectorySource(d.Task.SourceUrl) {
			args = append(args, "--kind", "directory")
		}
		if d.resume {
			args = append(args, "--resume")
		}
		if d.tokenEnv != "" {
			args = append(args, "--token-env", d.tokenEnv)
		}
		if proxy := s.downloadProxy(); proxy != "" {
			args = append(args, "--proxy-origin", proxy)
		}
		args = append(args, "--source", d.Task.SourceUrl, "--out", d.Task.OutDir)
		o, e := s.execStream(ctx, s.newStderrProgressHandler(d, &streamSeen), binary, args...)
		done <- result{o, e}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	type reading struct {
		at    time.Time
		bytes int64
	}
	window := []reading{}
	poll := func() {
		entries, e := os.ReadDir(d.Task.OutDir)
		if e != nil {
			return
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".part") || strings.HasSuffix(entry.Name(), ".json.part") {
				continue
			}
			info, e := entry.Info()
			if e != nil || !info.Mode().IsRegular() {
				continue
			}
			if streamSeen.Load() {
				s.mu.Lock()
				if d.Task.FileName == nil {
					d.Task.FileName = ptr(entry.Name())
				}
				s.mu.Unlock()
				break
			}
			at := time.Now()
			window = append(window, reading{at, info.Size()})
			if len(window) > 5 {
				window = window[len(window)-5:]
			}
			var rate *float64
			if len(window) > 1 && info.Size() >= window[0].bytes {
				rate = ptr(float64(info.Size()-window[0].bytes) / at.Sub(window[0].at).Seconds())
			}
			s.mu.Lock()
			d.Task.FileName = ptr(entry.Name())
			d.Task.DownloadedBytes = info.Size()
			d.Task.BytesPerSecond = rate
			s.Bus.Publish("download.progress", map[string]any{"task_id": d.Task.TaskId, "downloaded_bytes": info.Size(), "total_bytes": d.Task.TotalBytes, "bytes_per_second": rate, "utc": now()})
			s.mu.Unlock()
			break
		}
	}
	var r result
loop:
	for {
		select {
		case <-ticker.C:
			poll()
		case r = <-done:
			poll()
			break loop
		}
	}
	s.mu.Lock()
	s.finishDownloadLocked(ctx, d, r.out, r.err, func(cr inferyard.CommandResult, parseErr error) bool {
		var result DownloadResult
		e := parseErr
		if e == nil {
			e = json.Unmarshal(cr.Details, &result)
		}
		if e != nil || result.Sha256 == "" {
			raw, e1 := os.ReadFile(filepath.Join(d.Task.OutDir, "source.json"))
			if e1 == nil {
				e = json.Unmarshal(raw, &result)
			} else {
				e = e1
			}
		}
		if e != nil || result.Bytes < 0 || len(result.Sha256) != 64 {
			return false
		}
		d.Task.Stage = ""
		d.Task.Result = &result
		d.Task.DownloadedBytes = result.Bytes
		return true
	})
	doneOK := d.Task.State == "done"
	s.mu.Unlock()
	if doneOK {
		_ = s.RefreshAssets()
	}
}

func (s *Service) finishDownloadLocked(ctx context.Context, d *downloadEntry, out inferyard.Execution, execErr error, apply func(inferyard.CommandResult, error) bool) {
	d.Task.FinishedAt = ptr(now())
	switch {
	case out.ExitCode == inferyard.ExitCancelled || ctx.Err() != nil:
		d.Task.State = "cancelled"
	case execErr != nil:
		d.Task.State = "failed"
		d.Task.Error = Err("internal_error")
	case out.ExitCode != inferyard.ExitComplete:
		d.Task.State = "failed"
		d.Task.Error = exitError(out, nil)
		if d.Task.Error == nil {
			d.Task.Error = Err("cli_error")
		}
	default:
		cr, parseErr := inferyard.Parse(out.Stdout)
		if !apply(cr, parseErr) {
			d.Task.State = "failed"
			d.Task.Error = Err("internal_error")
		} else {
			d.Task.State = "done"
		}
	}
	s.Bus.Publish("download.state", map[string]any{"task": d.Task})
}

func (s *Service) CreateEngineDownload(source, outName string) (DownloadTask, *ApiError) {
	if !validEngineSource(source) || (outName != "" && !ValidDownloadName(outName)) {
		return DownloadTask{}, Err("invalid_request")
	}
	u := downloadNameDefaults(s.userSettings())
	root, apiErr := s.downloadRoot("engine", u)
	if apiErr != nil {
		return DownloadTask{}, apiErr
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return DownloadTask{}, Err("cli_unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if apiErr := s.admitDownloadLocked(); apiErr != nil {
		return DownloadTask{}, apiErr
	}
	if outName == "" {
		if existing, found := s.activeDownloadLocked("engine", source, ""); found {
			return existing, nil
		}
	}
	base := resolveAutoDownloadName("engine", u.EngineDownloadName, source)
	outName, dir, failure := s.downloadOutputLocked(root, outName, base)
	if failure != nil {
		return DownloadTask{}, failure
	}
	ctx, cancel := context.WithCancel(s.ctx)
	d := &downloadEntry{Task: DownloadTask{
		Kind:           "engine",
		DisplayName:    ResolveDownloadDisplayName("engine", source),
		TaskId:         uuid(),
		State:          "downloading",
		SourceUrl:      source,
		OutName:        outName,
		OutDir:         dir,
		TotalBytes:     catalogTotalBytes(source),
		StartedAt:      now(),
		ProgressReason: ptr("engine_transfer_progress_unavailable"),
	}, cancel: cancel}
	s.downloads = append(s.downloads, d)
	s.Bus.Publish("download.state", map[string]any{"task": d.Task})
	go s.downloadEngine(ctx, *p.Path, d)
	return clone(d.Task), nil
}

func enginePrepareArgs(sourceURL, outDir string, resume bool) []string {
	args := []string{"engine", "prepare", "--source", sourceURL, "--out", outDir}
	if entry, ok := CatalogEntryByURL(sourceURL); ok {
		if entry.SHA256 != "" {
			args = append(args, "--expected-sha256", entry.SHA256)
		}
		if entry.Profile != "" {
			args = append(args, "--profile", entry.Profile)
		}
		if entry.RuntimeURL != "" {
			args = append(args, "--runtime-source", entry.RuntimeURL)
		}
		if entry.RuntimeSHA256 != "" {
			args = append(args, "--runtime-expected-sha256", entry.RuntimeSHA256)
		}
	}
	args = append(args, "--progress-stream")
	if resume {
		args = append(args, "--resume")
	}
	return args
}

func (s *Service) downloadEngine(ctx context.Context, binary string, d *downloadEntry) {
	defer d.cancel()
	if d.Task.ProgressReason != nil {
		s.Bus.Publish("download.progress", map[string]any{
			"task_id": d.Task.TaskId, "downloaded_bytes": nil, "bytes_per_second": nil,
			"total_bytes": d.Task.TotalBytes, "progress_reason": d.Task.ProgressReason, "utc": now(),
		})
	}
	var streamSeen atomic.Bool
	args := enginePrepareArgs(d.Task.SourceUrl, d.Task.OutDir, d.resume)
	if proxy := s.downloadProxy(); proxy != "" {
		args = append(args, "--proxy-origin", proxy)
	}
	out, execErr := s.execStream(ctx, s.newStderrProgressHandler(d, &streamSeen), binary, args...)
	s.mu.Lock()
	s.finishDownloadLocked(ctx, d, out, execErr, func(cr inferyard.CommandResult, parseErr error) bool {
		if parseErr != nil {
			return false
		}
		var details struct {
			Engine string `json:"engine"`
			Bytes  int64  `json:"bytes"`
			Sha256 string `json:"sha256"`
		}
		if err := json.Unmarshal(cr.Details, &details); err != nil || details.Engine == "" || details.Bytes < 0 || len(details.Sha256) != 64 {
			return false
		}
		d.Task.Stage = ""
		d.Task.EnginePath = ptr(details.Engine)
		d.Task.Result = &DownloadResult{Bytes: details.Bytes, Sha256: details.Sha256}
		return true
	})
	doneOK := d.Task.State == "done"
	s.mu.Unlock()
	if doneOK {
		go s.VerifyEngines()
	}
}
