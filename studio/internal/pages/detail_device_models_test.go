package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

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
	s.detail.Progress.Cases = append(s.detail.Progress.Cases,
		core.CaseProgress{CaseId: "case-unscored", State: "completed", DurationMs: ptr(500.0)})
	tt.Frame()
	if got := caseQuality(core.CaseProgress{CaseId: "unscored", State: "completed"}); got != "缺测" {
		t.Fatalf("caseQuality(nil) = %q, want 缺测", got)
	}
	if got := caseQuality(core.CaseProgress{State: "completed", QualityState: ptr("")}); got != "缺测" {
		t.Fatalf("caseQuality(empty) = %q, want 缺测", got)
	}
	if got := caseQuality(core.CaseProgress{State: "completed", QualityState: ptr("pass")}); got != "通过" {
		t.Fatalf("caseQuality(pass) = %q, want 通过", got)
	}
	if got := caseQuality(core.CaseProgress{State: "completed", QualityState: ptr("not_applicable")}); got != "不适用" {
		t.Fatalf("caseQuality(not_applicable) = %q, want 不适用", got)
	}
	if !tt.HasText("case-unscored") {
		t.Fatalf("unscored case row missing: %v", tt.Texts())
	}
	if got := exactTextCount(tt.Texts(), "—"); got != 2 {
		t.Fatalf("— count = %d, want the two finished cases: %v", got, tt.Texts())
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

func TestResourceChartSourceSurvivesResize(t *testing.T) {
	s := testState()
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.resourceView(c, chartSamples()) }) }, 1400, 2400)
	tt.Frame()
	click(t, tt, "传感器温度 °C来源")
	tt.Frame()
	click(t, tt, "AppleSMC:Tp0C")
	tt.Frame()
	if !tt.HasText("AppleSMC:Tp0C") {
		t.Fatalf("selected sensor is not shown: %v", tt.Texts())
	}
	tt.SetSize(1000, 2400)
	tt.Frame()
	if !tt.HasText("AppleSMC:Tp0C") {
		t.Fatalf("resize reset the selected sensor: %v", tt.Texts())
	}
	first, _ := tt.Find("可用内存 GiB曲线")
	second, _ := tt.Find("服务 RSS GiB曲线")
	if second.Y <= first.Y {
		t.Fatalf("resized layout did not fall back to one column: first=%+v second=%+v", first, second)
	}
}

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

	s.selected = "device"
	s.busy, s.device.Refreshing = true, false
	tt.Frame()
	if err := tt.Click("采集设备信息"); err != nil {
		t.Fatalf("collection button missing: %v", err)
	}
	if s.selected != "device" {
		t.Fatalf("busy page started a device collection")
	}
	s.busy, s.device.Refreshing = false, true
	if !s.deviceCollectionBlocked() {
		t.Fatal("an in-flight collection must block another one")
	}
	s.device.Refreshing = false
	if s.deviceCollectionBlocked() {
		t.Fatal("idle page must allow collection")
	}
	s.selected = "device"
}

func TestStopReasonAndQualityLabelsStayChinese(t *testing.T) {
	for raw, want := range map[string]string{
		"completed":       "已完成",
		"plan_finished":   "已完成",
		"user_cancelled":  "用户取消",
		"":                "—",
		"some_new_reason": "some_new_reason",
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
	if got := stopReasonLabel("service_stop_unconfirmed"); got != "服务停止未确认" {
		t.Errorf("stopReasonLabel(service_stop_unconfirmed) = %q", got)
	}
	s := testState()
	s.failure = core.Conflict("not_resumable")
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.feedback(c) }) }, 1120, 400)
	tt.Frame()
	if !tt.HasText("当前运行不可续测") {
		t.Fatalf("not_resumable stayed raw: %v", tt.Texts())
	}
	if exactTextCount(tt.Texts(), "not_resumable") != 0 {
		t.Fatalf("raw reason leaked before the fold opened: %v", tt.Texts())
	}
	click(t, tt, "错误详情")
	tt.Frame()
	if exactTextCount(tt.Texts(), "not_resumable") == 0 {
		t.Fatalf("opening 错误详情 did not reveal the raw reason: %v", tt.Texts())
	}
	if got := conflictReasonLabel("unmapped_reason"); got != "" {
		t.Fatalf("unknown conflict reason = %q, want empty fallback", got)
	}
}

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

	s.selected, s.model = "models", ""
	s.tasks = []core.RunTask{{TaskId: "run", State: "running"}}
	tt.Frame()
	before := s.selected
	click(t, tt, "用此模型测评 · one.gguf")
	if s.model != "" || s.selected != before {
		t.Fatalf("active round was disturbed: model=%q page=%q", s.model, s.selected)
	}
}
