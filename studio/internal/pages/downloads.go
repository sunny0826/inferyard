package pages

import (
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) downloadsPageView(c *ui.Context) {
	if !s.assetsPreset {
		s.assetsPreset = true
		if s.modelURL == "" {
			s.modelURL = core.DefaultModelURL()
		}
		if s.engineURL == "" {
			s.engineURL = core.DefaultEngineURL()
		}
	}
	s.header(c, "下载")
	s.downloadTaskGuide(c)
	if len(s.downloads) > 0 {
		card(c, "下载任务", func() { s.downloadsView(c) })
	}
	s.newDownloadsView(c)
	s.catalogView(c)
}

func (s *viewState) newDownloadsView(c *ui.Context) {
	card(c, "新建下载任务", func() {
		ui.Text(c, "推荐资源 · "+core.PlatformLabel(core.CurrentPlatform())).TextColor(c.Theme().TextMuted)
		s.starterDownloadAction(c)
		s.engineDownloadView(c)
		s.modelAssetsView(c)
		button(c, "设置下载目录名").AlignSelf(ui.Start).OnClick(func() { s.selected = "settings" })
	})
}

func (s *viewState) downloadPrimaryKind() string {
	assets := s.availableTaskAssets()
	if !assets.engine && !s.downloadActive("engine") {
		return "engine"
	}
	if !assets.model && !s.downloadActive("model") {
		return "model"
	}
	return ""
}

func (s *viewState) downloadActive(kind string) bool {
	for _, task := range s.downloads {
		if task.Kind == kind && task.State == "downloading" {
			return true
		}
	}
	return false
}

func (s *viewState) downloadAction(c *ui.Context, kind, label string) ui.Element {
	if s.downloadPrimaryKind() == kind {
		return primaryButton(c, label)
	}
	return button(c, label)
}

func (s *viewState) modelAssetsView(c *ui.Context) {
	heading(c, "模型", 3)
	s.catalogChoices(c, core.CatalogModel, core.CurrentPlatform(), "自定义模型地址", func() {
		field(c, "模型 URL（支持 HF / ModelScope /resolve 或 /blob）", &s.modelURL).Description("支持 HuggingFace 或 ModelScope 的 /resolve、/blob 或 /file/view 地址，未固定 revision 时将自动解析。").Error(customModelURLError(s.modelURL))
	})
	s.modelDownloadForm(c)
}

func (s *viewState) modelDownloadForm(c *ui.Context) {
	s.modelMirrorSelector(c)
	catalogDetails(c, s.modelURL)
	componentFold(c, "需要凭据时", func() {
		field(c, "凭据环境变量名（可选）", &s.tokenEnv).Description("仅填写变量名称，不填写凭据值。").Error(tokenNameError(s.tokenEnv))
	})
	s.modelResolvePanel(c)
}

func (s *viewState) acquireModel() {
	source, env := strings.TrimSpace(s.modelURL), s.tokenEnv
	if source == "" || customModelURLError(source) != "" {
		return
	}
	if env != "" && !environmentName.MatchString(env) {
		return
	}
	if s.simulate(previewDownload{kind: "model", source: source}) {
		return
	}
	s.work(func() (string, *core.ApiError) {
		_, e := s.service.CreateOrResolveModelDownload(s.ctx, core.CreateDownloadRequest{SourceUrl: source}, env)
		return "已加入下载队列", e
	})
}

func (s *viewState) engineDownloadView(c *ui.Context) {
	heading(c, "引擎", 3)
	s.catalogChoices(c, core.CatalogEngine, core.CurrentPlatform(), "自定义引擎地址", func() {
		field(c, "引擎 URL", &s.engineURL).Description("必填；引擎归档下载地址。").Error(downloadURLError(s.engineURL))
	})
	catalogDetails(c, s.engineURL)
	s.downloadAction(c, "engine", "下载引擎").AlignSelf(ui.Start).Disabled(s.busy || strings.TrimSpace(s.engineURL) == "").OnClick(func() {
		s.acquireEngine(strings.TrimSpace(s.engineURL), "")
	})
}

func (s *viewState) acquireEngine(source, name string) {
	if downloadURLError(source) != "" {
		return
	}
	if s.simulate(previewDownload{kind: "engine", source: source, name: name}) {
		return
	}
	s.work(func() (string, *core.ApiError) {
		_, e := s.service.CreateEngineDownload(source, name)
		return "已加入下载队列", e
	})
}

func downloadURLError(value string) string {
	if value == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "请输入完整的 https URL。"
	}
	return ""
}
