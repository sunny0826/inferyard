package pages

import (
	"encoding/json"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"math"
	"net/url"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func testState() viewState {
	return viewState{selected: "dashboard", loaded: true, settingsLoaded: true, denied: map[string]bool{}, chartIndex: -1, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok", Version: ptr("1.0")}}, progress: &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_complete", BoundPath: ptr("/w/bound/zh-core-1")}, {Bundle: "zh-svg-pelican", Phase: "bound_missing"}}}}
}
func TestSixPagesRender(t *testing.T) {
	for _, tc := range []struct{ page, want string }{{"dashboard", "zh-svg-pelican · 待绑定服务"}, {"device", "尚无设备快照"}, {"models", "选择模型与引擎"}, {"downloads", "下载引擎"}, {"reports", "选择来源 run（1–100 个）"}, {"settings", "CLI"}} {
		t.Run(tc.page, func(t *testing.T) {
			s := testState()
			s.selected = tc.page
			tester := ui.NewTester(s.view, 1120, 800)
			if !tester.HasText(tc.want) {
				t.Fatalf("missing %q: %v", tc.want, tester.Texts())
			}
			if tc.page == "settings" {
				if tester.HasText("生效值与探测状态") || !tester.HasText("保存") {
					t.Fatal("settings retained obsolete content", tester.Texts())
				}
				for _, label := range tester.Texts() {
					if label == "保存设置" {
						t.Fatal("obsolete settings save button")
					}
				}
				if !tester.HasText("空值使用发现到的可执行文件，顺序是 PATH、~/.local/bin，macOS 还有 /opt/homebrew/bin 和 /usr/local/bin。") {
					t.Fatal("missing CLI discovery help", tester.Texts())
				}
			}
			for _, n := range navigation {
				if !tester.HasText(n.label) {
					t.Fatalf("missing Sidebar page %s", n.label)
				}
			}
			tester.SetDark(true)
			if !tester.HasText(tc.want) {
				t.Fatal("dark theme lost page content")
			}
		})
	}
}
func TestSidebarDashboardEntry(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.view, 1120, 2400)
	click(t, tt, "设置")
	if s.selected != "settings" || !tt.HasText("路径设置") {
		t.Fatal(tt.Texts())
	}
	if tt.HasText("环境准备") || tt.HasText("初始化工作区") {
		t.Fatal("setup remains in settings")
	}
	click(t, tt, "新任务")
	if s.selected != "dashboard" || !tt.HasText("运行 benchmark") {
		t.Fatal(tt.Texts())
	}
	if len(navigation) != 5 || pageID("") != "reports" || pageID("dashboard") != "dashboard" || pageID("missing") != "reports" {
		t.Fatal(navigation)
	}
	for i, want := range []struct{ id, label string }{{"reports", "报告"}, {"models", "模型"}, {"downloads", "下载"}, {"device", "设备"}, {"settings", "设置"}} {
		if navigation[i].id != want.id || navigation[i].label != want.label {
			t.Fatal(navigation)
		}
	}
}
func TestExplicitSingleBundleConfirmation(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.candidateView, 1120, 1400)
	if got := s.checkedBundles(); len(got) != 1 || got[0] != "zh-core" {
		t.Fatal(got)
	}
	click(t, tt, "zh-core")
	if len(s.checkedBundles()) != 0 || !tt.HasText("至少选择一个题包。") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "运行 benchmark")
	if s.busy {
		t.Fatal("empty scope started")
	}
}
func TestBoundBundleSelectsAndDuplicateGuard(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.candidateView, 1120, 1400)
	if tt.HasText("zh-smoke") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "zh-svg-pelican")
	if len(s.checkedBundles()) != 2 {
		t.Fatal(s.checkedBundles())
	}
	click(t, tt, "zh-core")
	if got := s.checkedBundles(); len(got) != 1 || got[0] != "zh-svg-pelican" {
		t.Fatal(got)
	}
}
func TestBundleHelpers(t *testing.T) {
	s := viewState{}
	if len(s.boundConfigs()) != 0 {
		t.Fatal("nil progress yielded options")
	}
	names := []string{"a", "b"}
	if defaultBundle("", names, "") != "a" || defaultBundle("a", names, "a") != "a" || defaultBundle("x", names, "a") != "b" {
		t.Fatal("defaultBundle mismatch")
	}
	options := []boundOption{{bundle: "a", config: "/w/a/config.toml"}}
	if configOf(options, "a") != "/w/a/config.toml" || configOf(options, "b") != "" {
		t.Fatal("configOf mismatch")
	}
}
func TestDetailTableAndResourcesRender(t *testing.T) {
	s := testState()
	s.selected = "dashboard"
	s.detailID = "fixture"
	s.detail = &core.RunTaskDetail{Task: core.RunTask{TaskId: "fixture", Kind: "resume", State: "done", Exit: &core.RunExit{ExitCode: 3}}, Progress: &core.RunProgress{Done: 1, Total: ptr(int64(2)), Phase: ptr("formal"), Cases: []core.CaseProgress{{CaseId: "case-pelican", State: "failed", ErrorCategory: ptr("fixture-failure")}}}, Resources: []core.ResourceSample{{Utc: "2026-10-13T06:00:00Z", MemAvailableBytes: ptr(int64(1073741824)), ServiceCpuPercent: ptr(12.5)}}}
	tt := ui.NewTester(s.view, 1120, 1800)
	for _, want := range []string{"运行不完整", "原始状态：done", "1 / 2", "分母为冻结续测子集", "case-pelican", "fixture-failure", "采样点：1", "1073741824", "缺测"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	if err := tt.Click("返回运行"); err != nil {
		t.Fatal(err)
	}
	if s.detailID != "" || !tt.HasText("运行历史") {
		t.Fatal("return navigation failed")
	}
}
func TestResourceUnitsNullsAndOrdering(t *testing.T) {
	samples := []core.ResourceSample{{Utc: "2026-10-13T06:00:02Z", ServiceCpuPercent: ptr(12.5)}, {Utc: "2026-10-13T06:00:00Z", MemAvailableBytes: ptr(int64(2147483648))}, {Utc: "2026-10-13T06:00:01Z"}, {Utc: "2026-10-13T06:00:02Z", ServiceCpuPercent: ptr(15.5)}, {Utc: "bad"}}
	points := resourcePoints(samples)
	if len(points) != 3 || *points[0].values[0] != 2 || points[1].values[0] != nil || points[1].values[2] != nil || *points[2].values[2] != 15.5 || points[2].seconds-points[0].seconds != 2 {
		t.Fatalf("units, gaps or timestamp ordering changed: %+v", points)
	}
	if finite(nil) || finite(ptr(math.NaN())) || finite(ptr(math.Inf(1))) {
		t.Fatal("missing / non-finite point accepted")
	}
}
func TestReportFileURLAndInputNames(t *testing.T) {
	value := reportURL("/tmp/report space/#测试/report.html")
	parsed, e := url.Parse(value)
	if e != nil || parsed.Scheme != "file" || parsed.Path != "/tmp/report space/#测试/report.html" || parsed.Fragment != "" || !strings.Contains(value, "%23") {
		t.Fatalf("unsafe file URL %s", value)
	}
	for _, v := range []string{"..", "...", "../outside", "bad name", "/tmp"} {
		if validName(v) {
			t.Fatalf("accepted unsafe directory %s", v)
		}
	}
	if !validName("model-v1.gguf") || environmentName.MatchString("hf-secret-token") {
		t.Fatal("asset input validation changed")
	}
}
func TestResumePairRejectsInvalidInput(t *testing.T) {
	s := testState()
	s.resumeSource = &core.RunSummary{Path: "/results/run"}
	s.endpoint = "http://127.0.0.1:8080"
	s.resume()
	if s.failure == nil || s.failure.Code != "invalid_request" {
		t.Fatal("unpaired endpoint accepted")
	}
	s.pid = "1.5"
	s.resume()
	if s.failure == nil || s.failure.Message != "PID 必须为正整数。" {
		t.Fatal("fractional PID accepted")
	}
}

func TestLongPageStartsAtTopAndScrolls(t *testing.T) {
	s := testState()
	s.selected = "settings"
	tt := ui.NewTester(s.view, 1120, 800)
	title, ok := tt.Find("InferYard Studio")
	if !ok || title.Y < 0 || title.Y > 100 {
		t.Fatalf("Sidebar was vertically centered: %+v", title)
	}
	field, ok := tt.Find("CLI")
	if !ok || field.Y < 0 || field.Y > 250 {
		t.Fatalf("first settings field clipped: %+v", field)
	}
	tt.Scroll(700, 500, 0, 600)
	if bounds, ok := tt.Find("恢复默认值"); !ok || bounds.Y < 0 || bounds.Y+bounds.H > 800 {
		t.Fatal("long page cannot scroll to download name actions")
	}
}

func TestNullDeviceSnapshotUsesEmptyState(t *testing.T) {
	s := testState()
	s.selected = "device"
	s.device.Snapshot = json.RawMessage("null")
	tt := ui.NewTester(s.view, 1120, 800)
	if !tt.HasText("尚无设备快照") || tt.HasText("推荐配置") {
		t.Fatal("nullable core snapshot was shown as measured data", tt.Texts())
	}
}

func TestSettingsFollowWorkspaceHandoffAndPreserveEdits(t *testing.T) {
	s := testState()
	s.selected = "settings"
	s.settingsLoaded = false
	old := core.SettingsResponse{Settings: core.UserSettings{WorkspaceRoot: ptr("/old"), ResultsRoot: ptr("/old/results"), ModelsRoot: ptr("/models")}, Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/old")}}
	s.applySettings(old)
	tt := ui.NewTester(s.view, 1120, 1200)
	next := old
	next.Settings.WorkspaceRoot = ptr("/new-child")
	next.Effective.WorkspaceRoot = ptr("/new-child")
	next.Settings.ResultsRoot = ptr("/new-child/results")
	s.applySettings(next)
	tt.Frame()
	if s.results != "/new-child/results" || s.workspace != "/new-child" {
		t.Fatal("workspace handoff left settings form stale", tt.Texts())
	}
	s.cliPath = "/editing/cli"
	newer := next
	newer.Settings.WorkspaceRoot = ptr("/another")
	newer.Effective.WorkspaceRoot = ptr("/another")
	s.applySettings(newer)
	tt.Frame()
	if s.cliPath != "/editing/cli" || s.workspace != "/new-child" {
		t.Fatal("refresh discarded unsaved settings")
	}
}
