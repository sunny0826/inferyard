// Package pages implements the Studio desktop views directly over core.Service.
package pages

import (
	"context"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

var navigation = []struct{ id, label string }{{"wizard", "向导"}, {"device", "设备"}, {"models", "模型与下载"}, {"runs", "运行"}, {"reports", "报告"}, {"settings", "设置"}}

type Window struct {
	Win   *mygo.Window
	state *viewState
}

func New(ctx context.Context, service *core.Service, page string, reports *Reports) *Window {
	ctx, cancel := context.WithCancel(ctx)
	s := &viewState{ctx: ctx, service: service, selected: pageID(page), reportSelection: map[string]bool{}, denied: map[string]bool{}, chartIndex: -1}
	w := mygo.NewWindow(mygo.WindowOptions{Title: "InferYard Studio", Width: 1120, Height: 800, Content: ui.View(s.view)})
	s.win = w
	s.reportWindows = reports
	w.OnClosed(cancel)
	wake, off, cursor := service.Bus.Subscribe()
	go func() {
		defer off()
		s.refresh(true)
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				events, reset, next := service.Bus.After(cursor)
				cursor = next
				full := reset
				for _, e := range events {
					if e.Type == "sync.reset" || e.Type == "report.created" || e.Type == "run.state" {
						full = true
					}
				}
				s.refresh(full)
			}
		}
	}()
	return &Window{Win: w, state: s}
}
func pageID(page string) string {
	page = strings.TrimPrefix(page, "/")
	for _, n := range navigation {
		if page == n.id {
			return page
		}
	}
	return "wizard"
}
func (w *Window) Navigate(page string) {
	w.Win.Update(func() {
		w.state.selected = pageID(page)
		w.state.detailID = ""
		w.state.detailMu.Lock()
		w.state.requestedDetail = ""
		w.state.detailMu.Unlock()
	})
	w.Win.Focus()
	go w.state.refresh(true)
}

type viewState struct {
	initDir, engine, process, bindEndpoint, candidate, installStatus, refreshFailure string
	enginePreset, assetsPreset                                                       bool
	engineState                                                                      core.EngineState
	startup                                                                          core.Startup
	processes                                                                        []core.ProcessCandidate
	runProgress                                                                      *core.RunProgress
	ctx                                                                              context.Context
	service                                                                          *core.Service
	win                                                                              *mygo.Window
	reportWindows                                                                    *Reports
	refreshMu                                                                        sync.Mutex
	detailMu                                                                         sync.Mutex
	requestedDetail                                                                  string
	selected, detailID                                                               string
	settings                                                                         core.SettingsResponse
	ready                                                                            core.Readiness
	progress                                                                         *inferyard.WorkspaceProgress
	device                                                                           core.DeviceResponse
	history                                                                          core.RunHistoryResponse
	tasks                                                                            []core.RunTask
	downloads                                                                        []core.DownloadTask
	reports                                                                          []core.ReportSummary
	detail                                                                           *core.RunTaskDetail
	historyError, reportsError, progressError, detailError                           *core.ApiError
	loaded, busy, settingsLoaded                                                     bool
	message                                                                          string
	failure                                                                          *core.ApiError
	workspace, cliPath, results, models                                              string
	firstBundle, secondBundle                                                        string
	single, confirmed                                                                bool
	groups                                                                           [][]string
	resumeSource                                                                     *core.RunSummary
	endpoint, pid                                                                    string
	denied                                                                           map[string]bool
	modelURL, modelName, tokenEnv, engineURL, engineName                             string
	reportSelection                                                                  map[string]bool
	scrolls                                                                          map[string]*ui.ScrollState
	casesTable, samplesTable                                                         ui.ListState
	chartIndex                                                                       int
}

