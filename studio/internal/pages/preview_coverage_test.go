//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestPreviewExpandedScenesAndInteractions(t *testing.T) {
	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1200, 2200)

	// 1. Outdated CLI scene supports failed upgrade banner and clean upgrade to current version.
	p.loadScene(previewSceneIndex(t, "测评 · CLI 版本过旧与升级"))
	tt.Frame()
	if p.state.ready.CLI.Status != "outdated" || !tt.HasText("升级 CLI") {
		t.Fatalf("expected outdated CLI upgrade card, got cli=%+v texts=%v", p.state.ready.CLI, tt.Texts())
	}
	p.failNext = true
	click(t, tt, "升级 CLI")
	p.advance(700 * time.Millisecond)
	tt.Frame()
	if p.state.cliUpgrade.State != core.CLIUpgradeFailed {
		t.Fatalf("expected simulated CLI upgrade failure state, got %+v", p.state.cliUpgrade)
	}
	click(t, tt, "升级 CLI")
	p.advance(1400 * time.Millisecond)
	tt.Frame()
	if p.state.ready.CLI.Status != "ok" || emptyText(p.state.ready.CLI.Version) != previewAppVersion || !p.state.serveSupported {
		t.Fatalf("expected CLI upgraded to %s with serveSupported=true, got %+v serve=%v", previewAppVersion, p.state.ready.CLI, p.state.serveSupported)
	}

	// 2. Library edge states scene covers unavailable model, ineligible engine, and rescan spinner.
	p.loadScene(previewSceneIndex(t, "模型库 · 失效资产与引擎校验异常"))
	tt.Frame()
	if !tt.HasText("DeepSeek-R1-Distill-32B-Q4_K_M.gguf") || !tt.HasText("/preview/extra-models") {
		t.Fatalf("expected unavailable model and extra root in library edge scene, texts=%v", tt.Texts())
	}
	click(t, tt, "重新扫描")
	if !p.state.registry.Refreshing {
		t.Fatalf("expected 重新扫描 in preview to set registry.Refreshing=true")
	}
	p.advance(500 * time.Millisecond)
	if p.state.registry.Refreshing {
		t.Fatalf("expected registry.Refreshing to clear after simulated reload")
	}

	// 3. Local engines removal enqueues Undo action toast in preview mode.
	p.loadScene(previewSceneIndex(t, "模型库 · 本地安装与目录模型"))
	p.state.removeInstallationWithUndo("/preview/vllm.json")
	if len(p.state.pendingToasts) == 0 || p.state.pendingToasts[len(p.state.pendingToasts)-1].Kind != toastKindAction || p.state.pendingToasts[len(p.state.pendingToasts)-1].Action == "" {
		t.Fatalf("expected Undo action toast after removing local engine installation in preview, got %+v", p.state.pendingToasts)
	}

	// 4. PolyStrata setup scene surfaces both build-tools blocker and runtime network proxy guidance.
	p.loadScene(previewSceneIndex(t, "模型库 · PolyStrata 引导安装与网络提示"))
	tt.Frame()
	if !tt.HasText("brew install cmake ninja") || !tt.HasText(l10n.TR("downloads.polystrata.proxy_hint.action")) {
		t.Fatalf("expected build-tools fix command and proxy guidance in PolyStrata setup scene, texts=%v", tt.Texts())
	}

	// 5. Multi-run comparison report and seeded Long-Context Speed history.
	p.loadScene(previewSceneIndex(t, "报告 · 多模型对比报告"))
	tt.Frame()
	if p.state.openedReport == nil || len(p.state.openedReport.index.Runs) != 2 {
		t.Fatalf("expected 2 runs in compare-report, got %+v", p.state.openedReport)
	}
	p.loadScene(previewSceneIndex(t, "报告 · 长上下文专项"))
	tt.Frame()
	if len(p.state.speed.history) != 3 || !tt.HasText("preview-speed-64k") || !tt.HasText("preview-speed-128k-gap") {
		t.Fatalf("expected 3 seeded speed history entries, got len=%d texts=%v", len(p.state.speed.history), tt.Texts())
	}

	// 6. Orphaned service scene renders both stop and discard lock actions; multi-service scene exposes discovery & quit dialog.
	p.loadScene(previewSceneIndex(t, "服务 · 孤儿进程恢复与锁清理"))
	tt.Frame()
	if p.state.engineState.State != "orphaned" || !orphanRecordRecoverable(p.state.engineState) {
		t.Fatalf("expected recoverable orphaned service state, got %+v", p.state.engineState)
	}
	p.loadScene(previewSceneIndex(t, "服务 · 多服务并行与外部发现"))
	tt.Frame()
	if !tt.HasText("ollama") {
		t.Fatalf("expected discovered external ollama service on Services page, texts=%v", tt.Texts())
	}
	p.state.handleWindowClose(nil)
	if !p.state.quitDialog.open {
		t.Fatalf("expected handleWindowClose to open quitServiceDialog when standalone services are running")
	}

	// 7. Notifications scene seeds 3 screen cards + bell center history, and engine update records event notification.
	p.loadScene(previewSceneIndex(t, "通知 · 叠卡横幅与通知中心"))
	tt.Frame()
	if got := len(p.state.notifications().screenItems()); got != 3 {
		t.Fatalf("expected 3 stacked screen notifications in notifications_center scene, got %d", got)
	}
	if got := len(p.state.notifications().items); got != 4 {
		t.Fatalf("expected 4 total notifications in center, got %d", got)
	}
	p.loadScene(previewSceneIndex(t, "测评 · 待运行"))
	eng := p.state.registry.Engines[0]
	upd := p.state.engineUpdates[eng.Name]
	p.startEngineUpdate(eng, upd)
	if got := len(p.state.notifications().items); got == 0 {
		t.Fatalf("expected startEngineUpdate in preview to record notification in center")
	}
}
