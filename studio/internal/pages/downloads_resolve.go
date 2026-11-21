package pages

import (
	"context"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type mirrorSourceState struct {
	choice string
}

type modelResolveOutcome struct {
	source   string
	resolved *inferyard.ModelResolve
	errText  string
}

type modelResolveMailbox struct {
	mu      sync.Mutex
	pending *modelResolveOutcome
}

type modelResolveState struct {
	source    string
	resolving bool
	resolved  *inferyard.ModelResolve
	errText   string
	mailbox   *modelResolveMailbox
}

func customModelURLError(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if err := downloadURLError(trimmed); err != "" {
		return err
	}
	if !core.IsResolvableModelURL(trimmed) {
		return "请输入 HuggingFace 或 ModelScope 的 .gguf 或 /tree 目录地址（支持 /blob、/resolve、/tree）。"
	}
	return ""
}

func (s *viewState) starterDownloadAction(c *ui.Context) {
	assets := s.availableTaskAssets()
	engineURL := core.DefaultEngineURL()
	modelURL := core.DefaultModelURL()
	if assets.engine || assets.model || s.downloadActive("engine") || s.downloadActive("model") || engineURL == "" || modelURL == "" {
		return
	}
	if button(c, "一键下载推荐引擎与模型").AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
		s.acquireStarterBundle(engineURL, modelURL)
	}
}

func (s *viewState) acquireStarterBundle(engineURL, modelURL string) {
	if s.preview != nil {
		s.simulate(previewDownload{kind: "engine", source: engineURL})
		s.simulate(previewDownload{kind: "model", source: modelURL})
		return
	}
	s.work(func() (string, *core.ApiError) {
		if _, e := s.service.CreateEngineDownload(engineURL, ""); e != nil {
			return "", e
		}
		if _, e := s.service.CreateOrResolveModelDownload(s.ctx, core.CreateDownloadRequest{SourceUrl: modelURL}, s.tokenEnv); e != nil {
			return "", e
		}
		return "已将推荐引擎与模型加入下载队列", nil
	})
}

func (s *viewState) modelMirrorSelector(c *ui.Context) {
	trimmed := strings.TrimSpace(s.modelURL)
	entry, ok := core.CatalogEntryByURL(trimmed)
	if !ok || entry.Kind != core.CatalogModel || entry.MirrorURL == "" {
		return
	}
	label := entry.MirrorLabel
	if label == "" {
		label = "镜像源"
	}
	host := ui.Column(c.Key("model-mirror-" + entry.ID)).FillWidth().Gap(tokens(c).Density.Tight)
	st := ui.Local(host, "mirror-source", func() mirrorSourceState {
		return mirrorSourceState{choice: "primary"}
	})
	if trimmed == entry.MirrorURL {
		st.choice = "mirror"
	} else {
		st.choice = "primary"
	}
	host.Children(func() {
		before := st.choice
		options := []components.NativeSelectOption[string]{
			{Label: "HuggingFace", Value: "primary"},
			{Label: label, Value: "mirror"},
		}
		segmentedField(c, "下载源", "国内网络可切换为 "+label+" 镜像，下载前自动解析为固定版本。", &st.choice, options)
		if st.choice != before {
			if st.choice == "mirror" {
				s.modelURL = entry.MirrorURL
			} else {
				s.modelURL = entry.URL
			}
		}
	})
}

func (s *viewState) modelResolvePanel(c *ui.Context) {
	trimmed := strings.TrimSpace(s.modelURL)
	t := tokens(c)
	host := ui.Column(c.Key("model-resolve-panel")).FillWidth().Gap(t.Density.Tight)
	st := ui.Local(host, "model-resolve", func() modelResolveState {
		return modelResolveState{mailbox: &modelResolveMailbox{}}
	})
	st.mailbox.mu.Lock()
	pending := st.mailbox.pending
	st.mailbox.pending = nil
	st.mailbox.mu.Unlock()
	if pending != nil && pending.source == trimmed {
		st.resolving = false
		st.resolved = pending.resolved
		st.errText = pending.errText
	}
	if st.source != trimmed {
		st.source = trimmed
		st.resolving = false
		st.resolved = nil
		st.errText = ""
	}
	canResolve := !s.busy && !st.resolving && trimmed != "" && core.IsResolvableModelURL(trimmed) && tokenNameError(s.tokenEnv) == ""
	host.Children(func() {
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if s.downloadAction(c, "model", "下载模型").Disabled(s.busy || trimmed == "").Clicked() {
				s.acquireModel()
			}
			if button(c, "解析地址").Disabled(!canResolve).Clicked() {
				s.triggerModelResolve(st, trimmed)
			}
		})
		switch {
		case st.resolving:
			ui.Text(c, "正在解析远端模型版本…").FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
		case st.errText != "":
			ui.Text(c, st.errText).FontSize(t.Type.Caption).TextColor(stateColor(c, stateDanger))
		case st.resolved != nil:
			sizeText := "大小未知"
			if st.resolved.Bytes != nil && *st.resolved.Bytes > 0 {
				sizeText = catalogSize(*st.resolved.Bytes)
			}
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Hairline).Padding(t.Density.Inset, t.Density.Stack), materialInset).Children(func() {
				ui.Text(c, "已解析版本："+st.resolved.Revision).Selectable().FontSize(t.Type.Caption)
				ui.Text(c, "文件："+st.resolved.Filename+" · "+sizeText).FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
				ui.Text(c, "固定地址："+st.resolved.PinnedURL).Selectable().FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
			})
		}
	})
}

func (s *viewState) triggerModelResolve(st *modelResolveState, source string) {
	if s.preview != nil {
		res := previewResolveModelURL(source)
		st.resolving = false
		st.resolved = &res
		st.errText = ""
		return
	}
	if s.service == nil {
		st.errText = "后端服务尚未就绪，无法解析地址。"
		return
	}
	st.resolving = true
	st.resolved = nil
	st.errText = ""
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	tokenEnv := s.tokenEnv
	mailbox := st.mailbox
	go func() {
		res, apiErr := s.service.ResolveModelURL(ctx, source, tokenEnv)
		outcome := &modelResolveOutcome{source: source}
		if apiErr != nil {
			msg := apiErr.Message
			if msg == "" {
				msg = human(apiErr.Code)
			}
			if cause, ok := apiErr.Details["failure_cause"].(string); ok && cause != "" {
				msg += "：" + human(cause)
			}
			outcome.errText = msg
		} else {
			outcome.resolved = &res
		}
		mailbox.mu.Lock()
		mailbox.pending = outcome
		mailbox.mu.Unlock()
		if s.win != nil {
			s.win.Update(func() {})
		}
	}()
}

func previewResolveModelURL(raw string) inferyard.ModelResolve {
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
