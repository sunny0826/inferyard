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
	service *core.Service
	windows map[string]reportWindow
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
	var view *mygo.WebView
	w := mygo.NewWindow(mygo.WindowOptions{Title: "InferYard · 报告 " + id, Width: 1120, Height: 800, Content: ui.View(func(c *ui.Context) {
		if view != nil {
			ui.WebView(c, view).Fill()
		} else {
			ui.Text(c, "正在打开报告…")
		}
	})})
	v, err := w.NewWebView(mygo.WebViewOptions{URL: reportURL(path)})
	if err != nil {
		w.Close()
		return nil, &core.ApiError{Code: "report_open_failed", Message: "报告窗口未能加载本地 HTML。"}
	}
	view = v
	r.windows[path] = reportWindow{win: w, view: v}
	w.OnClosed(func() { delete(r.windows, path) })
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
		if len(runs) == 0 {
			ui.Text(c, "暂无运行记录。")
		}
		count := s.selectedReports()
		for _, run := range runs {
			checked := s.reportSelection[run.Path]
			if ui.Checkbox(c, &checked, run.RunId+" · "+run.Path).Disabled(s.busy || (count >= 100 && !checked)).Changed() {
				s.reportSelection[run.Path] = checked
			}
		}
		count = s.selectedReports()
		ui.Text(c, fmt.Sprintf("已选 %d / 100", count))
		if ui.PrimaryButton(c, "生成报告").Disabled(s.busy || count < 1 || count > 100).Clicked() {
			paths := []string{}
			for _, r := range runs {
				if s.reportSelection[r.Path] {
					paths = append(paths, r.Path)
				}
			}
			s.work(func() (string, *core.ApiError) {
				report, e := s.service.CreateReport(core.CreateReportRequest{RunPaths: paths})
				return "已生成 " + report.ReportId, e
			})
		}
	})
	card(c, "报告列表", func() {
		errorView(c, s.reportsError)
		if len(s.reports) == 0 {
			ui.Text(c, "暂无报告。")
		}
		for _, report := range s.reports {
			ui.Column(c).Gap(8).Key(report.Path).Children(func() {
				ui.Text(c, report.ReportId).Bold()
				ui.Text(c, "目录修改时间："+text(report.CreatedAt))
				ui.Text(c, report.Path).Selectable()
				for i, id := range report.RunIds {
					path := "来源不可达"
					if i < len(report.RunPaths) && report.RunPaths[i] != nil {
						path = *report.RunPaths[i]
					}
					ui.Text(c, id+" · "+path).Selectable()
				}
				if ui.Button(c, "打开报告 "+report.ReportId).Clicked() {
					_, s.failure = s.reportWindows.Open(report.ReportId)
				}
			})
		}
	})
}
func (s *viewState) selectedReports() int {
	count := 0
	for _, selected := range s.reportSelection {
		if selected {
			count++
		}
	}
	return count
}
