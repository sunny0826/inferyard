package pages

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

type reportContent struct {
	id           string
	web          *mygo.WebView
	failure      *core.ApiError
	retry, close func()
}

func (r *reportContent) view(c *ui.Context) {
	if r.failure != nil {
		ui.Column(c).Padding(c.Theme().Space(6)).Gap(c.Theme().Space(3)).Children(func() {
			heading(c, "报告 "+r.id, 1)
			errorView(c, r.failure)
			button(c, "重新加载报告").AlignSelf(ui.Start).OnClick(r.retry)
			button(c, "关闭报告窗口").AlignSelf(ui.Start).OnClick(r.close)
		})
		return
	}
	if r.web != nil {
		ui.WebView(c, r.web).Fill().Label("离线报告 " + r.id)
		return
	}
	ui.Column(c).Padding(c.Theme().Space(6)).Gap(c.Theme().Space(3)).Children(func() {
		heading(c, "报告 "+r.id, 1)
		ui.Text(c, "正在打开本地报告…")
		progressBar(c, -1, "本地报告加载进度")
	})
}

func reportLoadError() *core.ApiError {
	return &core.ApiError{Code: "report_open_failed", Message: "报告窗口未能加载本地 HTML，请重新加载或关闭窗口后重试。"}
}

func (r *Reports) loadReport(id string, load func(string) error) *core.ApiError {
	path, e := r.service.ReportFile(id + "/report.html")
	if e != nil {
		return e
	}
	if load(reportURL(path)) != nil {
		return reportLoadError()
	}
	return nil
}