func (s *viewState) refresh(full bool) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	device, tasks, downloads := s.service.Device(), s.service.RunTasks(), s.service.Downloads()
	s.detailMu.Lock()
	id := s.requestedDetail
	s.detailMu.Unlock()
	var detail *core.RunTaskDetail
	var de *core.ApiError
	if id != "" {
		d, e := s.service.RunDetail(id)
		de = e
		if e == nil {
			detail = &d
		}
	}
	var settings core.SettingsResponse
	var ready core.Readiness
	var progress core.ProgressResponse
	var history core.RunHistoryResponse
	var reports []core.ReportSummary
	var he, re, pe *core.ApiError
	var onboarding onboardingSnapshot
	engineState := s.service.Engine()
	var runProgress *core.RunProgress
	for _, task := range tasks {
		if !terminal(task.State) {
			if d, e := s.service.RunDetail(task.TaskId); e == nil {
				runProgress = d.Progress
			}
			break
		}
	}
	if full {
		settings = s.service.Settings()
		ready = s.service.Readiness()
		history, he = s.service.History()
		_, reports, re = s.service.Reports()
		if ready.CLI.Status == "ok" {
			progress, pe = s.service.Progress(s.ctx)
		}
		onboarding = s.readOnboarding(progress.Progress)
	}
	if s.ctx.Err() != nil {
		return
	}
	s.win.Update(func() {
		if s.ctx.Err() != nil {
			return
		}
		s.device, s.tasks, s.downloads = device, tasks, downloads
		s.engineState, s.runProgress = engineState, runProgress
		if s.detailID == id {
			s.detail, s.detailError = detail, de
		}
		if full {
			s.applySettings(settings)
			s.ready, s.progress = ready, progress.Progress
			s.applyOnboarding(onboarding, pe)
			s.history, s.reports = history, reports
			s.historyError, s.reportsError, s.progressError = he, re, pe
			known := map[string]bool{}
			for _, r := range s.historyRuns() {
				known[r.Path] = true
			}
			for path := range s.reportSelection {
				if !known[path] {
					delete(s.reportSelection, path)
				}
			}
		}
		s.loaded = true
	})
}
func (s *viewState) work(fn func() (string, *core.ApiError)) {
	if s.busy {
		return
	}
	s.busy = true
	s.failure = nil
	s.message = ""
	go func() {
		message, e := fn()
		if s.ctx.Err() != nil {
			return
		}
		s.win.Update(func() {
			s.busy = false
			s.message = message
			s.failure = e
			if e != nil {
				s.message = ""
			}
		})
		s.refresh(true)
	}()
}
func (s *viewState) selectTask(id string) {
	s.detailID = id
	s.detail = nil
	s.detailError = nil
	s.detailMu.Lock()
	s.requestedDetail = id
	s.detailMu.Unlock()
	go s.refresh(false)
}
func (s *viewState) view(c *ui.Context) {
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Sidebar(c, &s.selected, func() {
			ui.Text(c, "InferYard Studio").Bold().Margin(12)
			for _, n := range navigation {
				ui.SidebarItem(c, n.id, nil, n.label)
			}
		}).Width(205)
		ui.Scroll(c).Grow(1).Key(s.selected).TrackScroll(s.pageScroll()).Padding(24).Children(func() {
			ui.Column(c).Gap(16).Children(func() {
				s.feedback(c)
				if s.message != "" {
					ui.Text(c, s.message).TextColor(c.Theme().Success)
				}
				if s.busy {
					ui.Text(c, "正在执行，请稍候…")
				}
				switch s.selected {
				case "device":
					s.deviceView(c)
				case "models":
					s.modelsView(c)
				case "runs":
					if s.detailID != "" {
						s.detailView(c)
					} else {
						s.runsView(c)
					}
				case "reports":
					s.reportsView(c)
				case "settings":
					s.settingsView(c)
				default:
					s.wizardView(c)
				}
			})
		})
	})
}
func (s *viewState) header(c *ui.Context, title string) {
	ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).Bold().FontSize(24).Grow(1)
		if ui.Button(c, "刷新").Disabled(s.busy).Clicked() {
			go s.refresh(true)
		}
	})
}
func card(c *ui.Context, title string, fn func()) {
	ui.Column(c).Gap(10).Padding(18).Background(c.Theme().Surface).Radius(12).Children(func() { ui.Text(c, title).Bold().FontSize(18); fn() })
}
func field(c *ui.Context, label string, value *string) {
	ui.Field(c, label, func() { ui.TextInput(c, value).Width(540) })
}
func text(p *string) string {
	if p == nil {
		return "未知"
	}
	return *p
}
func emptyText(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func terminal(state string) bool { return state == "done" || state == "failed" || state == "cancelled" }

func (s *viewState) pageScroll() *ui.ScrollState {
	if s.scrolls == nil {
		s.scrolls = map[string]*ui.ScrollState{}
	}
	key := s.selected
	if key == "runs" && s.detailID != "" {
		key = "detail"
	}
	if s.scrolls[key] == nil {
		s.scrolls[key] = &ui.ScrollState{}
	}
	return s.scrolls[key]
}
