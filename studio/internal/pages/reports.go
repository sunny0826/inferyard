package pages

import (
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"net/url"
	"path/filepath"
)

// Reports owns one native WebView window per report and never serves HTTP.
type reportWindow struct {
	win  *mygo.Window
	view *mygo.WebView
}

type Reports struct {
	service   *core.Service
	windows   map[string]reportWindow
	onFailure func(*core.ApiError)
}

func NewReports(service *core.Service) *Reports {
	return &Reports{service: service, windows: map[string]reportWindow{}}
}
func reportURL(path string) string {
	value := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		value = "/" + value
	}
	return (&url.URL{Scheme: "file", Path: value}).String()
}

// Open must be called on the UI thread, like mygo.NewWindow.
func (r *Reports) Open(id string) (*mygo.Window, *core.ApiError) {
	path, e := r.service.ReportFile(id + "/report.html")
	if e != nil {
		return nil, e
	}
	if existing, ok := r.windows[path]; ok && !existing.win.IsDestroyed() {
		existing.win.Focus()
		return existing.win, nil
	}
	content := &reportContent{id: id}
	w := mygo.NewWindow(mygo.WindowOptions{Title: "InferYard · 报告 " + id, Width: 1120, Height: 800, Content: ui.View(content.view)})
	v, err := w.NewWebView(mygo.WebViewOptions{})
	if err != nil {
		w.Close()
		return nil, reportLoadError()
	}
	content.web = v
	v.Page().OnPageTitleUpdated(func(e *mygo.TitleEvent) { e.PreventDefault() })
	fail := func(e *core.ApiError) {
		if w.IsDestroyed() {
			return
		}
		w.Update(func() { content.failure = e })
		if r.onFailure != nil {
			r.onFailure(e)
		}
	}
	v.Page().OnDidFailLoad(func(_ *mygo.LoadError) { fail(reportLoadError()) })
	v.Page().OnRenderProcessGone(func(_ string) { fail(reportLoadError()) })
	content.retry = func() {
		content.failure = nil
		if e := r.loadReport(id, v.Page().LoadURL); e != nil {
			fail(e)
		}
	}
	content.close = w.Close
	r.windows[path] = reportWindow{win: w, view: v}
	w.OnClosed(func() { delete(r.windows, path) })
	if e := r.loadReport(id, v.Page().LoadURL); e != nil {
		w.Close()
		return nil, e
	}
	w.Update(func() {})
	return w, nil
}
func (s *viewState) reportsView(c *ui.Context) {
	s.header(c, "报告")
	s.reportGuide(c)
	card(c, "生成报告", func() {
		errorView(c, s.historyError)
		ui.Text(c, "选择来源 run（1–100 个）")
		runs := s.historyRuns()
		if s.historyError != nil {
			button(c, "重试读取来源").AlignSelf(ui.Start).OnClick(s.refreshAction)
		} else if !s.loaded {
			ui.Text(c, "正在读取报告来源…")
		} else if len(runs) == 0 {
			ui.Text(c, "暂无运行记录，请先完成测评，再选择来源。")
			button(c, "到运行页启动测评").AlignSelf(ui.Start).OnClick(func() { s.selected = "runs" })
		}
		known := map[string]bool{}
		for _, r := range runs {
			known[r.Path] = true
		}
		for path := range s.reportChoices.All() {
			if !known[path] {
				s.reportChoices.Remove(path)
			}
		}
		s.reportSources.Label = func(i int) string { return runs[i].RunId + " · " + runs[i].Path }
		c.Key("report-sources")
		ui.List(c, &s.reportSources, len(runs)).Label("报告来源运行列表").Height(220).
			ItemKey(func(i int) any { return runs[i].Path }).
			Selection(&s.reportChoices).Disabled(s.busy).Rows(func(row ui.ListRow) {
			ui.Column(row.Context).Padding(6).Gap(2).Children(func() {
				ui.Text(row.Context, runs[row.Index].RunId)
				ui.Text(row.Context, runs[row.Index].Path).FontSize(max(12, c.Theme().Rem(.85))).MaxLines(2).Description(runs[row.Index].Path)
			})
		})
		count := s.reportChoices.Len()
		if count < 1 || count > 100 {
			ui.Text(c, "请选择 1–100 个来源 run。")
		}
		ui.Text(c, fmt.Sprintf("已选 %d / 100", count))
		if primaryButton(c, "生成报告").AlignSelf(ui.Start).Disabled(s.busy || count < 1 || count > 100).Clicked() {
			paths := []string{}
			for _, r := range runs {
				if s.reportChoices.Has(r.Path) {
					paths = append(paths, r.Path)
				}
			}
			s.work(func() (string, *core.ApiError) {
				report, e := s.service.CreateReport(core.CreateReportRequest{RunPaths: paths})
				return "已生成 " + report.ReportId, e
			})
		}
	})
	card(c, "报告列表", func() { s.reportsTableView(c) })
}
