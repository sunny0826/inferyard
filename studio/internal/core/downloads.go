package core

import (
	"context"
	"encoding/json"
	"inferyard-studio/internal/inferyard"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type downloadEntry struct {
	Task   DownloadTask
	cancel context.CancelFunc
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
func (s *Service) CreateDownload(req CreateDownloadRequest) (DownloadTask, *ApiError) {
	if !outputName.MatchString(req.OutName) || strings.Trim(req.OutName, ".") == "" || !validSource(req.SourceUrl) {
		return DownloadTask{}, Err("invalid_request")
	}
	u := s.userSettings()
	if u.ModelsRoot == nil {
		return DownloadTask{}, Conflict("settings_incomplete")
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return DownloadTask{}, Err("cli_unavailable")
	}
	dir, e := Resolve(*u.ModelsRoot, req.OutName)
	if e != nil {
		return DownloadTask{}, Err("invalid_request")
	}
	if e = os.MkdirAll(*u.ModelsRoot, 0700); e != nil {
		return DownloadTask{}, Err("internal_error")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e = os.Lstat(dir); !os.IsNotExist(e) {
		if e != nil {
			return DownloadTask{}, Err("internal_error")
		}
		return DownloadTask{}, Conflict("output_exists")
	}
	for _, d := range s.downloads {
		if d.Task.OutDir == dir {
			return DownloadTask{}, Conflict("output_exists")
		}
	}
	ctx, cancel := context.WithCancel(s.ctx)
	d := &downloadEntry{Task: DownloadTask{TaskId: uuid(), State: "downloading", SourceUrl: req.SourceUrl, OutName: req.OutName, OutDir: dir, StartedAt: now()}, cancel: cancel}
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
		o, e := s.exec(ctx, binary, "model", "acquire", "--source", d.Task.SourceUrl, "--out", d.Task.OutDir)
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
	d.Task.FinishedAt = ptr(now())
	switch {
	case r.out.ExitCode == 130 || ctx.Err() != nil:
		d.Task.State = "cancelled"
	case r.err != nil:
		d.Task.State = "failed"
		d.Task.Error = Err("internal_error")
	case r.out.ExitCode != 0:
		d.Task.State = "failed"
		d.Task.Error = exitError(r.out, nil)
		if d.Task.Error == nil {
			d.Task.Error = Err("cli_error")
		}
	default:
		var result DownloadResult
		cr, e := inferyard.Parse(r.out.Stdout)
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
			d.Task.State = "failed"
			d.Task.Error = Err("internal_error")
		} else {
			d.Task.State = "done"
			d.Task.Result = &result
			d.Task.DownloadedBytes = result.Bytes
		}
	}
	s.Bus.Publish("download.state", map[string]any{"task": d.Task})
}
