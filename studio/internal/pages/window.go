// Package pages implements the Studio desktop views directly over core.Service.
package pages

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chart"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

var navigation = []struct {
	id, label string
	icon      func() *ui.SVG
}{
	{"reports", "报告", lucide.FileText},
	{"models", "模型", lucide.Layers},
	{"downloads", "下载", lucide.Download},
	{"device", "设备", lucide.Cpu},
	{"settings", "设置", lucide.Settings},
}

// New opens the main window on the Onevra sidebar shell: a collapsible,
// resizable navigation sidebar, a floating window header and the Onevra theme.
// The navigation and content builders are shared with the in-memory test view.
func New(ctx context.Context, service *core.Service, page string) {
	ctx, cancel := context.WithCancel(ctx)
	s := &viewState{ctx: ctx, service: service, selected: pageID(page), denied: map[string]bool{}, chartIndex: -1}
	shell := sidebar.NewShell(pageShellOptions("InferYard Studio", "inferyard-studio-main", s.leftNav, s.contentView))
	w := shell.Window()
	s.win = w
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
				s.refresh(fullRefresh(events, reset))
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
	if page == "dashboard" {
		return page
	}
	return "reports"
}

type viewState struct {
	preview                                                                             *previewSession
	lastEngineVerification                                                              time.Time
	discoveryCLI                                                                        string
	autoBind                                                                            bindingArm
	binding                                                                             bool
	bindingBundle                                                                       string
	discovery                                                                           inferyard.ServiceDiscovery
	discoveryError                                                                      *core.ApiError
	discovering, discoveryLoaded                                                        bool
	registry                                                                            core.Registry
	checks                                                                              core.EngineCheckOverview
	attributionKey                                                                      string
	attributionMatch, attributionKnown                                                  bool
	candidateMatches                                                                    func(candidate, model, engine string) (bool, bool)
	model                                                                               string
	modelQuery, engineQuery                                                             string
	modelList, engineList                                                               components.ListState
	announcedPage                                                                       string
	announcedErrors                                                                     map[string]bool
	refreshing                                                                          bool
	initDir, engine, process, bindEndpoint, candidate, cliInstallStatus, refreshFailure string
	assetsPreset                                                                        bool
	engineState                                                                         core.EngineState
	startup                                                                             core.Startup
	processes                                                                           []core.ProcessCandidate
	runProgress                                                                         *core.RunProgress
	ctx                                                                                 context.Context
	service                                                                             *core.Service
	win                                                                                 *mygo.Window
	openedReport                                                                        *reportViewState
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
	bindingStale                                                                        bool
	bindingUnknown                                                                      bool
	task                                                                                benchmarkTask
	selectedBundles                                                                     map[string]bool
	focusedRun                                                                          string
	taskCancel                                                                          context.CancelFunc
	deviceChecked                                                                       string
	newTaskPending                                                                      bool
	resumeSource                                                                        *core.RunSummary
	endpoint, pid                                                                       string
	denied                                                                              map[string]bool
	modelURL, modelName, tokenEnv, engineURL, engineName                                string
	historyTable, reportsTable                                                          pageTableState
	reportSources                                                                       components.ListState
	reportChoices                                                                       ui.Selection[any]
	scrolls                                                                             map[string]*ui.ScrollState
	casesTable, samplesTable                                                            pageTableState
	resourceCharts                                                                      [3]chart.State
	chartIndex                                                                          int
}

func (s *viewState) refresh(full bool) {
	if s.preview != nil {
		return
	}
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
	var registry core.Registry
	if full {
		registry = s.service.Registry()
	}
	if s.ctx.Err() != nil {
		return
	}
	verifyVisible := false
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
		// engine.state wakes this refresh; read the latest state after slow scans
		// so an older snapshot cannot disarm a just-started binding chain.
		s.engineState = s.service.Engine()
		if full {
			s.registry = registry
		}
		s.checks = s.service.EngineChecks()
		s.adoptDownloadedEngine()
		s.loaded = true
		s.continueAutoBind()
		// Engine verification runs only while the models page or the Dashboard
		// candidate stage is visible.
		verifyVisible = s.selected == "models" ||
			(s.selected == "dashboard" && s.progress != nil && !s.progress.DefaultScopeComplete && s.progress.NextAction == "prepare_candidate")
		cliKey := emptyText(ready.CLI.Path) + "|" + emptyText(ready.CLI.Version) + "|" + ready.CLI.Status
		if cliKey != s.discoveryCLI {
			s.discoveryCLI, s.discoveryLoaded = cliKey, false
		}
		if !s.discoveryLoaded && !s.discovering {
			s.refreshDiscovery()
		}
	})
	if s.shouldVerifyEngines(full, verifyVisible, time.Now()) {
		s.service.VerifyEngines()
	}
}
func (s *viewState) work(fn func() (string, *core.ApiError)) {
	if s.previewAction() {
		return
	}
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
	if s.preview != nil {
		s.detailID, s.detail, s.detailError = id, previewDetail(id), nil
		return
	}
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
		c.AddToast(ui.Toast{ID: message, Title: message})
	}
	s.announceChanges(c)

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(205).Shrink(0).Children(func() { s.leftNav(c) })
		s.contentView(c)
	})
}

