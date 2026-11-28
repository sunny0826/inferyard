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
