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
func TestFirstOpen(t *testing.T) {
	for _, tc := range []struct {
		page  string
		first bool
		want  string
	}{
		{"", true, "dashboard"},
		{"", false, "reports"},
		{"/", true, "dashboard"},
		{"/", false, "reports"},
		{"/models", true, "models"},
		{"dashboard", false, "dashboard"},
		{"missing", true, "reports"},
	} {
		if got := firstOpen(tc.page, tc.first); got != tc.want {
			t.Fatalf("firstOpen(%q, %v) = %q, want %q", tc.page, tc.first, got, tc.want)
		}
	}
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
	if len(s.checkedBundles()) != 0 || !tt.HasText("至少选择一个题包。") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "运行 benchmark")
	if s.busy {
		t.Fatal("empty scope started")
	}
}
func TestDetailTableAndResourcesRender(t *testing.T) {
	s := testState()
	s.selected = "dashboard"
	s.detailID = "fixture"
	s.detail = &core.RunTaskDetail{Task: core.RunTask{TaskId: "fixture", Kind: "resume", State: "done", Exit: &core.RunExit{ExitCode: 3}}, Progress: &core.RunProgress{Done: 1, Total: ptr(int64(2)), Phase: ptr("formal"), Cases: []core.CaseProgress{{CaseId: "case-pelican", State: "failed", ErrorCategory: ptr("fixture-failure")}}}, Resources: chartSamples()}
	tt := ui.NewTester(s.view, 1120, 1800)
	for _, want := range []string{"运行不完整", "原始状态：done", "1 / 2", "分母为冻结续测子集", "case-pelican", "fixture-failure"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	if err := tt.Click("返回运行"); err != nil {
		t.Fatal(err)
	}
	if s.detailID != "" || !tt.HasText("配置、运行与跟踪本地模型测评。") {
		t.Fatal("return navigation failed")
	}
}
func TestReportFileURLAndInputNames(t *testing.T) {
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
