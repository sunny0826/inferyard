package pages

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

var navigation = []struct {
	id, labelKey string
	icon         func() *ui.SVG
}{
	{"benchmarks", "nav.benchmarks", lucide.Play},
	{"services", "nav.services", lucide.Server},
	{"library", "nav.library", lucide.Layers},
	{"reports", "nav.reports", lucide.FileText},
}

type WindowHooks struct {
	BindTray        func(win *mygo.Window) func(bool) error
	OnLocaleChanged func(locale string)
}

func New(ctx context.Context, service *core.Service, page string, hooks ...WindowHooks) func(core.Event) bool {
	ctx, cancel := context.WithCancel(ctx)
	title := "InferYard Studio"
	if paths := service.BoundPaths(); paths != nil {
		title = paths.WindowTitle
	}
	initialTheme := service.Theme()
	initialLanguage := service.Language()
	initialMinimizeToTray := service.MinimizeToTray()
	initialKeepServices := service.EffectiveKeepServicesOnQuit()
	syncNativeTheme(initialTheme)
	syncNativeLocale(initialLanguage)
	s := &viewState{
		ctx:                   ctx,
		service:               service,
		selected:              firstOpen(page, service.FirstRun()),
		windowTitle:           title,
		denied:                map[string]bool{},
		speed:                 &speedWorkspace{context: inferyard.ContextSpeed64K},
		themeMode:             initialTheme,
		appliedThemeMode:      initialTheme,
		languageMode:          initialLanguage,
		appliedLanguageMode:   initialLanguage,
		minimizeToTray:        initialMinimizeToTray,
		appliedMinimizeToTray: false,
		keepServicesOnQuit:    initialKeepServices,
		settings: core.SettingsResponse{
			Settings:  core.UserSettings{Theme: initialTheme, Language: initialLanguage, MinimizeToTray: initialMinimizeToTray},
			Effective: core.EffectiveSettings{Theme: initialTheme, Language: initialLanguage, MinimizeToTray: initialMinimizeToTray, KeepServicesOnQuit: initialKeepServices},
		},
	}
	options := pageShellOptions(title, "inferyard-studio-main", s.leftNav, s.contentView)
	options.Theme = s.themeFor
	shell := sidebar.NewShell(options)
	w := shell.Window()
	s.win = w
	if len(hooks) > 0 && hooks[0].BindTray != nil {
		s.applyMinimizeToTray = hooks[0].BindTray(w)
		_ = s.syncMinimizeToTrayIfNeeded()
	} else {
		s.appliedMinimizeToTray = initialMinimizeToTray
	}
	var localeHook func(string)
	if len(hooks) > 0 {
		localeHook = hooks[0].OnLocaleChanged
	}
	offLocale := mygo.App.OnLocaleChanged(func(locale string) {
		if localeHook != nil {
			localeHook(locale)
		}
		go s.refresh(true)
	})
	offHide := w.OnHide(func() {
		if s.openedReport != nil {
			s.openedReport.releaseSVGStage()
		}
	})
	offShow := w.OnShow(func() {
		if s.selected == "reports" && s.openedReport != nil {
			w.Invalidate()
		}
	})
	offClose := w.OnClose(s.handleWindowClose)
	w.OnClosed(func() {
		offClose()
		offHide()
		offShow()
		s.closeOpenedReport()
		offLocale()
		cancel()
		s.waitMinimizeToTrayPersist()
	})
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
				if needsRefresh(events, reset) {
					s.refresh(fullRefresh(events, reset))
				}
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
		return "services"
	}
	return "services"
}

func canonicalPageID(page string) string {
	page = strings.TrimPrefix(strings.TrimSpace(page), "/")
	switch page {
	case "benchmarks", "services", "library", "reports", "settings":
		return page
	case "dashboard":
		return "benchmarks"
	case "models", "downloads", "device":
		return "library"
	default:
		return "services"
	}
}

