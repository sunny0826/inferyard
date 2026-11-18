package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func testState() viewState {
	return viewState{selected: "dashboard", loaded: true, settingsLoaded: true, denied: map[string]bool{}, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok", Version: ptr("1.0")}, Workspace: core.DirectoryReadiness{Initialized: true}, Results: core.DirectoryReadiness{Exists: true}, Models: core.DirectoryReadiness{Exists: true}}, progress: &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_complete", BoundPath: ptr("/w/bound/zh-core-1")}, {Bundle: "zh-svg-pelican", Phase: "bound_missing"}}}}
}

func TestExplicitSingleBundleConfirmation(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.candidateView, 1120, 1400)
	if got := s.checkedBundles(); len(got) != 2 {
		t.Fatal(got)
	}
	click(t, tt, "zh-svg-pelican")
	if got := s.checkedBundles(); len(got) != 1 || got[0] != "zh-core" {
		t.Fatal(got)
	}
	click(t, tt, "zh-core")
	if len(s.checkedBundles()) != 0 || !tt.HasText("至少选择一个测评题集。") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "开始测评")
	if s.busy {
		t.Fatal("empty scope started")
	}
}
