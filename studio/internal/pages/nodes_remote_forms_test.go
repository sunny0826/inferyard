package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func remoteFormTestState(page string, online bool) *viewState {
	s := readyState(page)
	s.nodes.activeID = "node-a"
	s.nodes.snapshots = []core.NodeSnapshot{{
		Record: core.NodeRecord{NodeID: "node-a", DisplayName: "Linux GPU", Platform: "linux/amd64"}, Online: online,
	}}
	s.nodes.reportsLoaded = true
	return s
}

func clickNodeButton(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	button, ok := tt.AccessByRole(ui.RoleButton, label)
	if !ok {
		t.Fatalf("button %q not found: %v", label, tt.Texts())
	}
	tt.ClickAt(button.Bounds.X+button.Bounds.W/2, button.Bounds.Y+button.Bounds.H/2)
	tt.Frame()
}

func TestRemoteActionFormsAvailableWithEmptyLists(t *testing.T) {
	s := remoteFormTestState("services", true)
	tt := ui.NewTester(s.view, 1200, 1100)
	clickNodeButton(t, tt, tr(nil, "nodes.remote.start_service"))
	tt.Frame()
	if !tt.HasText(tr(nil, "nodes.remote.model_path")) || !tt.HasText(tr(nil, "nodes.remote.engine_path")) || !tt.HasText(tr(nil, "nodes.remote.context_size")) {
		t.Fatalf("service form missing (open=%v): %v", s.nodes.serviceFormOpen, tt.Texts())
	}

	s = remoteFormTestState("benchmarks", true)
	tt = ui.NewTester(s.view, 1200, 1100)
	clickNodeButton(t, tt, tr(nil, "nodes.remote.start_benchmark"))
	tt.Frame()
	if !tt.HasText(tr(nil, "nodes.remote.config_paths")) {
		t.Fatalf("benchmark form missing: %v", tt.Texts())
	}

	s = remoteFormTestState("reports", true)
	tt = ui.NewTester(s.view, 1200, 1100)
	clickNodeButton(t, tt, tr(nil, "nodes.reports.sync"))
	tt.Frame()
	if !tt.HasText(tr(nil, "nodes.reports.remote_id")) || s.nodes.reportSyncNodeID != "node-a" {
		t.Fatalf("report sync form missing: %v", tt.Texts())
	}
}

func TestRemoteActionsDisabledOffline(t *testing.T) {
	for _, tc := range []struct {
		page, label string
	}{
		{page: "services", label: tr(nil, "nodes.remote.start_service")},
		{page: "benchmarks", label: tr(nil, "nodes.remote.start_benchmark")},
	} {
		s := remoteFormTestState(tc.page, false)
		tt := ui.NewTester(s.view, 1200, 900)
		button, ok := tt.AccessByRole(ui.RoleButton, tc.label)
		if !ok || !button.Disabled || !tt.HasText(tr(nil, "nodes.remote.online_required")) {
			t.Fatalf("%s offline action: found=%v disabled=%v texts=%v", tc.page, ok, button.Disabled, tt.Texts())
		}
	}
}

func TestRemoteLibraryFiltersPlatformAndShowsInstall(t *testing.T) {
	s := remoteFormTestState("library", true)
	s.nodes.catalog = []core.EngineFamilyCatalog{{
		ID: "llama-cpp", Name: "llama.cpp",
		Releases: []core.EngineRelease{{
			ID: "release-a", Version: "1.0",
			Builds: []core.EnginePlatformBuild{
				{ID: "linux", Platform: "linux/amd64", Variant: "CUDA", AcquireMode: core.EngineAcquireDirect},
				{ID: "windows", Platform: "windows/amd64", Variant: "WrongPlatform", AcquireMode: core.EngineAcquireDirect},
			},
		}},
	}}
	tt := ui.NewTester(s.view, 1200, 1000)
	if !tt.HasText(tr(nil, "nodes.remote.install_engine")) || tt.HasText("WrongPlatform") {
		t.Fatalf("remote library was not filtered: %v", tt.Texts())
	}
}
