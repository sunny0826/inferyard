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
	"time"
)

type downloadEntry struct {
	Task     DownloadTask
	cancel   context.CancelFunc
	tokenEnv string
}

var outputName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var revision = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

func validSource(source string) bool {
	u, e := url.Parse(source)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Host != "huggingface.co" && u.Host != "modelscope.cn" {
		return false
	}
	for _, c := range source {
		if c < 33 || c >= 127 {
			return false
		}
	}
	p := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	for i, part := range p {
		decoded, e := url.PathUnescape(part)
		if e != nil {
			return false
		}
		p[i] = decoded
		for _, c := range decoded {
			if c < 32 || c >= 127 || strings.ContainsRune("/\\:", c) {
				return false
			}
		}
		if strings.TrimSpace(decoded) != decoded {
			return false
		}
	}
	owner := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	if len(p) < 5 || !owner.MatchString(p[0]) || !owner.MatchString(p[1]) || p[0] == "" || p[1] == "" || p[2] != "blob" || !revision.MatchString(p[3]) || !strings.HasSuffix(p[len(p)-1], ".gguf") {
		return false
	}
	for _, part := range p {
		if part == "" || part == "." || part == ".." || strings.Contains(part, "\\") {
			return false
		}
	}
	return true
}
func (s *Service) CreateModelDownload(req CreateDownloadRequest, tokenEnv string) (DownloadTask, *ApiError) {
	if (req.OutName != "" && !ValidDownloadName(req.OutName)) || !validSource(req.SourceUrl) || (tokenEnv != "" && !envName.MatchString(tokenEnv)) {
		return DownloadTask{}, Err("invalid_request")
	}
	u := downloadNameDefaults(s.userSettings())
	if u.ModelsRoot == nil {
		return DownloadTask{}, Conflict("settings_incomplete")
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return DownloadTask{}, Err("cli_unavailable")
	}
	if e := os.MkdirAll(*u.ModelsRoot, 0700); e != nil {
		return DownloadTask{}, Err("internal_error")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return DownloadTask{}, Err("service_closing")
	}
	name, dir, failure := s.downloadOutputLocked(*u.ModelsRoot, req.OutName, u.ModelDownloadName)
	if failure != nil {
		return DownloadTask{}, failure
	}
	ctx, cancel := context.WithCancel(s.ctx)
	d := &downloadEntry{Task: DownloadTask{Kind: "model", TaskId: uuid(), State: "downloading", SourceUrl: req.SourceUrl, OutName: name, OutDir: dir, StartedAt: now()}, cancel: cancel, tokenEnv: tokenEnv}
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
	done := make(chan result, 1)
	go func() {
		args := []string{"model", "acquire", "--source", d.Task.SourceUrl, "--out", d.Task.OutDir}
		if d.tokenEnv != "" {
			args = append(args, "--token-env", d.tokenEnv)
		}
		o, e := s.exec(ctx, binary, args...)
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
			if !strings.HasSuffix(entry.Name(), ".part") {
				continue
			}
			info, e := entry.Info()
			if e != nil || !info.Mode().IsRegular() {
				continue
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
			s.Bus.Publish("download.progress", map[string]any{"task_id": d.Task.TaskId, "downloaded_bytes": info.Size(), "bytes_per_second": rate, "utc": now()})
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
	defer s.mu.Unlock()
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
		d.Task.Result = &result
		d.Task.DownloadedBytes = result.Bytes
		return true
	})
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
	workspace := str(u.WorkspaceRoot)
	if workspace == "" {
		return DownloadTask{}, Conflict("settings_incomplete")
	}
	root, err := Resolve(workspace, "engines")
	if s.paths != nil {
		root, err = Resolve(s.paths.Engines, s.paths.Engines)
	}
	if err != nil {
		return DownloadTask{}, Err("invalid_request")
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return DownloadTask{}, Err("cli_unavailable")
	}
	if e := os.MkdirAll(root, 0755); e != nil {
		return DownloadTask{}, Err("internal_error")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return DownloadTask{}, Err("service_closing")
	}
	outName, dir, failure := s.downloadOutputLocked(root, outName, u.EngineDownloadName)
	if failure != nil {
		return DownloadTask{}, failure
	}
	ctx, cancel := context.WithCancel(s.ctx)
	d := &downloadEntry{Task: DownloadTask{
		Kind: "engine", TaskId: uuid(), State: "downloading", SourceUrl: source,
		OutName: outName, OutDir: dir, StartedAt: now(),
		ProgressReason: ptr("engine_transfer_progress_unavailable"),
	}, cancel: cancel}
	s.downloads = append(s.downloads, d)
	s.Bus.Publish("download.state", map[string]any{"task": d.Task})
	go s.downloadEngine(ctx, *p.Path, d)
	return clone(d.Task), nil
}

func enginePrepareArgs(sourceURL, outDir string) []string {
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
	return args
}

func (s *Service) downloadEngine(ctx context.Context, binary string, d *downloadEntry) {
	defer d.cancel()
	s.Bus.Publish("download.progress", map[string]any{
		"task_id": d.Task.TaskId, "downloaded_bytes": nil, "bytes_per_second": nil,
		"progress_reason": d.Task.ProgressReason, "utc": now(),
	})
	out, execErr := s.exec(ctx, binary, enginePrepareArgs(d.Task.SourceUrl, d.Task.OutDir)...)
	s.mu.Lock()
	defer s.mu.Unlock()
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
		d.Task.EnginePath = ptr(details.Engine)
		d.Task.Result = &DownloadResult{Bytes: details.Bytes, Sha256: details.Sha256}
		return true
	})
}
