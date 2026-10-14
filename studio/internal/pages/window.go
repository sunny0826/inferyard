// Package pages implements the Studio desktop views directly over core.Service.
package pages

import (
	"context"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chart"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"github.com/oneo-me/onevra/source/theme"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

var navigation = []struct {
	id, label string
	icon      func() *ui.SVG
}{
	{"dashboard", "Dashboard", lucide.LayoutDashboard},
	{"device", "设备", lucide.Cpu},
	{"models", "模型与下载", lucide.Download},
	{"reports", "报告", lucide.FileText},
	{"settings", "设置", lucide.Settings},
}

// New opens the main window on the Onevra sidebar shell: a collapsible,
// resizable navigation sidebar, a floating window header and the Onevra theme.
// The navigation and content builders are shared with the in-memory test view.
func New(ctx context.Context, service *core.Service, page string, reports *Reports) {
	ctx, cancel := context.WithCancel(ctx)
	s := &viewState{ctx: ctx, service: service, selected: pageID(page), denied: map[string]bool{}, chartIndex: -1}
	shell := sidebar.NewShell(sidebar.ShellOptions{
		Title:           "InferYard Studio",
		Width:           1120,
		Height:          800,
		MinWidth:        800,
		MinHeight:       600,
		StateKey:        "inferyard-studio-main",
		LeftWidth:       205,
		Left:            s.leftNav,
		Content:         s.contentView,
		Theme:           theme.Onevra,
		LeftToggleLabel: "切换侧栏",
		LeftResizeLabel: "调整侧栏宽度",
		MinimizeLabel:   "最小化",
		MaximizeLabel:   "最大化",
		RestoreLabel:    "还原",
		CloseLabel:      "关闭",
		CloseToastLabel: "关闭通知",
	})
	w := shell.Window()
	s.win = w
	s.reportWindows = reports
	if reports != nil {
		reports.onFailure = func(e *core.ApiError) { w.Update(func() { s.failure = e }) }
	}
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
}
func pageID(page string) string {
	page = strings.TrimPrefix(page, "/")
	for _, n := range navigation {
		if page == n.id {
			return page
		}
	}
	return "dashboard"
}

type viewState struct {
	announcedPage                                                                       string
	announcedErrors                                                                     map[string]bool
	returnToTasks                                                                       bool
	refreshing                                                                          bool
	initDir, engine, process, bindEndpoint, candidate, cliInstallStatus, refreshFailure string
	enginePreset, assetsPreset                                                          bool
	engineState                                                                         core.EngineState
	startup                                                                             core.Startup
	processes                                                                           []core.ProcessCandidate
	runProgress                                                                         *core.RunProgress
	ctx                                                                                 context.Context
	service                                                                             *core.Service
	win                                                                                 *mygo.Window
	reportWindows                                                                       *Reports
	refreshMu                                                                           sync.Mutex
	detailMu                                                                            sync.Mutex
	requestedDetail                                                                     string
	selected, detailID                                                                  string
	settings                                                                            core.SettingsResponse
	ready                                                                               core.Readiness
	progress                                                                            *inferyard.WorkspaceProgress
	device                                                                              core.DeviceResponse
	history                                                                             core.RunHistoryResponse
	tasks                                                                               []core.RunTask
	downloads                                                                           []core.DownloadTask
	reports                                                                             []core.ReportSummary
	detail                                                                              *core.RunTaskDetail
	historyError, reportsError, detailError                                             *core.ApiError
	loaded, busy, settingsLoaded                                                        bool
	pendingToast                                                                        string
	failure                                                                             *core.ApiError
	workspace, cliPath, results, models                                                 string
	manualFirst, manualSecond                                                           string
	manualConfig                                                                        bool
	bindingStale                                                                        bool
	bindingUnknown                                                                      bool
	firstBundle, secondBundle                                                           string
	single, confirmed                                                                   bool
	groups                                                                              [][]string
	resumeSource                                                                        *core.RunSummary
	endpoint, pid                                                                       string
	denied                                                                              map[string]bool
	modelURL, modelName, tokenEnv, engineURL, engineName                                string
	tasksTable, historyTable, reportsTable                                              pageTableState
	reportSources                                                                       ui.ListState
	reportChoices                                                                       ui.Selection[string]
	scrolls                                                                             map[string]*ui.ScrollState
	casesTable, samplesTable                                                            pageTableState
	resourceCharts                                                                      [3]chart.State
	chartIndex                                                                          int
}

func (s *viewState) refresh(full bool) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	s.win.Update(func() { s.refreshing = true })
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
	var dashboard dashboardSnapshot
	var runProgress *core.RunProgress
	for _, task := range tasks {
		if !terminal(task.State) {
			if d, e := s.service.RunDetail(task.TaskId); e == nil {
				runProgress = d.Progress
			}
			break
		}
	}
	settings = s.service.Settings()
	if full {
		history, he = s.service.History()
		_, reports, re = s.service.Reports()
	}
	ready = s.service.Readiness()
	if ready.CLI.Status == "ok" {
		progress, pe = s.service.Progress(s.ctx)
	}
	dashboard = s.readDashboard(progress.Progress)
	engineState := s.service.Engine()
	if s.ctx.Err() != nil {
		return
	}
	s.win.Update(func() {
		if s.ctx.Err() != nil {
			return
		}
		s.refreshing = false
		s.device, s.tasks, s.downloads = device, tasks, downloads
		s.runProgress = runProgress
		if s.detailID == id {
			if de == nil {
				s.detail = detail
			}
			s.detailError = de
		}
		s.applySettings(settings)
		if full {
			if he == nil {
				s.history = history
			}
			if re == nil {
				s.reports = reports
			}
			s.historyError, s.reportsError = he, re
		}
		s.ready, s.progress = ready, progress.Progress
		s.applyDashboard(dashboard, pe)
		s.engineState = engineState
		s.loaded = true
	})
}
func (s *viewState) work(fn func() (string, *core.ApiError)) {
	if s.busy {
		return
	}
	s.busy = true
	s.failure = nil
	s.pendingToast = ""
	go func() {
		message, e := fn()
		if s.ctx.Err() != nil {
			return
		}
		s.win.Update(func() { s.finishWork(message, e) })
		s.refresh(true)
	}()
}
func (s *viewState) finishWork(message string, e *core.ApiError) {
	s.busy = false
	s.failure = e
	s.pendingToast = ""
	if e == nil {
		s.pendingToast = message
	}
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
	if s.pendingToast != "" {
		message := s.pendingToast
		s.pendingToast = ""
		c.Toast(message)
	}
	s.announceChanges(c)

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(205).Shrink(0).Children(func() { s.leftNav(c) })
		s.contentView(c)
	})
}

