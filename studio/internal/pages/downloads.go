package pages

import (
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// downloadsPageView is the S8 downloads page: download task cards (with the
// failed-task "create again" entry) and the new-download forms moved off the
// models page.
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
	if s.preparingTask() {
		s.newDownloadsView(c)
	}
	if len(s.downloads) > 0 {
		card(c, "下载任务", func() { s.downloadsView(c) })
	}
	if !s.preparingTask() {
		s.newDownloadsView(c)
	}
	s.catalogView(c)
}

func (s *viewState) newDownloadsView(c *ui.Context) {
	card(c, "新建下载任务", func() {
		s.engineDownloadView(c)
		s.modelAssetsView(c)
		ui.Text(c, "目录名取自设置，占用自动加序号；实际目录名见任务详情。")
		button(c, "打开设置").AlignSelf(ui.Start).OnClick(func() { s.selected = "settings" })
	})
}

func (s *viewState) modelAssetsView(c *ui.Context) {
	heading(c, "模型 · "+s.modelAssetTitle(), 3)
	s.modelDownloadForm(c)
}

// modelAssetTitle names the model the download form is currently pointed at.
// A hand-edited URL matches no catalogue entry, so it has no title.
func (s *viewState) modelAssetTitle() string {
	if entry, ok := core.CatalogEntryByURL(s.modelURL); ok {
		return entry.Title
	}
	return "自定义地址"
}

func (s *viewState) modelDownloadForm(c *ui.Context) {
	field(c, "模型 URL（固定 40 位 revision 的 GGUF）", &s.modelURL).Description("必填；使用固定 revision 的 GGUF 下载地址。").Error(downloadURLError(s.modelURL))
	componentFold(c, "需要凭据时", func() {
		field(c, "凭据环境变量名（可选）", &s.tokenEnv).Description("仅填写变量名称，不填写凭据值。").Error(tokenNameError(s.tokenEnv))
		ui.Text(c, "凭据由启动 Studio 的环境提供。")
	})
	if button(c, "下载模型").AlignSelf(ui.Start).Disabled(s.busy || strings.TrimSpace(s.modelURL) == "").Clicked() {
		s.acquireModel()
	}
}

func (s *viewState) acquireModel() {
	source, env := strings.TrimSpace(s.modelURL), s.tokenEnv
	if message := downloadURLError(source); message != "" {
		s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
		return
	}
	if env != "" && !environmentName.MatchString(env) {
		s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: "只接受环境变量名，请勿输入明文 token。"})
		return
	}
	if s.simulate(previewDownload{kind: "model", source: source}) {
		return
	}
	s.work(func() (string, *core.ApiError) {
		_, e := s.service.CreateModelDownload(core.CreateDownloadRequest{SourceUrl: source}, env)
		return "已加入下载队列。", e
	})
}

func (s *viewState) engineDownloadView(c *ui.Context) {
	heading(c, "引擎 · "+s.engineAssetTitle(), 3)
	field(c, "引擎 URL", &s.engineURL).Description("必填；引擎归档下载地址。").Error(downloadURLError(s.engineURL))
	primaryButton(c, "下载引擎").AlignSelf(ui.Start).Disabled(s.busy || strings.TrimSpace(s.engineURL) == "").OnClick(func() {
		s.acquireEngine(strings.TrimSpace(s.engineURL), "")
	})
}

// engineAssetTitle names the engine the download form is currently pointed
// at. A hand-edited URL matches no catalogue entry, so it has no title.
func (s *viewState) engineAssetTitle() string {
	if entry, ok := core.CatalogEntryByURL(s.engineURL); ok {
		return entry.Title
	}
	return "自定义地址"
}

func (s *viewState) acquireEngine(source, name string) {
	if message := downloadURLError(source); message != "" {
		s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
		return
	}
	if s.simulate(previewDownload{kind: "engine", source: source, name: name}) {
		return
	}
	s.work(func() (string, *core.ApiError) {
		_, e := s.service.CreateEngineDownload(source, name)
		return "引擎已加入下载队列。", e
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