// selectPage switches the visible page. Opening the models page kicks engine
// verification directly: it otherwise runs only inside the event-driven
// refresh loop, and on a quiet workspace no bus event arrives after the page
// opens, leaving engines stuck at "unverified" (and unselectable) forever.
func (s *viewState) selectPage(id string) {
	if s.selected == id {
		return
	}
	s.selected = id
	if id != "models" || s.service == nil {
		return
	}
	go func() {
		s.service.VerifyEngines()
		if s.win != nil {
			s.win.Update(func() { s.checks = s.service.EngineChecks() })
		}
	}()
}

// studioOrigin reports a go run or go test process as source, and a built binary as packaged.
func studioOrigin() string {
	exe, err := os.Executable()
	if err != nil {
		return "Studio 来源未知"
	}
	if strings.Contains(filepath.ToSlash(exe), "/go-build") {
		return "Studio 源码运行 · " + exe
	}
	return "Studio 发行包 · " + exe
}

// leftNav builds the navigation column used both by the shell's left region
// and by the in-memory test view. It clears the shell's floating header.
func (s *viewState) leftNav(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().MinWidth(0).Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		components.Sidebar(c).Children(func() {
			components.Text(c, "InferYard Studio").Bold().FontSize(t.Rem(1)).
				Padding(t.Space(2), t.Space(2), t.Space(1), t.Space(2)).SingleLine()
			button(c, "新任务").OnClick(func() { s.newBenchmark() })
			for _, n := range navigation {
				if components.SidebarItem(c, n.label, n.icon(), s.selected == n.id).Clicked() {
					s.selectPage(n.id)
				}
			}
		})
	})
}

// contentView builds the page area below the shell's floating header: the
// corner status stays fixed while page content scrolls beneath it.
func (s *viewState) contentView(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Fill().Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		s.cornerStatus(c)
		ui.Scroll(c).Grow(1).Key(s.selected).TrackScroll(s.pageScroll()).Padding(c.Theme().Space(6)).Children(func() {
			ui.Column(c).Gap(c.Theme().Space(4)).Children(func() {
				s.feedback(c)
				if !s.loaded {
					title := "首次上手"
					if s.selected == "dashboard" {
						title = "Dashboard"
						if s.detailID != "" {
							title = "运行详情"
						}
					}
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
				case "downloads":
					s.downloadsPageView(c)
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
	if s.previewAction() {
		return
	}
	if !s.busy {
		go s.refresh(true)
	}
}
func (s *viewState) cornerStatus(c *ui.Context) {
	engine := "已停止"
	if s.engineState.State != "" {
		engine = stateLabel(s.engineState.State)
	}
	runActive := false
	for _, task := range s.tasks {
		if !terminal(task.State) {
			runActive = true
			break
		}
	}
	queue := "空"
	if runActive {
		queue = "执行中"
	}
	ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(c.Theme().Space(2)).Padding(0, c.Theme().Space(6)).Children(func() {
		if (s.busy || s.refreshing) && !runActive && (s.engineState.State == "" || s.engineState.State == "stopped") {
			ui.Text(c, "执行中")
		} else {
			ui.Text(c, "引擎 "+engine+" · 队列 "+queue)
		}
		if s.stopEnabled(true, runActive) {
			button(c, "停止").OnClick(s.stopEngine)
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
	if key == "reports" && s.openedReport != nil {
		key = "report/" + s.openedReport.reference
	}
	if s.scrolls[key] == nil {
		s.scrolls[key] = &ui.ScrollState{}
	}
	return s.scrolls[key]
}

// Called under refreshMu; progress ticks reuse the current verification targets.
func (s *viewState) shouldVerifyEngines(full, visible bool, now time.Time) bool {
	if !visible || (!full && !s.lastEngineVerification.IsZero() && now.Sub(s.lastEngineVerification) < 5*time.Second) {
		return false
	}
	s.lastEngineVerification = now
	return true
}
