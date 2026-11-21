package pages

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

var navigation = []struct {
	id, label string
	icon      func() *ui.SVG
}{
	{"dashboard", "任务", lucide.Play},
	{"reports", "报告", lucide.FileText},
	{"models", "模型", lucide.Layers},
	{"downloads", "下载", lucide.Download},
	{"device", "设备", lucide.Cpu},
	{"settings", "设置", lucide.Settings},
}

func New(ctx context.Context, service *core.Service, page string) func(core.Event) bool {
	ctx, cancel := context.WithCancel(ctx)
	title := "InferYard Studio"
	if paths := service.BoundPaths(); paths != nil {
		title = paths.WindowTitle
	}
	s := &viewState{ctx: ctx, service: service, selected: firstOpen(page, service.FirstRun()), windowTitle: title, denied: map[string]bool{}, speed: &speedWorkspace{context: inferyard.ContextSpeed64K}}
	shell := sidebar.NewShell(pageShellOptions(title, "inferyard-studio-main", s.leftNav, s.contentView))
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
				if len(events) > 0 {
					batch := events
					s.win.Update(func() { s.ingestCoreEvents(batch) })
				}
				s.refresh(fullRefresh(events, reset))
			}
		}
	}()
	return s.shouldSuppressOSNotification
}
func firstOpen(page string, first bool) string {
	if strings.TrimPrefix(page, "/") != "" {
		return pageID(page)
	}
	if first {
		return "dashboard"
	}
	return "reports"
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
	speed                                                                               *speedWorkspace
	preview                                                                             previewMode
	lastEngineVerification                                                              time.Time
	discoveryCLI                                                                        string
	binding                                                                             bool
	bindingBundle                                                                       string
	discovery                                                                           inferyard.ServiceDiscovery
	discoveryError                                                                      *core.ApiError
	discovering, discoveryLoaded                                                        bool
	registry                                                                            core.Registry
	checks                                                                              core.EngineCheckOverview
	installationPath, localPID, localEndpoint, localBindingTask                         string
	model                                                                               string
	announcedPage, promptPage                                                           string
	announcedErrors                                                                     map[string]bool
	refreshing                                                                          bool
	initDir, engine, process, bindEndpoint, candidate, cliInstallStatus, refreshFailure string
	openDialog                                                                          func(mygo.OpenDialogOptions) ([]string, error)
	cancelRun                                                                           func(string) (core.RunTask, *core.ApiError)
	stopEngineAPI                                                                       func() (core.EngineState, *core.ApiError)
	cancelRound                                                                         func(string) (core.RunTask, *core.ApiError)
	runTasks                                                                            func() []core.RunTask
	updateWindow                                                                        func(func())
	browseFailures                                                                      map[string]string
	assetsPreset                                                                        bool
	engineState                                                                         core.EngineState
	startup                                                                             core.Startup
	processes                                                                           []core.ProcessCandidate
	runProgress                                                                         *core.RunProgress
	ctx                                                                                 context.Context
	service                                                                             *core.Service
	win                                                                                 *mygo.Window
	openedReport                                                                        *reportViewState
	reportsTab                                                                          int
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
	busyAction                                                                          string
	windowTitle                                                                         string
	pendingToast                                                                        string
	pendingToasts                                                                       []toastSpec
	activeToastID                                                                       string
	toastPhase                                                                          int
	notifs                                                                              *notificationCenter
	lastManualReportID                                                                  string
	engineStartToastID                                                                  string
	failure                                                                             *core.ApiError
	failurePage                                                                         string
	workspace, cliPath, results, models                                                 string
	bindingStale                                                                        bool
	task                                                                                benchmarkTask
	selectedBundles                                                                     map[string]bool
	focusedRun                                                                          string
	taskCancel                                                                          context.CancelFunc
	deviceChecked                                                                       string
	newTaskPending                                                                      bool
	pendingDestructive                                                                  destructiveRequest
	resumeSource                                                                        *core.RunSummary
	resumeDiscardConfirm                                                                bool
	reportDialogOpen, reportDiscardConfirm                                              bool
	endpoint, pid                                                                       string
	denied                                                                              map[string]bool
	bindFailure                                                                         *core.ApiError
	failedBundle                                                                        string
	handoffFailure                                                                      *core.ApiError
	engineFailureCleared                                                                string
	localUsedPIDs                                                                       map[int64]bool
	localPendingPIDs                                                                    map[string]int64
	modelURL, modelName, tokenEnv, engineURL, engineName                                string
	historyTable                                                                        pageTableState
	reportChoices                                                                       ui.Selection[any]
	scrolls                                                                             map[string]*ui.ScrollState
	casesTable, samplesTable                                                            pageTableState
	resourceTablePage                                                                   int
	settingsEpochMu                                                                     sync.Mutex
	settingsEpoch                                                                       uint64
}

