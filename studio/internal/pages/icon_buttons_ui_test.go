package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func requireIconButton(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleButton, label)
	if !ok {
		t.Fatalf("icon button %q not found: %v", label, tt.Texts())
	}
	if info.Bounds.W <= 0 || info.Bounds.H <= 0 || info.Bounds.W != info.Bounds.H {
		t.Fatalf("button %q is not a visible square icon button: %+v", label, info.Bounds)
	}
}

func TestCorePageIconButtons(t *testing.T) {
	t.Cleanup(l10n.SetTestLocale(l10n.LanguageEn))

	t.Run("Services", func(t *testing.T) {
		s := readyState("services")
		s.serveSupported = true
		state := core.EngineState{
			State:       "running",
			Origin:      core.EngineOriginStandalone,
			Family:      "llama-cpp",
			ModelName:   "model.gguf",
			Endpoint:    "http://127.0.0.1:8080",
			LogPath:     "/tmp/model.log",
			ContextSize: 8192,
		}
		s.expandedServices = map[string]bool{serviceKey(state): true}
		tt := ui.NewTester(func(c *ui.Context) {
			s.renderRunningServiceCard(c, state, 0, false)
		}, 1200, 800)
		tt.Frame()
		for _, key := range []string{
			"services.card.action.benchmark",
			"services.card.action.open_web",
			"services.card.action.usage",
			"services.card.action.params",
			"services.card.action.log",
			"services.card.action.stop",
		} {
			requireIconButton(t, tt, tr(nil, key))
		}
		click(t, tt, tr(nil, "services.card.action.usage"))
		if !s.serviceUsage.open || s.serviceUsage.model != "model.gguf" {
			t.Fatalf("usage icon did not open the expected drawer: %+v", s.serviceUsage)
		}
	})

	t.Run("Library", func(t *testing.T) {
		s := readyState("library")
		tt := ui.NewTester(s.libraryView, 1200, 1400)
		tt.Frame()
		for _, key := range []string{
			"models.action.rescan",
			"downloads.page.settings",
			"library.hw.expand",
			"models.action.add_model_dir",
		} {
			requireIconButton(t, tt, tr(nil, key))
		}
		click(t, tt, tr(nil, "library.hw.expand"))
		tt.Frame()
		if !s.libraryHwExpanded {
			t.Fatal("hardware expand icon did not expand the card")
		}
		requireIconButton(t, tt, tr(nil, "library.hw.collapse"))
	})

	t.Run("Models", func(t *testing.T) {
		s := readyState("models")
		s.serveSupported = true
		model := core.ModelAsset{Name: "model.gguf", Format: "gguf", Available: true}
		s.registry.Models = []core.ModelAsset{model}
		s.registry.Engines = []core.EngineAsset{{Name: "llama-ok", Family: "llama-cpp"}}
		s.checks = core.EngineCheckOverview{
			Supported: true,
			Engines:   map[string]core.EngineEligibility{"llama-ok": {State: core.EngineCheckEligible}},
		}
		wizardOpen := false
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Children(func() {
				s.modelsIconPageActions(c, &wizardOpen)
				ui.Row(c).Children(func() {
					s.renderModelStandalonePrimaryAction(c, model)
					modelTaskAction(c, s, model)
				})
			})
		}, 1200, 600)
		tt.Frame()
		for _, key := range []string{
			"models.action.download_more",
			"models.action.add_model_dir",
			"engine.wizard.title",
			"models.action.refresh",
		} {
			requireIconButton(t, tt, tr(nil, key))
		}
		startLabel := tr(nil, "models.serve.action.start") + " · " + model.Name
		requireIconButton(t, tt, startLabel)
		requireIconButton(t, tt, tr(nil, "models.action.benchmark_with_model")+" · "+model.Name)
		click(t, tt, startLabel)
		if !s.standalone.drawerOpen || s.standalone.modelName != model.Name {
			t.Fatalf("start icon did not open the expected model drawer: %+v", s.standalone)
		}
	})

	t.Run("Benchmarks", func(t *testing.T) {
		s := readyState("benchmarks")
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Children(func() { s.benchmarksListView(c) })
		}, 1200, 1000)
		tt.Frame()
		label := tr(nil, "benchmarks.action.new")
		requireIconButton(t, tt, label)
		click(t, tt, label)
		if !s.benchFlowOpen || s.task.stage == "" {
			t.Fatalf("new benchmark icon did not open the flow: stage=%q open=%v", s.task.stage, s.benchFlowOpen)
		}
	})

	t.Run("Reports", func(t *testing.T) {
		s := readyState("reports")
		s.reports = []core.ReportSummary{{ReportId: "report-1", RunIds: []string{"run-1"}}}
		tt := ui.NewTester(s.reportsView, 1200, 900)
		tt.Frame()
		createLabel := tr(nil, "reports.action.create_from_history")
		requireIconButton(t, tt, createLabel)
		click(t, tt, createLabel)
		if !s.reportDialogOpen {
			t.Fatal("create-from-history icon did not open the report drawer")
		}

		opened := 0
		cardTester := ui.NewTester(func(c *ui.Context) {
			reportCard(c, s.reports[0], core.ExclusivityUnknown, "", func() { opened++ })
		}, 1000, 600)
		cardTester.Frame()
		openLabel := trf(nil, "reports.list.open_aria", "id", "report-1")
		requireIconButton(t, cardTester, openLabel)
		click(t, cardTester, openLabel)
		if opened != 1 {
			t.Fatalf("open report icon clicked %d times, want 1", opened)
		}
	})

	t.Run("AboutSettings", func(t *testing.T) {
		s := settingsState()
		s.buildInfo = func() core.BuildInfo {
			return core.BuildInfo{Version: "1.2.3", GitCommit: "abcdef"}
		}
		tt := ui.NewTester(s.aboutSettingsView, 1000, 600)
		tt.Frame()
		label := tr(nil, "settings.about.copy")
		requireIconButton(t, tt, label)
		click(t, tt, label)
		if clipboard := tt.Clipboard(); !strings.Contains(clipboard, "v1.2.3") || !strings.Contains(clipboard, "abcdef") {
			t.Fatalf("copy version icon wrote unexpected clipboard contents: %q", clipboard)
		}
	})

	t.Run("BenchmarkTasksFlow", func(t *testing.T) {
		s := readyState("benchmarks")
		s.tasks = []core.RunTask{{TaskId: "t1", Kind: "run", State: "running", Bundle: ptr("zh-core")}}
		runTT := ui.NewTester(s.runView, 1120, 700)
		runTT.Frame()
		requireIconButton(t, runTT, tr(nil, "common.confirm.cancel_run.label"))

		sf := readyState("benchmarks")
		sf.engineState = core.EngineState{State: "failed", LogPath: "/tmp/engine.log"}
		failSvcTT := ui.NewTester(sf.serviceView, 1120, 700)
		failSvcTT.Frame()
		requireIconButton(t, failSvcTT, tr(nil, "dashboard.engine_log.open_button"))

		sr := readyState("benchmarks")
		sr.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:8080"}
		runSvcTT := ui.NewTester(sr.serviceView, 1120, 700)
		runSvcTT.Frame()
		requireIconButton(t, runSvcTT, tr(nil, "common.confirm.stop_engine.label"))

		sc := readyState("benchmarks")
		sc.task.stage = "done"
		sc.task.report = "/tmp/rep-1"
		compTT := ui.NewTester(sc.completionView, 1120, 700)
		compTT.Frame()
		requireIconButton(t, compTT, tr(nil, "dashboard.completion.run_again"))
		requireIconButton(t, compTT, tr(nil, "dashboard.completion.view_report"))

		sd := readyState("benchmarks")
		sd.detailID = "t1"
		sd.detail = &core.RunTaskDetail{Task: core.RunTask{TaskId: "t1", State: "running", Bundle: ptr("zh-core")}}
		detailTT := ui.NewTester(sd.detailView, 1120, 700)
		detailTT.Frame()
		requireIconButton(t, detailTT, tr(nil, "detail.cancel.label"))

		sa := readyState("benchmarks")
		sa.speed = &speedWorkspace{current: &speedTask{id: "s1", model: "Demo.gguf", engine: "llama", context: 65536, stage: speedMeasuring, completed: 1}}
		speedTT := ui.NewTester(sa.speedTaskView, 1120, 800)
		speedTT.Frame()
		requireIconButton(t, speedTT, tr(nil, "speed.cancel.label"))

		sa.speed.current.stage = speedDone
		sa.speed.current.completed = 5
		speedTT.Frame()
		requireIconButton(t, speedTT, tr(nil, "speed.action.new_with_combo"))
		requireIconButton(t, speedTT, tr(nil, "speed.action.view_result"))

		sres := readyState("reports")
		sres.speed = &speedWorkspace{opened: &speedTask{id: "s1", model: "Demo.gguf", engine: "llama", context: 65536, stage: speedDone, selectionModel: "Demo.gguf", selectionEngine: "llama", summary: &speedSample{}}}
		resTT := ui.NewTester(sres.speedResultView, 1120, 800)
		resTT.Frame()
		requireIconButton(t, resTT, tr(nil, "speed.action.new_with_combo"))

		sh := readyState("reports")
		selIdx := 0
		sh.speed = &speedWorkspace{history: []*speedTask{{id: "s1", model: "Demo.gguf", engine: "llama", context: 65536, stage: speedDone}}}
		sh.speed.historyTable.Selected = &selIdx
		histTT := ui.NewTester(sh.speedHistoryView, 1120, 800)
		histTT.Frame()
		requireIconButton(t, histTT, tr(nil, "speed.history.open_result"))
	})
}
