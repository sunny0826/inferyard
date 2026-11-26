package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
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

func TestRunHistoryTabBelongsToBenchmarks(t *testing.T) {
	s := readyState("reports")
	s.reportsTab = 2 // Former history tab.
	reports := ui.NewTester(s.reportsView, 1120, 900)
	if tab, ok := reports.AccessByRole(ui.RoleTab, tr(nil, "reports.tab.reports")); !ok || !tab.Selected {
		t.Fatal("expected the report list to be selected")
	}
	if _, ok := reports.AccessByRole(ui.RoleTab, tr(nil, "reports.tab.history")); ok {
		t.Fatal("unexpected history tab on reports page")
	}
	speed := tr(nil, "reports.tab.context_speed")
	click(t, reports, speed)
	if tab, ok := reports.AccessByRole(ui.RoleTab, speed); !ok || !tab.Selected {
		t.Fatal("expected context speed tab to be selected")
	}
	s.selectPage("benchmarks")
	bench := ui.NewTester(s.benchmarksView, 1120, 900)
	history := tr(nil, "reports.tab.history")
	click(t, bench, history)
	if tab, ok := bench.AccessByRole(ui.RoleTab, history); !ok || !tab.Selected {
		t.Fatal("expected the benchmark history tab to be selected")
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

func TestInterfaceDetailsPolishInteractionsAndPlurals(t *testing.T) {
	// 1. Sidebar "+" button meets 24x24px desktop target minimum.
	s := readyState("services")
	tt := sidebarHeaderState(s)
	tt.Frame()
	plusInfo, ok := tt.AccessByRole(ui.RoleButton, "新任务")
	if !ok || plusInfo.Bounds.W < 24 || plusInfo.Bounds.H < 24 {
		t.Fatalf("expected 新任务 button >= 24x24, got ok=%v bounds=%+v", ok, plusInfo.Bounds)
	}

	// 2. Re-selecting active Reports or Benchmarks resets sub-views.
	s.selected = "reports"
	s.openedReport = &reportViewState{reference: "rep-1"}
	s.reportsTab = reportsTabSpeed
	s.selectPage("reports")
	if s.openedReport != nil || s.reportsTab != reportsTabList {
		t.Fatalf("expected re-selecting reports to close opened report and return to list tab, got opened=%v tab=%d", s.openedReport, s.reportsTab)
	}

	s.selected = "benchmarks"
	s.detailID = "task-1"
	s.benchFlowOpen = true
	s.selectPage("benchmarks")
	if s.detailID != "" || s.benchFlowOpen {
		t.Fatalf("expected re-selecting benchmarks to reset detailID and benchFlowOpen, got detail=%q flow=%v", s.detailID, s.benchFlowOpen)
	}

	// 3. Clickable listRow and running service controls expose RoleButton for keyboard access.
	bName := "zh-core"
	s.tasks = []core.RunTask{{TaskId: "t-kbd", State: "running", Bundle: &bName}}
	benchTT := ui.NewTester(s.benchmarksView, 1120, 800)
	benchTT.Frame()
	if _, ok := benchTT.AccessByRole(ui.RoleButton, "zh-core"); !ok {
		t.Fatalf("expected benchmark listRow to expose RoleButton for keyboard reachability")
	}

	// 4. Library standalone Stop button opens confirmation instead of immediate stop.
	m := core.ModelAsset{Name: "qwen.gguf", Path: "/models/qwen.gguf", Format: "gguf", Available: true}
	lib := readyState("library")
	lib.serveSupported = true
	lib.registry.Models = []core.ModelAsset{m}
	svcID := core.ServiceIDFor("qwen.gguf", "llama-cpp")
	lib.managedServices = []core.EngineState{{
		ServiceID: svcID,
		State:     "running",
		Origin:    core.EngineOriginStandalone,
		ModelName: "qwen.gguf",
		Model:     "/models/qwen.gguf",
		Endpoint:  "http://127.0.0.1:8080",
	}}
	lib.engineState = lib.managedServices[0]
	libTT := ui.NewTester(lib.libraryView, 1120, 1400)
	libTT.Frame()
	click(t, libTT, "停止 · qwen.gguf")
	libTT.Frame()
	if destructiveKind(lib.pendingDestructive.kind) != destructiveStopStandalone || lib.pendingDestructive.id != "qwen.gguf" {
		t.Fatalf("expected Library Stop to arm confirmation dialog, got %+v", lib.pendingDestructive)
	}
	if !libTT.HasText("确认停止") {
		t.Fatalf("expected confirmation dialog visible in Library view, texts=%v", libTT.Texts())
	}

	// 5. Clicking labeledSwitchRow text column toggles the bound boolean.
	on := false
	swTT := ui.NewTester(func(c *ui.Context) {
		labeledSwitchRow(c, "局域网访问", "默认仅本机监听", "切换局域网访问", &on)
	}, 600, 200)
	swTT.Frame()
	click(t, swTT, "局域网访问")
	swTT.Frame()
	if !on {
		t.Fatal("expected clicking switch label text to toggle switch on")
	}

	// 6. English ICU plural rules format singular vs plural cleanly.
	if got := l10n.Tf(l10n.LanguageEn, "services.discovered.models_count", "count", 1); got != "1 model" {
		t.Fatalf("got %q, want %q", got, "1 model")
	}
	if got := l10n.Tf(l10n.LanguageEn, "services.discovered.models_count", "count", 3); got != "3 models" {
		t.Fatalf("got %q, want %q", got, "3 models")
	}
	if got := l10n.Tf(l10n.LanguageEn, "reports.tables.requests.total", "count", 1); got != "1 case total" {
		t.Fatalf("got %q, want %q", got, "1 case total")
	}
}
