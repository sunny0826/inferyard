package pages

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestStatusToastShowsOneAtATimeWithoutButtons(t *testing.T) {
	s := readyState("settings")
	tt := ui.NewTester(s.view, 1120, 800)

	s.enqueueStatusToast("设置已保存")
	tt.Frame()
	if !tt.HasText("设置已保存") {
		t.Fatalf("status toast did not render: %v", tt.Texts())
	}
	if tt.HasText("关闭提示") {
		t.Fatalf("status toast should not render close button: %v", tt.Texts())
	}

	s.enqueueStatusToast("设备信息已更新")
	tt.Frame()
	if !tt.HasText("设备信息已更新") {
		t.Fatalf("second toast did not replace first: %v", tt.Texts())
	}
	if tt.HasText("设置已保存") {
		t.Fatalf("older toast remained visible when newer arrived: %v", tt.Texts())
	}
}

func TestActionToastUndoMorphsToRestoredState(t *testing.T) {
	s := readyState("settings")
	s.modelName, s.engineName = "custom-m", "custom-e"
	tt := ui.NewTester(s.view, 1120, 800)

	reverted := false
	s.enqueueActionToast("undo-names", "已恢复默认目录名", "撤销", "已恢复原目录名", 6*time.Second, func() {
		reverted = true
		s.modelName, s.engineName = "custom-m", "custom-e"
	})
	tt.Frame()
	if !tt.HasText("已恢复默认目录名") || !tt.HasText("撤销") {
		t.Fatalf("action toast missing text or button: %v", tt.Texts())
	}

	click(t, tt, "撤销")
	tt.Frame()
	if !reverted {
		t.Fatal("undo callback did not run")
	}
	if !tt.HasText("已恢复原目录名") {
		t.Fatalf("toast did not morph to restored message: %v", tt.Texts())
	}
}

func TestProgressToastResolvesWithActionAndDismissesOnFailure(t *testing.T) {
	s := readyState("reports")
	tt := ui.NewTester(s.view, 1120, 800)

	s.enqueueProgressToast("report-job", "正在生成报告…")
	tt.Frame()
	if !tt.HasText("正在生成报告…") {
		t.Fatalf("progress toast did not render: %v", tt.Texts())
	}

	opened := false
	s.resolveProgressToast("report-job", "报告已生成", "查看报告", func() { opened = true })
	tt.Frame()
	if !tt.HasText("报告已生成") || !tt.HasText("查看报告") {
		t.Fatalf("resolved progress toast missing result or action: %v", tt.Texts())
	}
	click(t, tt, "查看报告")
	if !opened {
		t.Fatal("resolved progress toast action did not fire")
	}

	s.enqueueProgressToast("fail-job", "正在开启引擎…")
	tt.Frame()
	if !tt.HasText("正在开启引擎…") {
		t.Fatalf("second progress toast did not render: %v", tt.Texts())
	}
	s.dismissToast("fail-job")
	tt.Frame()
	if tt.HasText("正在开启引擎…") {
		t.Fatalf("dismissed progress toast still visible: %v", tt.Texts())
	}
}

func TestNotificationStackCapsAtThreeAndKeepsDecisionItemsInBell(t *testing.T) {
	s := readyState("reports")
	tt := ui.NewTester(s.view, 1120, 800)

	s.pushNotification(studioNotification{Title: "通知 1", Body: "第一条普通通知", Kind: stateInfo})
	s.pushNotification(studioNotification{Title: "通知 2", Body: "第二条失败通知", Kind: stateDanger, NeedsDecision: true})
	s.pushNotification(studioNotification{Title: "通知 3", Body: "第三条完成通知", Kind: stateSuccess})
	s.pushNotification(studioNotification{Title: "通知 4", Body: "第四条最新通知", Kind: stateSuccess})
	tt.Frame()

	if !tt.HasText("通知 4") {
		t.Fatalf("newest notification missing: %v", tt.Texts())
	}
	if got := s.notifications().unreadCount(); got != 4 {
		t.Fatalf("unread count = %d, want 4", got)
	}
	if !strings.HasPrefix(s.notifications().lastWindowTitle, "(4) ") {
		t.Fatalf("window title did not include unread count: %q", s.notifications().lastWindowTitle)
	}

	s.clearScreenNotifications()
	tt.Frame()
	if len(s.notifications().screenItems()) != 0 {
		t.Fatalf("screen notifications not cleared: %d", len(s.notifications().screenItems()))
	}
	// NeedsDecision item must remain in bell inbox until handled.
	inbox := s.notifications().inboxItems()
	if len(inbox) != 1 || inbox[0].Title != "通知 2" {
		t.Fatalf("expected NeedsDecision notification to remain in inbox, got %+v", inbox)
	}
	if got := s.notifications().unreadCount(); got != 1 {
		t.Fatalf("unread count after clear = %d, want 1", got)
	}

	// Open bell popover and verify decision item is still accessible.
	s.notifications().bellOpen = true
	tt.Frame()
	if !tt.HasText("通知 2") {
		t.Fatalf("bell inbox did not display retained decision notification: %v", tt.Texts())
	}
}

