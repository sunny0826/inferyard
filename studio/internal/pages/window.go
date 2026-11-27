package pages

import (
	"context"
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
	MinimizeToTray  func() error
	OnLocaleChanged func(locale string)
}

func New(ctx context.Context, service *core.Service, page string, hooks ...WindowHooks) func(core.Event) bool {
	ctx, cancel := context.WithCancel(ctx)
	title := "InferYard Studio"
	if paths := service.BoundPaths(); paths != nil {
		title = paths.WindowTitle
	}
	initialTheme := service.Theme()
	initialAccent := service.Accent()
	initialLanguage := service.Language()
	initialReasoning := service.ReasoningEffort()
	initialMinimizeToTray := service.MinimizeToTray()
	initialKeepServices := service.EffectiveKeepServicesOnQuit()
	syncNativeTheme(initialTheme)
	setActiveAccent(initialAccent)
	syncNativeLocale(initialLanguage)
	s := &viewState{
		ctx:         ctx,
		service:     service,
		selected:    firstOpen(page, service.FirstRun()),
		windowTitle: title,
		denied:      map[string]bool{},
		speed:       &speedWorkspace{context: inferyard.ContextSpeed64K},
		settingsViewState: settingsViewState{
			themeMode:             initialTheme,
			appliedThemeMode:      initialTheme,
			accentMode:            initialAccent,
			languageMode:          initialLanguage,
			appliedLanguageMode:   initialLanguage,
			reasoningEffort:       initialReasoning,
			minimizeToTray:        initialMinimizeToTray,
			appliedMinimizeToTray: false,
		},
		keepServicesOnQuit: initialKeepServices,
		settings: core.SettingsResponse{
			Settings:  core.UserSettings{Theme: initialTheme, Accent: initialAccent, Language: initialLanguage, ReasoningEffort: initialReasoning, MinimizeToTray: initialMinimizeToTray},
			Effective: core.EffectiveSettings{Theme: initialTheme, Accent: initialAccent, Language: initialLanguage, ReasoningEffort: initialReasoning, MinimizeToTray: initialMinimizeToTray, KeepServicesOnQuit: initialKeepServices},
		},
	}
	shell := sidebar.NewShell(s.shellOptions(title))
	w := shell.Window()
	s.win = w
	if len(hooks) > 0 {
		s.minimizeToTrayNow = hooks[0].MinimizeToTray
	}
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
		s.bootstrap()
		s.refreshMask(refreshAll)
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
				if mask := refreshMaskFor(events, reset); mask != refreshNone {
					s.refreshMask(mask)
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
	settingsViewState
	speed                                                                               *speedWorkspace
	preview                                                                             previewMode
	lastEngineVerification                                                              time.Time
	engineVerifyPending, skipAutoModelAdopt                                             bool
	discoveryCLI                                                                        string
	binding                                                                             bool
	bindingBundle                                                                       string
	discovery                                                                           inferyard.ServiceDiscovery
	discoveryError                                                                      *core.ApiError
	discovering, discoveryLoaded                                                        bool
	registry                                                                            core.Registry
	checks                                                                              core.EngineCheckOverview
	serveSupported, serveProbing                                                        bool
	standalone                                                                          standaloneServeState
	recentServices                                                                      []core.ServicePreset
	exitedNotice                                                                        *core.ServiceExitNotice
	managedServices                                                                     []core.EngineState
	exitedNotices                                                                       []core.ServiceExitNotice
	dismissingNoticeIDs                                                                 map[string]bool
	dismissingAllNotices                                                                bool
	memorySummary                                                                       core.ServiceMemoryBudget
	hwMemoryCache                                                                       uiHardwareMemoryCache
	expandedServices                                                                    map[string]bool
	serviceLogTargetID                                                                  string
	serviceUsage                                                                        serviceUsageDrawerState
	serviceParams                                                                       serviceParamsDrawerState
	quitDialog                                                                          quitServiceDialogState
	extCheckDialog                                                                      extConnCheckDialogState
	pendingExclusivity                                                                  string
	pendingExclusivityModel                                                             string
	pendingExclusivityEngine                                                            string
	pendingExclusivityMode                                                              string
	modelEvidence                                                                       map[string]core.ModelEvidenceSummary
	engineUpdates                                                                       map[string]core.EngineUpdateInfo
	engineUpdateStatus                                                                  core.EngineUpdatesStatus
	engineUpdateNotifiedSig                                                             string
	engineUpdateCheckFn                                                                 func(context.Context) (core.EngineUpdatesStatus, *core.ApiError)
	engineUpdateRunFn                                                                   func(string) (core.DownloadTask, *core.ApiError)
	reportExclusivities                                                                 map[string]string
	probeServiceConnFunc                                                                func(context.Context, string) core.ExternalConnProbe
	probeEndpointConnFunc                                                               func(context.Context, string) core.ExternalConnProbe
	installationPath, localPID, localEndpoint, localBindingTask                         string
	wizardOpenRequested                                                                 bool
	wizardPresetFamily                                                                  string
	model                                                                               string
	announcedPage, promptPage                                                           string
	announcedErrors                                                                     map[string]bool
	refreshing                                                                          bool
	initDir, engine, process, bindEndpoint, candidate, cliInstallStatus, refreshFailure, refreshFailureCode string
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
	benchmarksTab                                                                       int
	reportsTab                                                                          int
	refreshMu                                                                           sync.Mutex
	bgRefresh                                                                           bgRefreshCache
	detailMu                                                                            sync.Mutex
	snapshotMu                                                                          sync.Mutex
	snapshotSeq                                                                         uint64
	appliedSnapshotSeq                                                                  uint64
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
	loaded, hydrating, busy, settingsLoaded                                             bool
	probingReuse                                                                        bool
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
	pendingNavAction                                                                    func()
	endpoint, pid                                                                       string
	denied                                                                              map[string]bool
	bindFailure                                                                         *core.ApiError
	failedBundle                                                                        string
	handoffFailure                                                                      *core.ApiError
	engineFailureCleared                                                                string
	localUsedPIDs                                                                       map[int64]bool
	localPendingPIDs                                                                    map[string]int64
	keepServicesOnQuit                                                                  bool
	historyTable                                                                        pageTableState
	historyQuery, historyStatusFilter, historyDetailPath                                string
	downloadsUI                                                                         *downloadsUIState
	engineCatalog                                                                       *core.EngineCatalogRegistry
	pendingPolystrataSelection                                                          *polystrataSelectionTarget
	reportChoices                                                                       ui.Selection[any]
	scrolls                                                                             map[string]*ui.ScrollState
	casesTable, samplesTable                                                            pageTableState
	casesTablePage, resourceTablePage                                                   int
	resourceCache                                                                       resourceViewCache
	reportLoadSeq                                                                       uint64
	processesLoading                                                                    bool
	processesLoadedAt                                                                   time.Time
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
	if s.detailID != id {
		s.casesTablePage = 0
		s.resourceTablePage = 0
	}
	if s.preview != nil {
		s.detailID, s.detail, s.detailError = id, s.preview.detail(id), nil
		return
	}
	s.detailID = id
	s.detailMu.Lock()
	s.requestedDetail = id
	s.detailMu.Unlock()
	if s.service != nil {
		d, e := s.service.RunDetail(id)
		if e == nil {
			s.detail, s.detailError = &d, nil
		} else {
			s.detail, s.detailError = nil, e
		}
		return
	}
	s.detail, s.detailError = nil, nil
}

func (s *viewState) shellOptions(title string) sidebar.ShellOptions {
	options := pageShellOptions(title, "inferyard-studio-main", s.leftNav, s.contentView)
	options.ContentTitle = func(c *ui.Context) { s.shellContentTitle(c, nil) }
	options.Theme = s.themeFor
	return options
}

func (s *viewState) view(c *ui.Context) {
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(205).Shrink(0).Children(func() { s.leftNav(c) })
		ui.Box(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
			s.contentView(c)
			ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(sidebar.HeaderHeight(c)).
				AlignItems(ui.Center).Padding(6, 16, 6, 16).Children(func() {
				s.shellContentTitle(c, nil)
			})
		})
	})
}
