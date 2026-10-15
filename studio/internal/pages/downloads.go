package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// downloadsPageView is the S8 downloads page: download task cards (with the
// failed-task "create again" entry) and the new-download forms moved off the
// models page.
func (s *viewState) downloadsPageView(c *ui.Context) {
	s.header(c, "下载")
	card(c, "下载任务", func() {
		if len(s.downloads) == 0 {
			ui.Text(c, "尚无下载任务，请在下方选择模型或引擎并创建任务。")
		}
		s.downloadsView(c)
	})
	card(c, "新建下载任务", func() {
		s.modelAssetsView(c)
		componentFold(c, "引擎下载", func() { s.engineDownloadView(c) })
		ui.Text(c, "目录名取自设置，占用自动加序号；实际目录名见任务详情。")
		button(c, "打开设置").AlignSelf(ui.Start).OnClick(func() { s.selected = "settings" })
	})
}

func (s *viewState) modelAssetsView(c *ui.Context) {
	heading(c, "创建模型下载", 3)
	ui.Text(c, "预置测试模型：SmolLM2-135M · Instruct · Q4_K_M · 固定版本 GGUF")
	primaryButton(c, "下载预置模型").AlignSelf(ui.Start).Disabled(s.busy).OnClick(func() {
		s.work(func() (string, *core.ApiError) {
			_, e := s.service.CreateDownload(core.CreateDownloadRequest{SourceUrl: core.DefaultModelURL})
			return "已加入下载队列。", e
		})
	})
	componentFold(c, "自定义模型下载", func() { s.modelDownloadForm(c) })
}

func (s *viewState) modelDownloadForm(c *ui.Context) {
	field(c, "模型 URL（固定 40 位 revision 的 GGUF）", &s.modelURL).Description("必填；使用固定 revision 的 GGUF 下载地址。").Error(urlError(s.modelURL))
	field(c, "凭据环境变量名（可选）", &s.tokenEnv).Description("仅填写变量名称，不填写凭据值。").Error(tokenNameError(s.tokenEnv))
	ui.Text(c, "凭据由启动 Studio 的环境提供。")
	if primaryButton(c, "下载模型").AlignSelf(ui.Start).Disabled(s.busy || strings.TrimSpace(s.modelURL) == "").Clicked() {
		s.acquireModel()
	}
}

func (s *viewState) acquireModel() {
	source, env := strings.TrimSpace(s.modelURL), s.tokenEnv
	if message := urlError(source); message != "" {
		s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
		return
	}
	if env != "" && !environmentName.MatchString(env) {
		s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: "只接受环境变量名，请勿输入明文 token。"})
		return
	}
	s.work(func() (string, *core.ApiError) {
		_, e := s.service.CreateModelDownload(core.CreateDownloadRequest{SourceUrl: source}, env)
		return "已加入下载队列。", e
	})
}

func (s *viewState) engineDownloadView(c *ui.Context) {
	button(c, "下载预置引擎").AlignSelf(ui.Start).Disabled(s.busy || core.DefaultEngineURL() == "").OnClick(func() {
		s.acquireEngine(core.DefaultEngineURL(), "")
	})
	field(c, "引擎 URL", &s.engineURL).Description("必填；引擎归档下载地址。").Error(urlError(s.engineURL))
	button(c, "下载引擎").AlignSelf(ui.Start).Disabled(s.busy || strings.TrimSpace(s.engineURL) == "").OnClick(func() {
		s.acquireEngine(strings.TrimSpace(s.engineURL), "")
	})
}

func (s *viewState) acquireEngine(source, name string) {
	if message := urlError(source); message != "" {
		s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
		return
	}
	s.work(func() (string, *core.ApiError) {
		_, e := s.service.CreateEngineDownload(source, name)
		return "引擎已加入下载队列。", e
	})
}
