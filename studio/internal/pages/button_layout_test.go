package pages

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestReportsHeaderCreateButtonAboveTabs(t *testing.T) {
	s := readyState("reports")
	tt := ui.NewTester(s.view, 1120, 900)
	tt.Frame()

	title, hasTitle := tt.AccessByRole(ui.RoleHeading, tr(nil, "reports.title"))
	createBtn, hasCreate := tt.AccessByRole(ui.RoleButton, tr(nil, "reports.action.create_from_history"))
	tabBtn, hasTab := tt.AccessByRole(ui.RoleTab, tr(nil, "reports.tab.reports"))
	if !hasTitle || !hasCreate || !hasTab {
		t.Fatalf("missing reports header elements: title=%v create=%v tab=%v texts=%v", hasTitle, hasCreate, hasTab, tt.Texts())
	}
	if createBtn.Bounds.X < title.Bounds.X+title.Bounds.W {
		t.Fatalf("create button should sit to the right of title: title=%+v btn=%+v", title.Bounds, createBtn.Bounds)
	}
	titleMidY := title.Bounds.Y + title.Bounds.H/2
	btnMidY := createBtn.Bounds.Y + createBtn.Bounds.H/2
	if math.Abs(float64(btnMidY-titleMidY)) > 8 {
		t.Fatalf("create button should align on header row with title: title=%+v btn=%+v", title.Bounds, createBtn.Bounds)
	}
	if createBtn.Bounds.Y+createBtn.Bounds.H > tabBtn.Bounds.Y {
		t.Fatalf("create button should sit above tabs: btn=%+v tab=%+v", createBtn.Bounds, tabBtn.Bounds)
	}
}

func TestLibraryEnginesHeaderOrderAndSingleCollapse(t *testing.T) {
	s := readyState("library")
	s.libraryTab = libraryTabEngines
	tt := ui.NewTester(s.view, 1120, 900)
	tt.Frame()

	checkBtn, okCheck := tt.AccessByRole(ui.RoleButton, tr(nil, "models.engine.action.check_updates"))
	regBtn, okReg := tt.AccessByRole(ui.RoleButton, tr(nil, "models.action.register_local_engine"))
	if !okCheck || !okReg {
		t.Fatalf("missing engine header buttons: check=%v reg=%v texts=%v", okCheck, okReg, tt.Texts())
	}
	if checkBtn.Bounds.X+checkBtn.Bounds.W > regBtn.Bounds.X {
		t.Fatalf("update check button should sit left of register button: check=%+v reg=%+v", checkBtn.Bounds, regBtn.Bounds)
	}

	click(t, tt, tr(nil, "models.action.register_local_engine"))
	collapseCount := 0
	for _, item := range tt.AccessAll() {
		if item.Role == ui.RoleButton && item.Name == tr(nil, "models.action.collapse") {
			collapseCount++
		}
	}
	if collapseCount != 1 {
		t.Fatalf("expected exactly 1 inner collapse button when wizard expanded, got %d", collapseCount)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, tr(nil, "models.action.collapse_local_engine")); !ok {
		t.Fatal("expected header toggle button to switch to collapse_local_engine")
	}
}

