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
	"inferyard/studio/internal/l10n"
)

func previewTaskInRoot(outRoot string, task core.DownloadTask) bool {
	if outRoot == "" || task.OutDir == "" {
		return true
	}
	if !filepath.IsAbs(task.OutDir) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(outRoot), filepath.Clean(task.OutDir))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (p *previewSession) startDownload(op previewDownload) {
	s := p.state
	outRoot := emptyText(s.settings.Effective.ModelsRoot)
	if op.kind == "engine" {
		if ws := emptyText(s.settings.Effective.WorkspaceRoot); ws != "" {
			outRoot = filepath.Join(ws, "engines")
		} else {
			outRoot = ""
		}
	}
	if op.name == "" && op.source != "" {
		for _, task := range s.downloads {
			if task.Kind == op.kind && task.State == "downloading" && task.SourceUrl == op.source && previewTaskInRoot(outRoot, task) {
				return
			}
		}
	}
	name := op.name
	if name == "" {
		name = core.SuggestDownloadSlug(op.kind, op.source)
	}
	base := name
	for suffix := 2; ; suffix++ {
		used := false
		for _, task := range s.downloads {
			used = used || (task.OutName == name && previewTaskInRoot(outRoot, task))
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
	task := core.DownloadTask{TaskId: id, Kind: op.kind, State: "downloading", SourceUrl: op.source, OutName: name, OutDir: filepath.Join(outRoot, name), FileName: previewValue(filename), StartedAt: p.timestamp()}
	s.downloads = append([]core.DownloadTask{task}, s.downloads...)
	p.tickDownload(id, p.takeFailure(), 0)
	p.noticeKey = "preview.notice.download_started"
}

func (p *previewSession) startPolystrataSetup(req core.PolystrataSetupRequest) {
	s := p.state
	for _, task := range s.downloads {
		if task.Kind == "polystrata" && task.State == "downloading" {
			return
		}
	}
	id := p.nextID("polystrata")
	parts := []string{"PolyStrata"}
	if strings.TrimSpace(req.Family) != "" {
		parts = append(parts, strings.TrimSpace(req.Family))
	}
	if strings.TrimSpace(req.Model) != "" {
		parts = append(parts, strings.TrimSpace(req.Model))
	}
	outName := "polystrata-" + id
	outDir := filepath.Join(emptyText(s.settings.Effective.WorkspaceRoot), "engines", outName)
	task := core.DownloadTask{
		TaskId:      id,
		Kind:        "polystrata",
		DisplayName: strings.Join(parts, " · "),
		State:       "downloading",
		Stage:       "preflight",
		Step:        previewValue(1),
		StepTotal:   previewValue(4),
		Percent:     previewValue(10.0),
		Detail:      previewValue(l10n.TR("preview.polystrata.stage.preflight")),
		ModelSource: strings.TrimSpace(req.ModelSource),
		SourceUrl:   "https://github.com/simplexai-labs/PolyStrata/archive/3e1642161198c50ff1abed4046f4e4f476099dbb.tar.gz",
		OutName:     outName,
		OutDir:      outDir,
		LogPath:     previewValue(filepath.Join(outDir, ".inferyard-setup.log")),
		StartedAt:   p.timestamp(),
	}
	s.downloads = append([]core.DownloadTask{task}, s.downloads...)
	if p.polystrataReqs == nil {
		p.polystrataReqs = map[string]core.PolystrataSetupRequest{}
	}
	p.polystrataReqs[id] = req
	p.tickPolystrataSetup(id, p.takeFailure(), 0)
	p.noticeKey = "preview.notice.polystrata_started"
}

func (p *previewSession) tickPolystrataSetup(id string, fail bool, tick int) {
	p.after("download/"+id, 500*time.Millisecond, func() {
		s := p.state
		for i := range s.downloads {
			task := &s.downloads[i]
			if task.TaskId != id || task.State != "downloading" {
				continue
			}
			if fail && tick == 1 {
				apiErr := core.Err("polystrata_preflight_blocked")
				apiErr.Details = map[string]any{
					"blockers": []map[string]any{{
						"code":        "xcode_clt_missing",
						"message":     l10n.TR("preview.polystrata.blocker.xcode_clt"),
						"fix_command": "xcode-select --install",
					}},
				}
				task.State, task.Error, task.FinishedAt = "failed", apiErr, previewValue(p.timestamp())
				return
			}
			switch tick {
			case 0:
				task.Stage, task.Step, task.Percent = "downloading_runtime", previewValue(2), previewValue(35.0)
				task.Detail = previewValue("PolyStrata v0.1.45-poly.10")
			case 1:
				task.Stage, task.Step, task.Percent = "setup_engine", previewValue(3), previewValue(65.0)
				task.Detail = previewValue(l10n.TR("preview.polystrata.stage.build_metal"))
			case 2:
				task.Stage, task.Step, task.Percent = "downloading_model", previewValue(4), previewValue(90.0)
				task.Detail = previewValue(l10n.TR("preview.polystrata.stage.download_model"))
			default:
				instPath := filepath.Join(task.OutDir, "installation.json")
				cfgPath := filepath.Join(task.OutDir, "strata-preview.json")
				task.State, task.Stage, task.Percent, task.FinishedAt = "done", "", previewValue(100.0), previewValue(p.timestamp())
				task.EnginePath = previewValue(instPath)
				task.InstallationPath = previewValue(instPath)
				task.ModelConfigPath = previewValue(cfgPath)
				p.installDownloadedAsset(task)
				return
			}
			p.tickPolystrataSetup(id, fail, tick+1)
			return
		}
	})
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
	outRoot := emptyText(s.settings.Effective.ModelsRoot)
	if task.Kind == "engine" || task.Kind == "polystrata" {
		if ws := emptyText(s.settings.Effective.WorkspaceRoot); ws != "" {
			outRoot = filepath.Join(ws, "engines")
		} else {
			outRoot = ""
		}
	}
	if !previewTaskInRoot(outRoot, *task) {
		return
	}
	if task.Kind == "polystrata" {
		engName := "PolyStrata · v0.1.45-poly.10"
		instPath := emptyText(task.InstallationPath)
		existing := false
		guidedSource := l10n.TR("preview.source.guided_install")
		for _, eng := range s.registry.Engines {
			if eng.Family == "polystrata" && (eng.Path == instPath || eng.Source == guidedSource) {
				engName = eng.Name
				existing = true
				break
			}
		}
		if !existing {
			s.registry.Engines = append(s.registry.Engines, core.EngineAsset{
				Name: engName, Family: "polystrata", Kind: "local_installation", Source: guidedSource,
				Path: instPath, Deletable: true,
			})
		}
		s.registry.DefaultEngine = engName
		if s.checks.Engines == nil {
			s.checks.Engines = map[string]core.EngineEligibility{}
		}
		s.checks.Engines[engName] = core.EngineEligibility{State: core.EngineCheckEligible, Family: "polystrata", VerificationScope: "installation", Version: "v0.1.45-poly.10"}
		cfgPath := emptyText(task.ModelConfigPath)
		modelExists := false
		for _, m := range s.registry.Models {
			if cfgPath != "" && m.Path == cfgPath {
				modelExists = true
				break
			}
		}
		if !modelExists {
			modelName := strings.TrimSpace(strings.TrimPrefix(task.DisplayName, "PolyStrata · "))
			if modelName == "" || modelName == task.DisplayName {
				modelName = "Qwen3.6-35B-A3B (preview)"
			}
			s.registry.Models = append(s.registry.Models, core.ModelAsset{
				Name:      modelName,
				Format:    "strata_pack",
				SizeBytes: previewValue(int64(22663387424)),
				Path:      cfgPath,
				Available: true,
				Deletable: true,
			})
		}
		s.adoptDownloadedEngine()
		s.adoptOnlyModel()
		return
	}
	if task.Kind == "engine" {
		name := task.OutName + " · llama-server"
		task.EnginePath = previewValue(filepath.Join(task.OutDir, "llama-server"))
		for _, asset := range s.registry.Engines {
			if asset.Name == name {
				return
			}
		}
		s.registry.Engines = append(s.registry.Engines, core.EngineAsset{Name: name, Family: "llama-cpp", Source: l10n.TR("preview.source.download")})
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

func (p *previewSession) tickPolystrataModelPreview(id string) {
	p.after("download/"+id, time.Second, func() {
		s := p.state
		for i := range s.downloads {
			task := &s.downloads[i]
			if task.TaskId != id || task.State != "downloading" {
				continue
			}
			delta := int64(22502441)
			if task.TotalBytes != nil && *task.TotalBytes > 0 {
				if task.DownloadedBytes+delta >= *task.TotalBytes {
					task.DownloadedBytes = 4456028569
				} else {
					task.DownloadedBytes += delta
				}
			}
			p.tickPolystrataModelPreview(id)
			return
		}
	})
}

func (p *previewSession) downloadLogTail(taskID string) string {
	if taskID == "preview-polystrata-model-live" {
		return strings.Join([]string{
			l10n.TR("preview.polystrata.log.line1"),
			l10n.TR("preview.polystrata.log.line2"),
			l10n.TR("preview.polystrata.log.line3"),
			l10n.TR("preview.polystrata.log.line4"),
			l10n.TR("preview.polystrata.log.line5"),
			l10n.TR("preview.polystrata.log.line6"),
			l10n.TR("preview.polystrata.log.line7"),
			"07:36:35 [download] Qwen3.8-Flash-Next-GSQ-RCO-Q2_0-00001-of-00002.gguf 4.15 / 35.04 GiB (21.46 MiB/s)",
		}, "\n")
	}
	return "[preview] setup_engine: building strata runtime\n[preview] downloading_model: waiting for weights"
}

func previewPolystrataRequestFromTask(task core.DownloadTask, fallback core.PolystrataSetupRequest) core.PolystrataSetupRequest {
	modelSource := strings.TrimSpace(task.ModelSource)
	if modelSource == "" {
		modelSource = fallback.ModelSource
	}
	parts := strings.Split(task.DisplayName, " · ")
	if len(parts) >= 2 && strings.TrimSpace(parts[0]) == "PolyStrata" {
		req := core.PolystrataSetupRequest{
			Family:      strings.TrimSpace(parts[1]),
			ModelSource: modelSource,
		}
		if len(parts) >= 3 {
			req.Model = strings.TrimSpace(parts[2])
		}
		return req
	}
	if modelSource != "" {
		fallback.ModelSource = modelSource
	}
	return fallback
}

func (p *previewSession) cancelDownload(id string) {
	for i := range p.state.downloads {
		task := &p.state.downloads[i]
		if task.TaskId == id && task.State == "downloading" {
			p.cancelEvents("download/" + id)
			task.State, task.FinishedAt = "cancelled", previewValue(p.timestamp())
			p.noticeKey = "preview.notice.download_cancelled"
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