func pageID(page string) string {
	return canonicalPageID(page)
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
	serveSupported                                                                      bool
	standalone                                                                          standaloneServeState
	recentServices                                                                      []core.ServicePreset
	exitedNotice                                                                        *core.ServiceExitNotice
	serviceUsage                                                                        serviceUsageDrawerState
	serviceParams                                                                       serviceParamsDrawerState
	quitDialog                                                                          quitServiceDialogState
	installationPath, localPID, localEndpoint, localBindingTask                         string
	wizardOpenRequested                                                                 bool
	wizardPresetFamily                                                                  string
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
	engineState                                                                         core.EngineState
	startup                                                                             core.Startup
	processes                                                                           []core.ProcessCandidate
	runProgress                                                                         *core.RunProgress
	ctx                                                                                 context.Context
	service                                                                             *core.Service
	win                                                                                 *mygo.Window
	openedReport                                                                        *reportViewState
	newSVGStage                                                                         func() (svgStageView, error)
	reportsTab                                                                          int
	settingsTab                                                                         int
	buildInfo                                                                           func() core.BuildInfo
	refreshMu                                                                           sync.Mutex
	detailMu                                                                            sync.Mutex
	requestedDetail                                                                     string
	selected, detailID                                                                  string
	libraryTab                                                                          int
	libraryHwExpanded                                                                   bool
	benchFlowOpen                                                                       bool
	sidebarServiceOpen, sidebarQueueOpen, sidebarDownloadOpen                           bool
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
	workspace                                                                           string
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
	settingsLeaveConfirm                                                                bool
	pendingNavAction                                                                    func()
	endpoint, pid                                                                       string
	denied                                                                              map[string]bool
	bindFailure                                                                         *core.ApiError
	failedBundle                                                                        string
	handoffFailure                                                                      *core.ApiError
	engineFailureCleared                                                                string
	localUsedPIDs                                                                       map[int64]bool
	localPendingPIDs                                                                    map[string]int64
	tokenEnv, downloadProxy                                                             string
	themeMode, appliedThemeMode                                                         string
	themeSaving                                                                         bool
	themeMu                                                                             sync.Mutex
	themeWanted                                                                         string
	themeWantedSeq                                                                      uint64
	themeWorkerRunning                                                                  bool
	applyNativeTheme                                                                    func(string)
	persistTheme                                                                        func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	languageMode, appliedLanguageMode                                                   string
	languageSaving                                                                      bool
	languageMu                                                                          sync.Mutex
	languageWanted                                                                      string
	languageWantedSeq                                                                   uint64
	languageWorkerRunning                                                               bool
	applyNativeLocale                                                                   func(string)
	persistLanguage                                                                     func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	minimizeToTray, appliedMinimizeToTray                                               bool
	trayUnavailable                                                                     bool
	minimizeToTraySaving                                                                bool
	minimizeToTrayMu                                                                    sync.Mutex
	minimizeToTrayWanted                                                                bool
	minimizeToTrayWantedSeq                                                             uint64
	minimizeToTrayWorkerRunning                                                         bool
	minimizeToTrayWorkerDone                                                            chan struct{}
	applyMinimizeToTray                                                                 func(bool) error
	persistMinimizeToTray                                                               func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	keepServicesOnQuit                                                                  bool
	keepServicesSaving                                                                  bool
	keepServicesMu                                                                      sync.Mutex
	keepServicesWanted                                                                  bool
	keepServicesWantedSeq                                                               uint64
	keepServicesWorkerRunning                                                           bool
	keepServicesWorkerDone                                                              chan struct{}
	persistKeepServices                                                                 func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	historyTable                                                                        pageTableState
	downloadsUI                                                                         *downloadsUIState
	engineCatalog                                                                       *core.EngineCatalogRegistry
	pendingPolystrataSelection                                                          *polystrataSelectionTarget
	reportChoices                                                                       ui.Selection[any]
	scrolls                                                                             map[string]*ui.ScrollState
	casesTable, samplesTable                                                            pageTableState
	resourceTablePage                                                                   int
	settingsEpochMu                                                                     sync.Mutex
	settingsEpoch                                                                       uint64
	updateState                                                                         core.UpdateState
	cliUpgrade                                                                          core.CLIUpgradeState
	cliUpgradeDismissedSeq                                                              uint64
	updateDialogOpen                                                                    bool
	updateReadyPromptDismissedVersion                                                   string
	updateCheckFn                                                                       func(context.Context) (core.UpdateState, error)
	updateStartFn                                                                       func()
	updateCancelFn                                                                      func() core.UpdateState
	updateApplyFn                                                                       func(context.Context, bool) (core.UpdateState, error)
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
	if active := activeRunTask(tasks); active != nil {
		if d, e := s.service.RunDetail(active.TaskId); e == nil {
			runProgress = d.Progress
		}
	}
	settings = s.service.Settings()
	updateState := s.service.UpdateStatus()
	cliUpgrade := s.service.CLIUpgradeStatus()
	if full {
		history, he = s.service.History()
		_, reports, re = s.service.Reports()
	}
	ready = s.service.Readiness()
	if ready.CLI.Status == "ok" && ready.Workspace.Initialized {
		progress, pe = s.service.Progress(s.ctx)
	}
	dashboard = s.readDashboard(progress.Progress)
	var registry core.Registry
	if full {
		registry = s.service.Registry()
	}
	serveSupported := s.service.ServeSupported(s.ctx)
	recentServices := s.service.RecentServices()
	exitedNotice := s.service.ExitedServiceNotice()
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
			updateState: updateState, cliUpgrade: cliUpgrade, serveSupported: serveSupported,
			recentServices: recentServices, exitedNotice: exitedNotice,
		}
		if !s.applyRefreshBatch(batch) {
			return
		}
		curPage := canonicalPageID(s.selected)
		verifyVisible = curPage == "library" ||
			(curPage == "benchmarks" && s.progress != nil && !s.progress.DefaultScopeComplete && s.progress.NextAction == "prepare_candidate")
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
	updateState                core.UpdateState
	cliUpgrade                 core.CLIUpgradeState
	serveSupported             bool
	recentServices             []core.ServicePreset
	exitedNotice               *core.ServiceExitNotice
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
	skipEngineAdopt := false
	skipModelAdopt := false
	if b.full {
		if b.historyError == nil {
			s.history = b.history
		}
		if b.reportsError == nil {
			s.reports = b.reports
		}
		s.historyError, s.reportsError = b.historyError, b.reportsError
		prevEngineName, prevModelName := s.engine, s.model
		prevEnginePath := s.selectedEnginePath()
		prevModelPath := s.selectedModelPath()
		s.registry = b.registry
		if s.applyPendingPolystrataSelection(prevEngineName, prevModelName) {
			skipEngineAdopt = true
			skipModelAdopt = true
		} else {
			skipEngineAdopt = s.reconcileSelectedEngine(prevEnginePath)
			skipModelAdopt = s.reconcileSelectedModel(prevModelPath)
		}
	}
	s.ready, s.progress = b.ready, b.progress.Progress
	s.applyDashboard(b.dashboard, b.progressError)
	s.engineState = s.service.Engine()
	s.checks = s.service.EngineChecks()
	s.serveSupported = b.serveSupported
	s.recentServices = b.recentServices
	s.exitedNotice = b.exitedNotice
	s.applyUpdateState(b.updateState)
	s.applyCLIUpgradeState(b.cliUpgrade)
	if !skipEngineAdopt {
		s.adoptDownloadedEngine()
	}
	if !skipModelAdopt {
		s.adoptOnlyModel()
	}
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
		if s.ctx != nil && s.ctx.Err() != nil {
			return
		}
		if s.updateWindow != nil {
			s.updateWindow(func() { s.finishWork(message, e) })
		} else if s.win != nil {
			s.win.Update(func() { s.finishWork(message, e) })
		} else {
			return
		}
		if s.win != nil {
			s.refresh(true)
		}
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
}