func TestResumeDialogAndUpdateDialogFooterOrder(t *testing.T) {
	s := testState()
	bundle := "zh-core"
	s.resumeSource = &core.RunSummary{RunId: "run-1", Path: "/runs/run-1", Bundle: &bundle}
	rtt := ui.NewTester(func(c *ui.Context) { s.resumeDialog(c) }, 900, 700)
	rtt.Frame()

	closeBtn, okClose := rtt.AccessByRole(ui.RoleButton, tr(nil, "dashboard.resume.close_button"))
	confirmBtn, okConfirm := rtt.AccessByRole(ui.RoleButton, tr(nil, "dashboard.resume.confirm_button"))
	if !okClose || !okConfirm {
		t.Fatalf("missing resume footer buttons: close=%v confirm=%v texts=%v", okClose, okConfirm, rtt.Texts())
	}
	if closeBtn.Bounds.X+closeBtn.Bounds.W > confirmBtn.Bounds.X {
		t.Fatalf("resume close button should sit left of confirm button: close=%+v confirm=%+v", closeBtn.Bounds, confirmBtn.Bounds)
	}

	su := readyState("reports")
	su.updateDialogOpen = true
	su.updateState = core.UpdateState{
		State:          core.UpdateReadyToApply,
		CurrentVersion: "0.9.0",
		Release:        &core.UpdateRelease{Version: "0.10.0"},
	}
	utt := ui.NewTester(func(c *ui.Context) { su.updateDialog(c) }, 900, 700)
	utt.Frame()

	laterBtn, okLater := utt.AccessByRole(ui.RoleButton, tr(nil, "update.action.later"))
	applyBtn, okApply := utt.AccessByRole(ui.RoleButton, tr(nil, "update.action.update_and_restart"))
	if !okLater || !okApply {
		t.Fatalf("missing update footer buttons: later=%v apply=%v texts=%v", okLater, okApply, utt.Texts())
	}
	if laterBtn.Bounds.X+laterBtn.Bounds.W > applyBtn.Bounds.X {
		t.Fatalf("update later button should sit left of apply CTA: later=%+v apply=%+v", laterBtn.Bounds, applyBtn.Bounds)
	}
}

func TestSettingsWorkspaceFooterSeparatesSaveAndCleanup(t *testing.T) {
	s := settingsState()
	tt := settingsTester(s)
	tt.Frame()

	saveBtn, okSave := tt.AccessByRole(ui.RoleButton, tr(nil, "common.save"))
	cleanBtn, okClean := tt.AccessByRole(ui.RoleButton, tr(nil, "settings.workspace.cleanup.action"))
	if !okSave || !okClean {
		t.Fatalf("missing workspace footer buttons: save=%v clean=%v texts=%v", okSave, okClean, tt.Texts())
	}
	if cleanBtn.Bounds.X-(saveBtn.Bounds.X+saveBtn.Bounds.W) < 100 {
		t.Fatalf("save and destructive cleanup buttons should be separated by spacer: save=%+v clean=%+v", saveBtn.Bounds, cleanBtn.Bounds)
	}
}

func TestEmptyEngineLinksNavigateToLibrary(t *testing.T) {
	s := readyState("benchmarks")
	s.registry.Models = []core.ModelAsset{{Name: "qwen3", Available: true}}
	s.progress = &inferyard.WorkspaceProgress{
		Bundles: []inferyard.BundleProgress{{Bundle: "zh-core"}},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		if s.selected == "library" {
			s.libraryView(c)
			return
		}
		s.taskAssetsView(c)
	}, 1120, 900)
	tt.Frame()

	click(t, tt, tr(nil, "onboarding.assets.use_existing"))
	if s.selected != "library" || s.libraryTab != libraryTabEngines || !tt.HasText(tr(nil, "models.wizard.card_title")) {
		t.Fatalf("expected onboarding existing-asset action to open library wizard: selected=%q tab=%d texts=%v", s.selected, s.libraryTab, tt.Texts())
	}

	sd := readyState("benchmarks")
	sd.registry.Models = []core.ModelAsset{{Name: "qwen3", Available: true}}
	sd.progress = &inferyard.WorkspaceProgress{
		Bundles: []inferyard.BundleProgress{{Bundle: "zh-core"}},
	}
	dtt := ui.NewTester(func(c *ui.Context) {
		if sd.selected == "library" {
			sd.libraryView(c)
			return
		}
		sd.candidateFields(c)
	}, 1120, 900)
	dtt.Frame()

	click(t, dtt, tr(nil, "dashboard.candidate.register_engine"))
	if sd.selected != "library" || sd.libraryTab != libraryTabEngines || !dtt.HasText(tr(nil, "models.wizard.card_title")) {
		t.Fatalf("expected candidate register-engine action to open library wizard: selected=%q tab=%d texts=%v", sd.selected, sd.libraryTab, dtt.Texts())
	}
}