// leftNav builds the navigation column used both by the shell's left region
// and by the in-memory test view. It clears the shell's floating header.
func (s *viewState) leftNav(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().MinWidth(0).Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		components.Sidebar(c).Children(func() {
			components.Text(c, "InferYard Studio").Bold().FontSize(t.Rem(1)).
				Padding(t.Space(2), t.Space(2), t.Space(1), t.Space(2)).SingleLine()
			for _, n := range navigation {
				if components.SidebarItem(c, n.label, n.icon(), s.selected == n.id).Clicked() {
					s.selected = n.id
				}
			}
		})
	})
}

// contentView builds the page area below the shell's floating header: the
// activity toolbar stays fixed while page content scrolls beneath it.
func (s *viewState) contentView(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Fill().Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		s.activityStatus(c)
		ui.Scroll(c).Grow(1).Key(s.selected).TrackScroll(s.pageScroll()).Padding(c.Theme().Space(6)).Children(func() {
			ui.Column(c).Gap(c.Theme().Space(4)).Children(func() {
				s.feedback(c)
				if !s.loaded {
					title := "首次上手"
					for _, n := range navigation {
						if n.id == s.selected {
							title = n.label
						}
					}
					s.header(c, title)
					if s.failure != nil || s.refreshFailure != "" {
						button(c, "重试读取页面").AlignSelf(ui.Start).OnClick(s.refreshAction)
					} else {
						ui.Text(c, "正在读取页面数据…")
						progressBar(c, -1, "页面数据加载进度")
					}
					return
				}
				switch s.selected {
				case "device":
					s.deviceView(c)
				case "models":
					s.modelsView(c)
				case "dashboard":
					if s.detailID != "" {
						s.detailView(c)
					} else {
						s.dashboardView(c)
					}
				case "reports":
					s.reportsView(c)
				case "settings":
					s.settingsView(c)
				default:
					s.dashboardView(c)
				}
			})
		})
	})
}

func (s *viewState) refreshAction() {
	if !s.busy {
		go s.refresh(true)
	}
}
func (s *viewState) activityStatus(c *ui.Context) {
	if !s.busy && !s.refreshing {
		return
	}
	ui.Toolbar(c, func() {
		if s.refreshing && !s.busy {
			activitySpinner(c, "正在刷新页面")
			ui.Text(c, "正在刷新…")
		}
		if s.busy {
			activitySpinner(c, "正在执行操作")
			ui.Text(c, "执行中…")
		}
	})
}
func (s *viewState) header(c *ui.Context, title string) {
	heading(c, title, 1)
}
func card(c *ui.Context, title string, fn func()) {
	ui.Column(c).MinWidth(0).Gap(c.Theme().Space(2.5)).Padding(c.Theme().Space(4.5)).Background(c.Theme().Surface).Radius(c.Theme().Space(3)).Children(func() { heading(c, title, 2); fn() })
}
func field(c *ui.Context, label string, value *string) ui.Element {
	return ui.Field(c, label, func() { components.TextBox(c, value, components.TextBoxOptions{}) })
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
	if key == "dashboard" && s.detailID != "" {
		key = "detail"
	}
	if s.scrolls[key] == nil {
		s.scrolls[key] = &ui.ScrollState{}
	}
	return s.scrolls[key]
}