func (s *viewState) settingsSequence() uint64 {
	s.settingsEpochMu.Lock()
	defer s.settingsEpochMu.Unlock()
	return s.settingsEpoch
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
	settingsEpoch := s.settingsSequence()
	s.win.Update(func() { s.refreshing = true })
	device, tasks, downloads := s.service.Device(), s.service.RunTasks(), s.service.Downloads()
	speedTasks, speedHistoryError := s.service.ContextSpeedHistory()
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
		batch := refreshBatch{
			epoch: settingsEpoch, full: full, device: device, tasks: tasks, downloads: downloads,
			speedTasks: speedTasks, speedHistoryError: speedHistoryError, runProgress: runProgress,
			id: id, detail: detail, detailError: de, settings: settings,
			history: history, reports: reports, historyError: he, reportsError: re,
			ready: ready, progress: progress, progressError: pe, dashboard: dashboard, registry: registry,
		}
		if !s.applyRefreshBatch(batch) {
			return
		}
		verifyVisible = s.selected == "models" ||
			(s.selected == "dashboard" && s.progress != nil && !s.progress.DefaultScopeComplete && s.progress.NextAction == "prepare_candidate")
	})
	if s.shouldVerifyEngines(full, verifyVisible, time.Now()) {
		s.service.VerifyEngines()
	}
}

type refreshBatch struct {
	epoch                      uint64
	full                       bool
	device                     core.DeviceResponse
	tasks                      []core.RunTask
	downloads                  []core.DownloadTask
	speedTasks                 []core.RunTask
	speedHistoryError          *core.ApiError
	runProgress                *core.RunProgress
	id                         string
	detail                     *core.RunTaskDetail
	detailError                *core.ApiError
	settings                   core.SettingsResponse
	history                    core.RunHistoryResponse
	reports                    []core.ReportSummary
	historyError, reportsError *core.ApiError
	ready                      core.Readiness
	progress                   core.ProgressResponse
	progressError              *core.ApiError
	dashboard                  dashboardSnapshot
	registry                   core.Registry
}

func (s *viewState) applyRefreshBatch(b refreshBatch) bool {
	if !s.refreshBatchCurrent(b.epoch) {
		return false
	}
	s.device, s.tasks, s.downloads = b.device, b.tasks, b.downloads
	s.reconcileLocalPIDs(b.tasks)
	if b.speedHistoryError == nil {
		s.syncSpeedTasks(b.speedTasks)
	}
	s.runProgress = b.runProgress
	if s.detailID == b.id {
		if b.detailError == nil {
			s.detail = b.detail
		}
		s.detailError = b.detailError
	}
	s.applySettings(b.settings)
	if b.full {
		if b.historyError == nil {
			s.history = b.history
		}
		if b.reportsError == nil {
			s.reports = b.reports
		}
		s.historyError, s.reportsError = b.historyError, b.reportsError
		s.registry = b.registry
	}
	s.ready, s.progress = b.ready, b.progress.Progress
	s.applyDashboard(b.dashboard, b.progressError)
	s.engineState = s.service.Engine()
	s.checks = s.service.EngineChecks()
	s.adoptDownloadedEngine()
	s.loaded = true
	cliKey := emptyText(b.ready.CLI.Path) + "|" + emptyText(b.ready.CLI.Version) + "|" + b.ready.CLI.Status
	if cliKey != s.discoveryCLI {
		s.discoveryCLI, s.discoveryLoaded = cliKey, false
	}
	if !s.discoveryLoaded && !s.discovering {
		s.refreshDiscovery()
	}
	return true
}

