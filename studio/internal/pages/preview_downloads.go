//go:build ui_debug && !release

package pages

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func (p *previewSession) startDownload(op previewDownload) {
	s := p.state
	if op.name == "" && op.source != "" {
		for _, task := range s.downloads {
			if task.Kind == op.kind && task.State == "downloading" && task.SourceUrl == op.source {
				return
			}
		}
	}
	name := op.name
	if name == "" {
		name = defaultDownloadName(s.settings.Settings.ModelDownloadName, "model")
		if op.kind == "engine" {
			name = defaultDownloadName(s.settings.Settings.EngineDownloadName, "engine")
		}
	}
	base := name
	for suffix := 2; ; suffix++ {
		used := false
		for _, task := range s.downloads {
			used = used || task.OutName == name
		}
		if !used {
			break
		}
		name = fmt.Sprintf("%s-%d", base, suffix)
	}
	id := p.nextID("download")
	u, _ := url.Parse(op.source)
	filename := path.Base(u.Path)
	if op.kind == "engine" {
		filename = "llama-server"
	}
	task := core.DownloadTask{TaskId: id, Kind: op.kind, State: "downloading", SourceUrl: op.source, OutName: name, OutDir: filepath.Join(s.models, name), FileName: previewValue(filename), StartedAt: p.timestamp()}
	s.downloads = append([]core.DownloadTask{task}, s.downloads...)
	p.tickDownload(id, p.takeFailure(), 0)
	p.notice = "模拟下载已开始。"
}

func (p *previewSession) tickDownload(id string, fail bool, tick int) {
	p.after("download/"+id, 600*time.Millisecond, func() {
		s := p.state
		for i := range s.downloads {
			task := &s.downloads[i]
			if task.TaskId != id || task.State != "downloading" {
				continue
			}
			if fail && tick == 1 {
				task.State, task.Error, task.FinishedAt = "failed", core.Err("download_failed"), previewValue(p.timestamp())
				return
			}
			if task.Kind == "model" {
				task.DownloadedBytes += 4 << 20
				task.BytesPerSecond = previewValue(float64(4<<20) / .6)
			}
			if tick < 3 {
				p.tickDownload(id, fail, tick+1)
				return
			}
			task.State, task.FinishedAt = "done", previewValue(p.timestamp())
			p.installDownloadedAsset(task)
			return
		}
	})
}

func (p *previewSession) installDownloadedAsset(task *core.DownloadTask) {
	s := p.state
	if task.Kind == "engine" {
		name := task.OutName + " · llama-server"
		task.EnginePath = previewValue(filepath.Join(task.OutDir, "llama-server"))
		for _, asset := range s.registry.Engines {
			if asset.Name == name {
				return
			}
		}
		s.registry.Engines = append(s.registry.Engines, core.EngineAsset{Name: name, Family: "llama-cpp", Source: "模拟下载"})
		s.registry.DefaultEngine = name
		if s.checks.Engines == nil {
			s.checks.Engines = map[string]core.EngineEligibility{}
		}
		s.checks.Engines[name] = core.EngineEligibility{State: core.EngineCheckEligible, Version: "preview"}
		s.adoptDownloadedEngine()
	} else {
		name := task.OutName + " · " + emptyText(task.FileName)
		assetPath := filepath.Join(task.OutDir, emptyText(task.FileName))
		format := "gguf"
		if strings.Contains(task.SourceUrl, "/tree/") {
			assetPath = task.OutDir
			format = "directory"
		}
		task.Result = &core.DownloadResult{Bytes: task.DownloadedBytes, Sha256: strings.Repeat("0", 64)}
		s.registry.Models = append(s.registry.Models, core.ModelAsset{
			Name:      name,
			Format:    format,
			Path:      assetPath,
			Available: true,
			SizeBytes: previewValue(task.DownloadedBytes),
		})
		s.adoptOnlyModel()
	}
}

func (p *previewSession) cancelDownload(id string) {
	for i := range p.state.downloads {
		task := &p.state.downloads[i]
		if task.TaskId == id && task.State == "downloading" {
			p.cancelEvents("download/" + id)
			task.State, task.FinishedAt = "cancelled", previewValue(p.timestamp())
			p.notice = "模拟下载已取消。"
			return
		}
	}
}

func (p *previewSession) resolveModelURL(raw string) inferyard.ModelResolve {
	u, _ := url.Parse(raw)
	platform := "huggingface"
	host := "huggingface.co"
	if u != nil && strings.ToLower(u.Host) == "modelscope.cn" {
		platform = "modelscope"
		host = "modelscope.cn"
	}
	filename := "model.gguf"
	if u != nil && u.Path != "" {
		if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
			filename = base
		}
	}
	rev := "0123456789abcdef0123456789abcdef01234567"
	var size *int64
	alreadyPinned := false
	repo := "preview/model"
	if entry, ok := core.CatalogEntryByURL(raw); ok {
		if entry.Revision != "" {
			rev = entry.Revision
		}
		if entry.Bytes > 0 {
			b := entry.Bytes
			size = &b
		}
		alreadyPinned = raw == entry.URL
	}
	if u != nil {
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if host == "modelscope.cn" && len(parts) >= 4 && parts[0] == "models" {
			repo = parts[1] + "/" + parts[2]
		} else if len(parts) >= 2 {
			repo = parts[0] + "/" + parts[1]
		}
	}
	if strings.Contains(raw, "/tree/") {
		repoName := path.Base(strings.TrimSuffix(repo, "/"))
		pinned := "https://" + host + "/" + repo + "/tree/" + rev
		return inferyard.ModelResolve{
			Definition:    inferyard.ModelResolveDefinition,
			Kind:          "directory",
			Platform:      platform,
			Repository:    repo,
			Revision:      rev,
			PinnedURL:     pinned,
			DownloadURL:   pinned,
			Filename:      repoName,
			Bytes:         size,
			AlreadyPinned: alreadyPinned,
		}
	}
	pinned := "https://" + host + "/" + repo + "/blob/" + rev + "/" + filename
	download := "https://" + host + "/" + repo + "/resolve/" + rev + "/" + filename
	if platform == "modelscope" {
		download = "https://modelscope.cn/api/v1/models/" + repo + "/repo?Revision=" + rev + "&FilePath=" + url.QueryEscape(filename)
	}
	return inferyard.ModelResolve{
		Definition:    inferyard.ModelResolveDefinition,
		Platform:      platform,
		Repository:    repo,
		Revision:      rev,
		Path:          filename,
		PinnedURL:     pinned,
		DownloadURL:   download,
		Filename:      filename,
		Bytes:         size,
		AlreadyPinned: alreadyPinned,
	}
}