func TestBenchmarkTaskButtonsPlacement(t *testing.T) {
	s := readyState("benchmarks")
	s.tasks = []core.RunTask{{TaskId: "task-1", Kind: "run", State: "running", Bundle: ptr("zh-core")}}
	s.runProgress = &core.RunProgress{Phase: ptr("formal"), Done: 30, Total: ptr(int64(120))}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Children(func() { s.runView(c) }) }, 1120, 900)
	tt.Frame()

	bundleRect, okTitle := tt.Find("zh-core")
	detailBtn, okDetail := tt.AccessByRole(ui.RoleButton, tr(nil, "dashboard.run.view_live_detail"))
	cancelBtn, okCancel := tt.AccessByRole(ui.RoleButton, tr(nil, "common.confirm.cancel_run.label"))
	statusRect, okStatus := tt.Find(trf(nil, "dashboard.run.cases_progress", "done", 30, "total", 120))
	if !okTitle || !okDetail || !okCancel || !okStatus {
		t.Fatalf("missing run elements: title=%v detail=%v cancel=%v status=%v texts=%v", okTitle, okDetail, okCancel, okStatus, tt.Texts())
	}
	if detailBtn.Bounds.X < bundleRect.X+bundleRect.W || detailBtn.Bounds.X+detailBtn.Bounds.W > cancelBtn.Bounds.X {
		t.Fatalf("run action icons should sit right of title with cancel rightmost: title=%+v detail=%+v cancel=%+v", bundleRect, detailBtn.Bounds, cancelBtn.Bounds)
	}
	if math.Abs(float64(detailBtn.Bounds.Y-cancelBtn.Bounds.Y)) > 2 || detailBtn.Bounds.Y+detailBtn.Bounds.H > statusRect.Y {
		t.Fatalf("run action icons should sit on header row above status strip: detail=%+v cancel=%+v status=%+v", detailBtn.Bounds, cancelBtn.Bounds, statusRect)
	}

	sc := readyState("benchmarks")
	sc.task.stage = "done"
	sc.task.report = "/reports/rep-1"
	ctt := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Children(func() { sc.completionView(c) }) }, 1120, 900)
	ctt.Frame()

	againBtn, okAgain := ctt.AccessByRole(ui.RoleButton, tr(nil, "dashboard.completion.run_again"))
	viewBtn, okView := ctt.AccessByRole(ui.RoleButton, tr(nil, "dashboard.completion.view_report"))
	if !okAgain || !okView {
		t.Fatalf("missing completion action icons: again=%v view=%v texts=%v", okAgain, okView, ctt.Texts())
	}
	if againBtn.Bounds.X+againBtn.Bounds.W > viewBtn.Bounds.X || againBtn.Bounds.X <= 500 {
		t.Fatalf("completion actions should be right-aligned with primary report CTA rightmost: again=%+v view=%+v", againBtn.Bounds, viewBtn.Bounds)
	}

	sh := readyState("benchmarks")
	sh.history.StandaloneRuns = []core.RunSummary{{
		RunId: "run-1", Path: "/results/run-1", ModelName: ptr("Demo.gguf"),
		Bundle: ptr("zh-core"), Status: "complete", Sealed: true,
	}}
	sh.reports = []core.ReportSummary{{ReportId: "rep-1", RunIds: []string{"run-1"}}}
	sh.historyDetailPath = "/results/run-1"
	htt := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Children(func() { sh.historyOverlays(c) }) }, 1120, 900)
	htt.Frame()

	rerunBtn, okRerun := htt.AccessByRole(ui.RoleButton, tr(nil, "dashboard.history.rerun_same"))
	histViewBtn, okHistView := htt.AccessByRole(ui.RoleButton, tr(nil, "dashboard.completion.view_report"))
	pathRect, okPath := htt.Find(tr(nil, "dashboard.history.run_path_label"))
	if !okRerun || !okHistView || !okPath {
		t.Fatalf("missing history drawer elements: rerun=%v view=%v path=%v texts=%v", okRerun, okHistView, okPath, htt.Texts())
	}
	if rerunBtn.Bounds.X+rerunBtn.Bounds.W > histViewBtn.Bounds.X || histViewBtn.Bounds.Y < pathRect.Y+pathRect.H {
		t.Fatalf("history drawer actions should sit in bottom footer below scroll content: rerun=%+v view=%+v path=%+v", rerunBtn.Bounds, histViewBtn.Bounds, pathRect)
	}
}