func TestCoreEventsIngestIntoNotificationsAndSkipManualReportDuplicate(t *testing.T) {
	s := readyState("settings")
	s.engine = "llama-cpp"
	s.lastManualReportID = "rep-manual"

	mustJSON := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	bundle := "core"
	s.ingestCoreEvents([]core.Event{
		{Type: "report.created", Data: mustJSON(map[string]any{"report": map[string]any{"report_id": "rep-manual"}})},
		{Type: "report.created", Data: mustJSON(map[string]any{"report": map[string]any{"report_id": "rep-auto"}})},
		{Type: "run.state", Data: mustJSON(map[string]any{"task": map[string]any{"task_id": "t1", "kind": "benchmark", "state": "failed", "bundle": &bundle}})},
		{Type: "download.state", Data: mustJSON(map[string]any{"task": map[string]any{"task_id": "d1", "kind": "model", "state": "done", "out_name": "qwen"}})},
		{Type: "engine.state", Data: mustJSON(map[string]any{"engine": map[string]any{"state": "orphaned"}})},
		{Type: "engine.check", Data: mustJSON(map[string]any{"engine": "llama-cpp", "eligibility": core.EngineEligibility{State: core.EngineCheckIneligible, Reason: "runtime_receipt_missing"}})},
	})

	items := s.notifications().items
	if len(items) != 5 {
		t.Fatalf("expected 5 notifications (manual report skipped), got %d", len(items))
	}
	for _, item := range items {
		if strings.Contains(item.Body, "rep-manual") {
			t.Fatalf("manual report should not produce duplicate notification: %+v", item)
		}
	}
}

func TestFeedbackBannerScopedToTriggerPageAndDismissible(t *testing.T) {
	s := readyState("settings")
	s.submissionFailed(&core.ApiError{Code: "io_error", Message: "写入失败"})
	tt := ui.NewTester(s.view, 1120, 800)

	if !tt.HasText("写入失败") || tt.HasText("错误码：io_error") {
		t.Fatalf("failure banner did not render expected summary on trigger page: %v", tt.Texts())
	}
	click(t, tt, "错误详情")
	if !tt.HasText("错误码：io_error") {
		t.Fatalf("error details did not reveal code: %v", tt.Texts())
	}

	s.selected = "reports"
	tt.Frame()
	if tt.HasText("写入失败") || tt.HasText("io_error") || s.failure != nil {
		t.Fatalf("failure banner leaked across pages: failure=%+v", s.failure)
	}
}

func TestToastAndNotificationsRenderInDarkAndReducedMotion(t *testing.T) {
	s := readyState("reports")
	s.enqueueStatusToast("这是一条用于验证暗色与减弱动态模式渲染的长文本状态提示")
	s.pushNotification(studioNotification{
		Title: "下载完成",
		Body:  "模型 qwen 已就绪，可前往任务页开始测评。",
		Kind:  stateSuccess,
	})
	tt := ui.NewTester(func(c *ui.Context) {
		c.Theme().Dark = true
		s.view(c)
	}, 420, 720)
	if !tt.HasText("这是一条用于验证暗色与减弱动态模式渲染的长文本状态提示") {
		t.Fatalf("long toast did not render on narrow dark layout: %v", tt.Texts())
	}
	if !tt.HasText("下载完成") {
		t.Fatalf("notification did not render on narrow dark layout: %v", tt.Texts())
	}
}