func (s *viewState) contentView(c *ui.Context) {
	page := canonicalPageID(s.selected)
	if page != "reports" && s.openedReport != nil && s.openedReport.svgStage != nil {
		s.openedReport.releaseSVGStage()
	}
	s.consumeFeedback(c)
	s.dropStaleStopPrompt()
	s.clearPromptOnNavigation()
	ui.Column(c).Grow(1).MinWidth(0).Fill().Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		ui.Scroll(c.Key(page)).Grow(1).TrackScroll(s.pageScroll()).Padding(tokens(c).Density.Page).Children(func() {
			ui.Column(c).Gap(tokens(c).Density.Section).Children(func() {
				s.feedback(c)
				if !s.loaded {
					s.loadingView(c)
					return
				}
				switch page {
				case "services":
					s.servicesView(c)
				case "library":
					s.libraryView(c)
				case "benchmarks":
					if s.detailID != "" {
						s.detailView(c)
					} else {
						s.benchmarksView(c)
					}
				case "reports":
					s.reportsView(c)
				case "settings":
					s.settingsView(c)
				default:
					s.servicesView(c)
				}
			})
		})
	})
	q := s.queueActivity()
	if s.destructiveDialogModal(c, destructiveOption{
		kind: destructiveStopEngine, id: "header", label: tr(c, "shell.header.stop_engine.label"), confirmLabel: tr(c, "shell.header.stop_engine.confirm"),
		prompt:   tr(c, "common.confirm.stop_engine.prompt"),
		disabled: !s.stopEnabled(true, q.active()),
	}) {
		s.stopEngine()
	}
	s.resumeDialog(c)
	s.settingsDiscardDialog(c)
	s.standaloneDialogs(c)
	s.serviceDrawersAndDialogs(c)
	s.updateDialog(c)
	s.updateReadyPrompt(c)
	s.studioNotifications(c)
	studioToasts(c)
}
