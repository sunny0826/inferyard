package pages

import (
	"context"
	"encoding/json"
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
	"inferyard/studio/internal/l10n"
)

var navigation = []struct {
	id, labelKey string
	icon         func() *ui.SVG
}{
	{"dashboard", "nav.dashboard", lucide.Play},
	{"reports", "nav.reports", lucide.FileText},
	{"models", "nav.models", lucide.Layers},
	{"downloads", "nav.downloads", lucide.Download},
	{"device", "nav.device", lucide.Cpu},
	{"settings", "nav.settings", lucide.Settings},
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
		settings: core.SettingsResponse{
			Settings:  core.UserSettings{Theme: initialTheme, Language: initialLanguage, MinimizeToTray: initialMinimizeToTray},
			Effective: core.EffectiveSettings{Theme: initialTheme, Language: initialLanguage, MinimizeToTray: initialMinimizeToTray},
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
	w.OnClosed(func() {
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
	reportsTab                                                                          int
	refreshMu                                                                           sync.Mutex
	detailMu                                                                            sync.Mutex
	requestedDetail                                                                     string
	selected, detailID                                                                  string
	settings                                                                            core.SettingsResponse
	ready                                                                               core.Readiness
	prepLocations                                                                       core.PreparationLocations
	prepLocationsLoaded                                                                 bool
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
	modelName, tokenEnv, engineName, downloadProxy                                      string
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
	for _, task := range tasks {
		if !terminal(task.State) {
			if d, e := s.service.RunDetail(task.TaskId); e == nil {
				runProgress = d.Progress
			}
			break
		}
	}
	settings = s.service.Settings()
	updateState := s.service.UpdateStatus()
	if full {
		history, he = s.service.History()
		_, reports, re = s.service.Reports()
	}
	ready = s.service.Readiness()
	var prepLocations core.PreparationLocations
	var hasPrepLocations bool
	if ready.CLI.Status != "ok" || !ready.Workspace.Initialized || !ready.Results.Exists || !ready.Models.Exists {
		prepLocations = s.service.PreparationLocations()
		hasPrepLocations = true
	}
	if ready.CLI.Status == "ok" && ready.Workspace.Initialized {
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
			ready: ready, prepLocations: prepLocations, hasPrepLocations: hasPrepLocations, progress: progress, progressError: pe, dashboard: dashboard, registry: registry,
			updateState: updateState,
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
	prepLocations              core.PreparationLocations
	hasPrepLocations           bool
	progress                   core.ProgressResponse
	progressError              *core.ApiError
	dashboard                  dashboardSnapshot
	registry                   core.Registry
	updateState                core.UpdateState
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
	skipModelAdopt := false
	if b.full {
		if b.historyError == nil {
			s.history = b.history
		}
		if b.reportsError == nil {
			s.reports = b.reports
		}
		s.historyError, s.reportsError = b.historyError, b.reportsError
		prevModelPath := s.selectedModelPath()
		s.registry = b.registry
		s.reconcileSelectedEngine()
		s.applyPendingPolystrataSelection()
		skipModelAdopt = s.reconcileSelectedModel(prevModelPath)
	}
	s.ready, s.progress = b.ready, b.progress.Progress
	if b.hasPrepLocations {
		s.prepLocations = b.prepLocations
		s.prepLocationsLoaded = true
	} else {
		s.prepLocations = core.PreparationLocations{}
		s.prepLocationsLoaded = false
	}
	s.applyDashboard(b.dashboard, b.progressError)
	s.engineState = s.service.Engine()
	s.checks = s.service.EngineChecks()
	s.applyUpdateState(b.updateState)
	s.adoptDownloadedEngine()
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

func (s *viewState) reconcileSelectedEngine() {
	if s.engine == "" {
		return
	}
	for _, eng := range s.registry.Engines {
		if eng.Name == s.engine {
			return
		}
	}
	s.engine = ""
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
	s.resumeDialog(c)
	s.settingsDiscardDialog(c)
	s.updateDialog(c)
	s.updateReadyPrompt(c)
	s.studioNotifications(c)
	studioToasts(c)
}

func (s *viewState) loadingView(c *ui.Context) {
	title := tr(c, "shell.loading.getting_started")
	if s.selected == "dashboard" {
		title = ""
		if s.detailID != "" {
			title = tr(c, "shell.page.run_detail")
		}
	}
	for _, n := range navigation {
		if n.id == s.selected {
			title = tr(c, n.labelKey)
		}
	}
	if title != "" {
		s.header(c, title)
	}
	if s.failure != nil || s.refreshFailure != "" {
		button(c, tr(c, "shell.loading.retry_page")).AlignSelf(ui.Start).OnClick(s.refreshAction)
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
		return l10n.TR("common.missing")
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

func (s *viewState) openLocalEngineWizard(family string) {
	s.wizardOpenRequested = true
	s.wizardPresetFamily = family
	s.navigateTo("models", "")
}