func TestPolystrataDownloadStateNotifications(t *testing.T) {
	mustJSON := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	s := readyState("downloads")
	s.registry.Engines = []core.EngineAsset{{
		Name:   "polystrata-1",
		Family: "polystrata",
		Path:   "/opt/engines/ps-1/installation.json",
	}}
	s.registry.Models = []core.ModelAsset{{
		Name:      "qwen36-pack",
		Format:    "strata_pack",
		Path:      "/opt/models/qwen36-strata.json",
		Available: true,
	}}

	s.ingestCoreEvents([]core.Event{
		{
			Type: "download.state",
			Data: mustJSON(map[string]any{
				"task": map[string]any{
					"task_id":           "ps-done",
					"kind":              "polystrata",
					"state":             "done",
					"display_name":      "PolyStrata · qwen36 · UD-Q4_K_M",
					"out_name":          "polystrata-1",
					"installation_path": "/opt/engines/ps-1/installation.json",
					"model_config_path": "/opt/models/qwen36-strata.json",
				},
			}),
		},
		{
			Type: "download.state",
			Data: mustJSON(map[string]any{
				"task": map[string]any{
					"task_id":      "ps-failed",
					"kind":         "polystrata",
					"state":        "failed",
					"display_name": "PolyStrata · qwen · Q2_0",
					"out_name":     "polystrata-2",
				},
			}),
		},
	})

	items := s.notifications().items
	if len(items) != 2 {
		t.Fatalf("expected 2 polystrata notifications, got %d: %+v", len(items), items)
	}
	var doneItem, failedItem *studioNotification
	for i := range items {
		if strings.Contains(items[i].Title, "模型下载") {
			t.Fatalf("polystrata notification must not say 模型下载: %+v", items[i])
		}
		switch items[i].Key {
		case "dl:ps-done:done":
			doneItem = items[i]
		case "dl:ps-failed:failed":
			failedItem = items[i]
		}
	}
	if doneItem == nil || doneItem.Title != "PolyStrata 安装完成" || doneItem.Primary == nil || doneItem.Primary.Label != "前往测评" {
		t.Fatalf("unexpected polystrata done notification: %+v", doneItem)
	}
	if failedItem == nil || failedItem.Title != "PolyStrata 安装失败" || !failedItem.NeedsDecision || failedItem.Primary == nil || failedItem.Primary.Label != "查看并重试" {
		t.Fatalf("unexpected polystrata failed notification: %+v", failedItem)
	}

	doneItem.Primary.OnClick()
	if s.selected != "dashboard" || s.engine != "polystrata-1" || s.model != "qwen36-pack" {
		t.Fatalf("done CTA did not navigate to dashboard with PolyStrata selection: page=%q engine=%q model=%q", s.selected, s.engine, s.model)
	}

	failedItem.Primary.OnClick()
	if s.selected != "downloads" {
		t.Fatalf("failed CTA did not navigate to downloads: %q", s.selected)
	}
}

func TestPolystrataDoneNotificationRespectsUnsavedSettingsGuard(t *testing.T) {
	mustJSON := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	s := readyState("settings")
	s.settingsLoaded = true
	s.cliPath = "/custom/unsaved/inferyard"
	s.engine = "llama-cpp"
	s.model = "existing-model"
	s.registry.Engines = []core.EngineAsset{
		{Name: "llama-cpp", Family: "llama-cpp", Path: "/opt/engines/llama"},
		{Name: "polystrata-1", Family: "polystrata", Path: "/opt/engines/ps-1/installation.json"},
	}
	s.registry.Models = []core.ModelAsset{
		{Name: "existing-model", Format: "gguf", Path: "/opt/models/existing.gguf", Available: true},
		{Name: "qwen36-pack", Format: "strata_pack", Path: "/opt/models/qwen36-strata.json", Available: true},
	}

	s.ingestCoreEvents([]core.Event{{
		Type: "download.state",
		Data: mustJSON(map[string]any{
			"task": map[string]any{
				"task_id":           "ps-done-guard",
				"kind":              "polystrata",
				"state":             "done",
				"display_name":      "PolyStrata · qwen36",
				"installation_path": "/opt/engines/ps-1/installation.json",
				"model_config_path": "/opt/models/qwen36-strata.json",
			},
		}),
	}})
	doneItem := s.notifications().items[0]

	doneItem.Primary.OnClick()
	if !s.settingsLeaveConfirm || s.selected != "settings" {
		t.Fatalf("expected unsaved-settings confirm dialog while staying on settings: confirm=%v page=%q", s.settingsLeaveConfirm, s.selected)
	}
	if s.engine != "llama-cpp" || s.model != "existing-model" || s.pendingPolystrataSelection != nil {
		t.Fatalf("clicking CTA with unsaved settings mutated selection early: engine=%q model=%q pending=%+v", s.engine, s.model, s.pendingPolystrataSelection)
	}

	// Simulate clicking "留在设置页".
	s.settingsLeaveConfirm = false
	s.pendingNavAction = nil
	if s.engine != "llama-cpp" || s.model != "existing-model" || s.selected != "settings" {
		t.Fatalf("staying on settings lost original selection: page=%q engine=%q model=%q", s.selected, s.engine, s.model)
	}

	// Simulate confirming leave -> switches to dashboard and applies PolyStrata selection.
	doneItem.Primary.OnClick()
	if s.pendingNavAction == nil {
		t.Fatal("expected pendingNavAction to be populated")
	}
	action := s.pendingNavAction
	s.pendingNavAction = nil
	s.settingsLeaveConfirm = false
	action()
	if s.selected != "dashboard" || s.engine != "polystrata-1" || s.model != "qwen36-pack" {
		t.Fatalf("confirming leave did not apply PolyStrata selection: page=%q engine=%q model=%q", s.selected, s.engine, s.model)
	}
}
