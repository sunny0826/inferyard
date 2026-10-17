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
)

func (p *previewSession) startDownload(op previewDownload) {
	s := p.state
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
	s.downloads = append(s.downloads, task)
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
		s.registry.Engines = append(s.registry.Engines, core.EngineAsset{Name: name, Source: "模拟下载"})
		s.registry.DefaultEngine = name
		if s.checks.Engines == nil {
			s.checks.Engines = map[string]core.EngineEligibility{}
		}
		s.checks.Engines[name] = core.EngineEligibility{State: core.EngineCheckEligible, Version: "preview"}
		s.adoptDownloadedEngine()
	} else {
		name := task.OutName + " · " + emptyText(task.FileName)
		task.Result = &core.DownloadResult{Bytes: task.DownloadedBytes, Sha256: strings.Repeat("0", 64)}
		s.registry.Models = append(s.registry.Models, core.ModelAsset{Name: name, Available: true, SizeBytes: previewValue(task.DownloadedBytes)})
		if s.model == "" {
			s.model = name
		}
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