func (s *viewState) work(fn func() (string, *core.ApiError)) {
	s.workAs("", fn)
}

func (s *viewState) workAs(action string, fn func() (string, *core.ApiError)) {
	if s.previewAction() {
		return
	}
	if s.busy {
		return
	}
	s.busy = true
	s.busyAction = action
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

func (s *viewState) actionBusy(action string) bool {
	return s.busy && action != "" && s.busyAction == action
}

func (s *viewState) finishWork(message string, e *core.ApiError) {
	s.busy = false
	s.busyAction = ""
	s.failure = e
	if e != nil {
		s.failurePage = s.selected
	} else {
		s.failurePage = ""
	}
	s.pendingToast = ""
	if e == nil && message != "" {
		s.enqueueStatusToast(message)
	}
}
func (s *viewState) selectTask(id string) {
	if s.preview != nil {
		s.detailID, s.detail, s.detailError = id, s.preview.detail(id), nil
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
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(205).Shrink(0).Children(func() { s.leftNav(c) })
		s.contentView(c)
	})
	s.resumeDialog(c)
	studioToasts(c)
}

func (s *viewState) contentView(c *ui.Context) {
	s.consumeFeedback(c)
	s.dropStaleStopPrompt()
	s.clearPromptOnNavigation()
	ui.Column(c).Grow(1).MinWidth(0).Fill().Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		s.cornerStatus(c)
		ui.Scroll(c.Key(s.selected)).Grow(1).TrackScroll(s.pageScroll()).Padding(tokens(c).Density.Page).Children(func() {
			ui.Column(c).Gap(tokens(c).Density.Section).Children(func() {
				s.feedback(c)
				if !s.loaded {
					s.loadingView(c)
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
	s.studioNotifications(c)
	studioToasts(c)
}

func (s *viewState) loadingView(c *ui.Context) {
	title := "首次上手"
	if s.selected == "dashboard" {
		title = ""
		if s.detailID != "" {
			title = "运行详情"
		}
	}
	for _, n := range navigation {
		if n.id == s.selected {
			title = n.label
		}
	}
	if title != "" {
		s.header(c, title)
	}
	if s.failure != nil || s.refreshFailure != "" {
		button(c, "重试读取页面").AlignSelf(ui.Start).OnClick(s.refreshAction)
		return
	}
	host := ui.Column(c.Key("page-loading")).FillWidth().Gap(tokens(c).Density.Section)
	show := delayedVisible(c, host, "page-load", !s.loaded)
	host.Children(func() {
		if show {
			pageLoadingSkeleton(c)
		}
	})
}

func (s *viewState) refreshAction() {
	if s.simulate(previewReload) {
		return
	}
	if !s.busy {
		go s.refresh(true)
	}
}
func (s *viewState) header(c *ui.Context, title string) {
	heading(c, title, 1)
}
func field(c *ui.Context, label string, value *string) ui.Element {
	return ui.Field(c, label, func() { components.TextBox(c, value, components.TextBoxOptions{}) })
}
func text(p *string) string {
	if p == nil || *p == "" {
		return "缺测"
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

func (s *viewState) shouldVerifyEngines(full, visible bool, now time.Time) bool {
	if !visible || (!full && !s.lastEngineVerification.IsZero() && now.Sub(s.lastEngineVerification) < 5*time.Second) {
		return false
	}
	s.lastEngineVerification = now
	return true
}
