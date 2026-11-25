package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestLegacyPageIDRedirects(t *testing.T) {
	tests := []struct {
		name           string
		legacy         string
		wantPage       string
		wantLibraryTab int
		wantHwExpanded bool
	}{
		{
			name:     "dashboard_to_benchmarks",
			legacy:   "dashboard",
			wantPage: "benchmarks",
		},
		{
			name:           "models_to_library_models_tab",
			legacy:         "models",
			wantPage:       "library",
			wantLibraryTab: 0,
		},
		{
			name:           "downloads_to_library",
			legacy:         "downloads",
			wantPage:       "library",
			wantLibraryTab: 0,
		},
		{
			name:           "device_to_library_with_hw_expanded",
			legacy:         "device",
			wantPage:       "library",
			wantHwExpanded: true,
		},
		{
			name:     "leading_slash_dashboard",
			legacy:   "/dashboard",
			wantPage: "benchmarks",
		},
		{
			name:     "unknown_defaults_to_services",
			legacy:   "unknown-page",
			wantPage: "services",
		},
	}

	for _, tc := range tests {
		t.Run("navigateToNow/"+tc.name, func(t *testing.T) {
			s := readyState("services")
			s.libraryTab = 1
			s.libraryHwExpanded = false
			s.navigateToNow(tc.legacy, "")
			if s.selected != tc.wantPage {
				t.Fatalf("navigateToNow(%q): got selected=%q, want %q", tc.legacy, s.selected, tc.wantPage)
			}
			if tc.wantPage == "library" && tc.legacy != "device" && s.libraryTab != tc.wantLibraryTab {
				t.Fatalf("navigateToNow(%q): got libraryTab=%d, want %d", tc.legacy, s.libraryTab, tc.wantLibraryTab)
			}
			if tc.wantHwExpanded && !s.libraryHwExpanded {
				t.Fatalf("navigateToNow(%q): expected libraryHwExpanded=true", tc.legacy)
			}
		})

		t.Run("selectPageNow/"+tc.name, func(t *testing.T) {
			s := readyState("services")
			s.libraryTab = 1
			s.libraryHwExpanded = false
			s.selectPageNow(tc.legacy)
			if s.selected != tc.wantPage {
				t.Fatalf("selectPageNow(%q): got selected=%q, want %q", tc.legacy, s.selected, tc.wantPage)
			}
			if tc.wantPage == "library" && tc.legacy != "device" && s.libraryTab != tc.wantLibraryTab {
				t.Fatalf("selectPageNow(%q): got libraryTab=%d, want %d", tc.legacy, s.libraryTab, tc.wantLibraryTab)
			}
			if tc.wantHwExpanded && !s.libraryHwExpanded {
				t.Fatalf("selectPageNow(%q): expected libraryHwExpanded=true", tc.legacy)
			}
		})
	}
}

func TestFirstOpenDefaultsToServices(t *testing.T) {
	if got := firstOpen("", false); got != "services" {
		t.Fatalf("firstOpen(\"\", false) = %q, want \"services\"", got)
	}
	if got := firstOpen("", true); got != "services" {
		t.Fatalf("firstOpen(\"\", true) = %q, want \"services\"", got)
	}
	if got := firstOpen("/dashboard", false); got != "benchmarks" {
		t.Fatalf("firstOpen(\"/dashboard\", false) = %q, want \"benchmarks\"", got)
	}
	if got := firstOpen("/device", false); got != "library" {
		t.Fatalf("firstOpen(\"/device\", false) = %q, want \"library\"", got)
	}
}

func TestSidebarAndLibraryAccessibilityRoles(t *testing.T) {
	s := readyState("services")
	s.engineState = core.EngineState{State: "running"}
	b1, b2 := "zh-core", "zh-svg-pelican"
	s.tasks = []core.RunTask{
		{TaskId: "t1", State: "running", Bundle: &b1},
		{TaskId: "t2", State: "queued", Queued: true, Bundle: &b2},
	}

	tt := sidebarHeaderState(s)
	tt.Frame()
	for _, label := range []string{"测评", "新任务", "服务", "模型库", "报告", "设置", "引擎 · 运行中", "测评 1 · 等待 1"} {
		info, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || info.Bounds.W <= 0 || info.Bounds.H <= 0 {
			t.Fatalf("expected RoleButton %q with visible bounds, got ok=%v info=%+v", label, ok, info)
		}
	}

	lib := readyState("library")
	libTT := ui.NewTester(lib.libraryView, 1120, 1400)
	libTT.Frame()
	modelsTab, ok := libTT.AccessByRole(ui.RoleTab, "模型")
	if !ok || !modelsTab.Selected {
		t.Fatalf("expected RoleTab 模型 selected, got ok=%v info=%+v", ok, modelsTab)
	}
	enginesTab, ok := libTT.AccessByRole(ui.RoleTab, "引擎")
	if !ok || enginesTab.Selected {
		t.Fatalf("expected RoleTab 引擎 unselected, got ok=%v info=%+v", ok, enginesTab)
	}
	libTT.ClickAt(enginesTab.Bounds.X+enginesTab.Bounds.W/2, enginesTab.Bounds.Y+enginesTab.Bounds.H/2)
	libTT.Frame()
	if lib.libraryTab != 1 {
		t.Fatalf("clicking 引擎 tab did not switch libraryTab: got %d, want 1", lib.libraryTab)
	}
	enginesTab, ok = libTT.AccessByRole(ui.RoleTab, "引擎")
	if !ok || !enginesTab.Selected {
		t.Fatalf("expected RoleTab 引擎 selected after click, got ok=%v info=%+v", ok, enginesTab)
	}
}
