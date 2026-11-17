package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// Debug fields stay behind folds; the running page keeps one readable state.
func TestDetailKeepsDebugFieldsFolded(t *testing.T) {
	s := testState()
	s.selected = "dashboard"
	s.detailID = "fixture"
	s.detail = &core.RunTaskDetail{
		Task:     core.RunTask{TaskId: "fixture", Kind: "run", State: "running", Bundle: ptr("zh-core")},
		Progress: &core.RunProgress{Done: 42, Total: ptr(int64(120)), LastSeq: 42, Phase: ptr("formal")},
	}
	tt := ui.NewTester(s.detailView, 1120, 1400)
	if tt.HasText("last_seq") || tt.HasText("序号：42") {
		t.Fatal("debug progress field visible by default", tt.Texts())
	}
	if tt.HasText("completed") || tt.HasText("运行中 · 运行中") {
		t.Fatal("duplicated state text", tt.Texts())
	}
	click(t, tt, "进度原始字段")
	if !tt.HasText("序号：42") {
		t.Fatal("folded debug field unreachable", tt.Texts())
	}
}

func TestDownloadFailureKeepsRawCodeFolded(t *testing.T) {
	s := testState()
	s.downloads = []core.DownloadTask{{
		TaskId: "dl", Kind: "model", OutName: "model", State: "failed", SourceUrl: "https://example.invalid/model.gguf",
		Error: core.Err("download_failed"),
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 1000)
	if tt.HasText("download_failed") {
		t.Fatal("raw error code shows by default", tt.Texts())
	}
	if !tt.HasText("下载失败，请检查网络后重试。") {
		t.Fatal("readable failure sentence missing", tt.Texts())
	}
	click(t, tt, "观测依据与输出")
	if !tt.HasText("错误码：download_failed") {
		t.Fatal("raw code unreachable", tt.Texts())
	}
}

// The header states each status in words instead of an icon-only control.
func TestCornerStatusShowsWordsAndCounts(t *testing.T) {
	s := testState()
	s.engineState = core.EngineState{State: "running"}
	s.tasks = []core.RunTask{{TaskId: "a", State: "running"}, {TaskId: "b", State: "starting", Queued: true}}
	tt := ui.NewTester(s.cornerStatus, 900, 600)
	for _, want := range []string{"运行中", "执行 1 · 等待 1"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
}

// The settings page states the save rule once and the directory state per field.
func TestSettingsStateAndSaveNoteAreNotRepeated(t *testing.T) {
	s := testState()
	s.selected = "settings"
	s.settingsLoaded = true
	s.workspace = "/missing/workspace"
	tt := ui.NewTester(s.view, 1120, 2200)
	count := 0
	for _, text := range tt.Texts() {
		count += strings.Count(text, "保存设置不创建目录")
	}
	if count != 1 {
		t.Fatalf("save note repeated %d times: %v", count, tt.Texts())
	}
	if !tt.HasText("路径已修改，保存后生效") {
		t.Fatal("edited path state missing", tt.Texts())
	}
	s.workspace = "/old"
	s.settings.Effective.WorkspaceRoot = ptr("/old")
	tt.Frame()
	if !tt.HasText("目录不存在") {
		t.Fatal("existing path state missing", tt.Texts())
	}
}

// Task, download and status wording carries no duplicated Chinese/English state
// and no invented "unknown" state for an empty value.
func TestStateLabelsStayReadable(t *testing.T) {
	if got := stateLabel(""); got == "未知状态" {
		t.Fatal("empty state claims to be unknown instead of missing")
	}
	for _, value := range []string{"running", "complete", "failed"} {
		if strings.Contains(stateLabel(value), value) {
			t.Fatalf("state %q keeps its raw English alongside the label", value)
		}
	}
}

// The task page keeps a page-level heading like the other pages; the old muted
// one-liner is now a subtitle below it.
func TestTaskPageKeepsItsPageHeading(t *testing.T) {
	s := testState()
	s.selected = "dashboard"
	tt := ui.NewTester(s.view, 1120, 1200)
	if !tt.HasText("任务") || !tt.HasText("配置、运行与跟踪本地模型测评。") {
		t.Fatal("task page heading or subtitle missing", tt.Texts())
	}
	heading, ok := tt.Find("任务")
	if !ok || heading.Y > 120 {
		t.Fatalf("task heading is not at page top: %+v", heading)
	}
}

// Secondary and primary actions hug their label instead of stretching across a
// card, so weight shows in style rather than width.
func TestCardActionsKeepOneButtonGeometry(t *testing.T) {
	s := testState()
	s.selected = "settings"
	s.settingsLoaded = true
	tt := ui.NewTester(s.view, 1120, 2400)
	for _, label := range []string{"保存", "保存下载目录名", "恢复默认值", "注册引擎", "重新核验引擎"} {
		rect, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing action %q", label)
		}
		if rect.W > 400 {
			t.Fatalf("action %q stretches to %g DIP", label, rect.W)
		}
	}
	s.selected = "models"
	s.registry, s.checks = modelRegistryFixture(), eligibleChecks()
	s.discoveryLoaded = true
	tt.Frame()
	refresh, ok := tt.Find("刷新服务列表")
	if !ok || refresh.W > 400 {
		t.Fatalf("refresh control is not a label-sized action: %+v", refresh)
	}
}
