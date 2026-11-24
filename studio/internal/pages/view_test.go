package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func ptr[T any](v T) *T { return &v }

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func testState() viewState {
	return viewState{
		selected: "dashboard", loaded: true, settingsLoaded: true, denied: map[string]bool{},
		ready: core.Readiness{
			CLI:       core.InferyardProbe{Status: "ok", Version: ptr("1.0")},
			Workspace: core.DirectoryReadiness{Initialized: true},
			Results:   core.DirectoryReadiness{Exists: true},
			Models:    core.DirectoryReadiness{Exists: true},
		},
		progress: &inferyard.WorkspaceProgress{
			NextAction: "bind",
			Bundles: []inferyard.BundleProgress{
				{Bundle: "zh-core", Phase: "run_complete", BoundPath: ptr("/w/bound/zh-core-1")},
				{Bundle: "zh-svg-pelican", Phase: "bound_missing"},
			},
		},
	}
}

func readyState(selected string) *viewState {
	return &viewState{
		selected: selected, loaded: true, denied: map[string]bool{},
		ready: core.Readiness{
			CLI:       core.InferyardProbe{Status: "ok"},
			Workspace: core.DirectoryReadiness{Initialized: true},
			Results:   core.DirectoryReadiness{Exists: true},
			Models:    core.DirectoryReadiness{Exists: true},
		},
	}
}

func sidebarHeaderState(s *viewState) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(205).Shrink(0).Children(func() { s.leftNav(c) })
			ui.Column(c).Grow(1).Children(func() { s.cornerStatus(c) })
		})
	}, 1120, 700)
}

func settingsState() *viewState {
	s := testState()
	s.selected = "settings"
	s.settings = core.SettingsResponse{
		Settings: core.UserSettings{
			WorkspaceRoot: ptr("/saved/workspace"), ResultsRoot: ptr("/saved/results"),
			ModelsRoot: ptr("/saved/models"), ModelDownloadName: "saved-model", EngineDownloadName: "saved-engine",
		},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/saved/workspace")},
	}
	s.loadSettings(s.settings)
	s.settingsLoaded = true
	return &s
}

func settingsTester(s *viewState) *ui.Tester {
	return ui.NewTester(s.settingsView, 1120, 1800)
}
