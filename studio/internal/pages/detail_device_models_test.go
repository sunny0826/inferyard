package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// TestDetailCaseTableSeparatesNotApplicableFromMissing pins the two different
// readings: a finished case without a failure has no error category ("—"), a
// failed case without a recorded category is a missing measurement ("缺测"),
// quality reuses the report vocabulary, and the duration carries its unit.
func TestDetailCaseTableSeparatesNotApplicableFromMissing(t *testing.T) {
	s := testState()
	s.detailID = "detail-1"
	s.detail = &core.RunTaskDetail{
		Task: core.RunTask{TaskId: "detail-1", Kind: "run", State: "running", Bundle: ptr("zh-core")},
		Progress: &core.RunProgress{Done: 2, Total: ptr(int64(2)), Cases: []core.CaseProgress{
			{CaseId: "case-pass", State: "completed", Category: ptr("qa"), QualityState: ptr("pass"), DurationMs: ptr(1240.0)},
			{CaseId: "case-failed", State: "failed", QualityState: ptr("unscorable"), DurationMs: ptr(840.0)},
		}},
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.detailView(c) }) }, 1120, 1200)
	tt.Frame()
	for _, want := range []string{"1.24 s", "840 ms", "通过", "无法评分", "← 返回任务"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %v", want, tt.Texts())
		}
	}
	if exactTextCount(tt.Texts(), "pass") != 0 || exactTextCount(tt.Texts(), "unscorable") != 0 {
		t.Fatalf("raw quality enum leaked: %v", tt.Texts())
	}
	if got := exactTextCount(tt.Texts(), "—"); got != 1 {
		t.Fatalf("— count = %d, want only the passing case's error category: %v", got, tt.Texts())
	}
	for _, item := range []core.CaseProgress{
		{CaseId: "pass", State: "completed"},
		{CaseId: "failed", State: "failed"},
		{CaseId: "reported", State: "failed", ErrorCategory: ptr("request_timeout")},
	} {
		if got := caseErrorCategory(item); (item.State == "completed" && got != "—") || (item.State == "failed" && item.ErrorCategory == nil && got != "缺测") || (item.ErrorCategory != nil && got != "request_timeout") {
			t.Fatalf("caseErrorCategory(%+v) = %q", item, got)
		}
	}
	click(t, tt, "← 返回任务")
	if s.detailID != "" {
		t.Fatalf("back did not leave the detail: %q", s.detailID)
	}
}

// TestResourceChartsUseTwoColumnsWhenWide checks the layout rule directly: on a
// wide viewport the second curve sits beside the first, while the compact
// layout keeps one column.
func TestResourceChartsUseTwoColumnsWhenWide(t *testing.T) {
	render := func(width int) *ui.Tester {
		s := testState()
		tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.resourceView(c, chartSamples()) }) }, width, 2400)
		tt.Frame()
		return tt
	}
	wide := render(1400)
	memory, ok := wide.Find("可用内存 GiB曲线")
	if !ok {
		t.Fatalf("missing memory chart: %v", wide.Texts())
	}
	rss, ok := wide.Find("服务 RSS GiB曲线")
	if !ok {
		t.Fatalf("missing rss chart: %v", wide.Texts())
	}
	if memory.Y != rss.Y || memory.X >= rss.X {
		t.Fatalf("wide layout did not pair charts: memory=%+v rss=%+v", memory, rss)
	}
	cpu, ok := wide.Find("服务 CPU %曲线")
	if !ok || cpu.Y <= memory.Y {
		t.Fatalf("third chart did not wrap to a new row: %+v", cpu)
	}
	if wide.HasText("fixture:available") {
		t.Fatal("raw collector id leaked into a single-source caption")
	}

	narrow := render(1000)
	first, _ := narrow.Find("可用内存 GiB曲线")
	second, _ := narrow.Find("服务 RSS GiB曲线")
	if second.Y <= first.Y {
		t.Fatalf("compact layout stacked side by side: first=%+v second=%+v", first, second)
	}
}

// TestDeviceEmptyStateOffersCollection proves the empty state leads with the
// action that fills it instead of sending the user to settings.
func TestDeviceEmptyStateOffersCollection(t *testing.T) {
	s := testState()
	s.device.Snapshot = nil
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.deviceView(c) }) }, 1120, 900)
	tt.Frame()
	if !tt.HasText("尚无设备快照") || !tt.HasText("采集设备信息") || !tt.HasText("前往设置") {
		t.Fatalf("empty state actions missing: %v", tt.Texts())
	}
	if tt.HasText("前往设置配置模型目录") {
		t.Fatalf("old settings-only call to action remained: %v", tt.Texts())
	}
	click(t, tt, "前往设置")
	if s.selected != "settings" {
		t.Fatalf("secondary action did not open settings: %q", s.selected)
	}
}

// TestStopReasonAndQualityLabelsStayChinese pins the translations that used to
// leak raw enums into primary text.
func TestStopReasonAndQualityLabelsStayChinese(t *testing.T) {
	for raw, want := range map[string]string{
		"completed":       "已完成",
		"plan_finished":   "已完成",
		"user_cancelled":  "用户取消",
		"":                "—",
		"some_new_reason": "未识别停止原因：some_new_reason",
	} {
		if got := stopReasonLabel(raw); got != want {
			t.Errorf("stopReasonLabel(%q) = %q, want %q", raw, got, want)
		}
	}
	for raw, want := range map[string]string{
		"pass": "通过", "fail": "未通过", "unscorable": "无法评分",
		"not_scored": "未评分", "not_applicable": "不适用",
	} {
		value := raw
		if got := reportQuality(&value); got != want {
			t.Errorf("reportQuality(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := reportQuality(nil); got != "—" {
		t.Errorf("reportQuality(nil) = %q, want —", got)
	}
}

// TestModelsActionsLoopIntoTaskAndDownloads covers the actions that were
// missing once assets exist, including the guard that keeps an active round.
func TestModelsActionsLoopIntoTaskAndDownloads(t *testing.T) {
	s := testState()
	s.model = ""
	s.registry = core.Registry{Models: []core.ModelAsset{
		{Name: "one.gguf", Available: true},
		{Name: "hidden.gguf", Available: false},
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.modelsView(c) }) }, 1120, 1200)
	tt.Frame()
	if !tt.HasText("下载更多") || !tt.HasText("刷新") || !tt.HasText("用此模型测评 · one.gguf") {
		t.Fatalf("model actions missing: %v", tt.Texts())
	}
	if tt.HasText("用此模型测评 · hidden.gguf") {
		t.Fatalf("unavailable model offered a task action: %v", tt.Texts())
	}
	click(t, tt, "下载更多")
	if s.selected != "downloads" {
		t.Fatalf("download more did not open downloads: %q", s.selected)
	}

	s.selected = "models"
	tt.Frame()
	click(t, tt, "用此模型测评 · one.gguf")
	if s.model != "one.gguf" || s.selected != "dashboard" {
		t.Fatalf("task action did not enter the task page with the model: %q %q", s.model, s.selected)
	}
	if s.task.stage != "prepare_candidate" {
		t.Fatalf("task action did not start a fresh round: %q", s.task.stage)
	}

	// An active round owns the page: the shortcut stays visible but disabled.
	s.selected, s.model = "models", ""
	s.tasks = []core.RunTask{{TaskId: "run", State: "running"}}
	tt.Frame()
	before := s.selected
	click(t, tt, "用此模型测评 · one.gguf")
	if s.model != "" || s.selected != before {
		t.Fatalf("active round was disturbed: model=%q page=%q", s.model, s.selected)
	}
}
