//go:build ui_debug && !release

package pages

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func previewTester(p *previewSession, width, height int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.themeFor(c))
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(205).Shrink(0).Children(func() { p.left(c) })
			p.content(c)
		})
	}, width, height)
}

func previewSceneIndex(t *testing.T, label string) int {
	t.Helper()
	for i, scene := range previewScenes {
		if scene.label == label {
			return i
		}
	}
	t.Fatal("unknown scene", label)
	return -1
}

func TestPreviewResetReturnsReportsToDefaultTab(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "任务 · 已完成"))
	p.state.selected = "reports"
	tt := previewTester(p, 1120, 1400)
	tt.Frame()
	click(t, tt, "运行历史")
	if !tt.HasText("运行历史表") {
		t.Fatalf("history tab did not open: %v", tt.Texts())
	}
	click(t, tt, "重置场景")
	tt.Frame()
	if p.state.reportsTab != reportsTabList {
		t.Fatalf("reset kept tab %d", p.state.reportsTab)
	}
	p.state.selected = "reports"
	tt.Frame()
	if !tt.HasText("打开报告") || tt.HasText("运行历史表") {
		t.Fatalf("reset kept the history tab: %v", tt.Texts())
	}
}

func TestPreviewOutOfBandActionsStayInMemory(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "任务 · 待运行"))
	s := p.state
	s.work(func() (string, *core.ApiError) { t.Fatal("preview executed closure"); return "", nil })
	s.startEngine()
	s.executeBinding("unused", nil)
	s.bindDiscovered(42, "http://127.0.0.1:48857")
	s.retryBenchmark()
	s.refresh(true)
	s.refreshAction()
	p.advance(20 * time.Second)
	if command, err := s.bindCommand("42", "http://127.0.0.1:48857"); command != "" || err != nil {
		t.Fatal(command, err)
	}
	if s.service != nil || s.taskCancel != nil {
		t.Fatal("preview executed a real bind")
	}
	p.advance(20 * time.Second)
	if s.busy || s.binding || s.taskCancel != nil || s.service != nil || s.engineState.State != "running" {
		t.Fatal("preview bind did not stay in memory", s.busy, s.engineState.State)
	}
}

func TestPreviewPrepareAndContextSpeedSmoke(t *testing.T) {
	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1120, 2200)
	click(t, tt, "准备环境")
	p.advance(600 * time.Millisecond)
	tt.Frame()
	p.advance(600 * time.Millisecond)
	tt.Frame()
	if p.state.busy || !p.state.preflightReady() || !tt.HasText("开始测评") {
		t.Fatalf("environment preparation did not unlock benchmark: %v", tt.Texts())
	}
	click(t, tt, "长上下文速度")
	tt.Frame()
	if !tt.HasText("上下文档位") {
		t.Fatalf("context-speed view did not render: %v", tt.Texts())
	}
}

func TestPreviewScenesAllLoadAndSaveSettingsPreservesProxy(t *testing.T) {
	if len(previewScenes) == 0 || previewScenes[0].label != "任务 · 准备" {
		t.Fatalf("previewScenes[0] must be 任务 · 准备, got %+v", previewScenes)
	}

	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1120, 2200)
	for i, scene := range previewScenes {
		p.loadScene(i)
		if scene.label != "下载 · 进行中与失败" && scene.label != "下载 · PolyStrata 引导安装" && scene.label != "下载 · PolyStrata 模型下载" && len(p.state.downloads) != 0 {
			t.Fatalf("scene %q unexpectedly seeded %d downloads", scene.label, len(p.state.downloads))
		}
		p.themeMode = 1
		tt.Frame()
		p.themeMode = 2
		tt.Frame()
	}

	p.loadScene(0)
	p.dispatch(previewSettings{patch: map[string]json.RawMessage{
		"download_proxy": json.RawMessage(`"http://127.0.0.1:7890"`),
	}})
	if got := emptyText(p.state.settings.Effective.DownloadProxy); got != "http://127.0.0.1:7890" {
		t.Fatalf("Effective.DownloadProxy = %q, want http://127.0.0.1:7890", got)
	}
}

func TestPreviewResolveModelURLAndDetectedEngines(t *testing.T) {
	p := newPreviewSession(context.Background())

	qwen, _ := core.CatalogEntryByID("model-qwen3-0.6b-q8")
	resGGUF := p.resolveModelURL(qwen.MirrorURL)
	if resGGUF.Platform != "modelscope" || resGGUF.Revision != qwen.Revision || resGGUF.Filename != "Qwen3-0.6B-Q8_0.gguf" {
		t.Fatalf("unexpected preview GGUF resolve: %+v", resGGUF)
	}

	mlx, _ := core.CatalogEntryByID("model-qwen3-0.6b-mlx")
	resDir := p.resolveModelURL(mlx.URL)
	if resDir.Kind != "directory" || resDir.Revision != mlx.Revision || resDir.PinnedURL != mlx.URL {
		t.Fatalf("unexpected preview directory resolve: %+v", resDir)
	}

	detected := p.detectedEngines()
	if len(detected) < 2 {
		t.Fatalf("expected preview detected engines, got %+v", detected)
	}
}

func TestPreviewRetryDownloadDeduplicatesActiveRetry(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(0)
	src := core.DefaultModelURL()
	p.state.downloads = []core.DownloadTask{
		{TaskId: "dl-failed", Kind: "model", State: "failed", SourceUrl: src, OutName: "model"},
		{TaskId: "dl-cancelled", Kind: "model", State: "cancelled", SourceUrl: "https://example.invalid/cancelled.gguf", OutName: "cancelled"},
	}

	p.dispatch(previewRetryDownload{id: "dl-failed"})
	p.dispatch(previewRetryDownload{id: "dl-failed"})
	if len(p.state.downloads) != 3 {
		t.Fatalf("expected single active retry for failed preview task, got %+v", p.state.downloads)
	}

	p.dispatch(previewRetryDownload{id: "dl-cancelled"})
	p.dispatch(previewRetryDownload{id: "dl-cancelled"})
	if len(p.state.downloads) != 4 {
		t.Fatalf("expected single active retry for cancelled preview task, got %+v", p.state.downloads)
	}

	mirror := "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/resolve/master/Qwen3-0.6B-Q8_0.gguf"
	pinnedA := "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/blob/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/Qwen3-0.6B-Q8_0.gguf"
	pinnedB := "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/blob/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/Qwen3-0.6B-Q8_0.gguf"
	revA := core.DownloadTask{TaskId: "dl-rev-a", Kind: "model", State: "failed", SourceUrl: pinnedA, RequestedSourceUrl: mirror, OutName: "rev-a"}
	revB := core.DownloadTask{TaskId: "dl-rev-b", Kind: "model", State: "downloading", SourceUrl: pinnedB, RequestedSourceUrl: mirror, OutName: "rev-b"}
	p.state.downloads = append(p.state.downloads, revB, revA)
	if p.state.downloadRetryActive(revA) {
		t.Fatal("different pinned revision sharing RequestedSourceUrl blocked retry card")
	}
	p.dispatch(previewRetryDownload{id: "dl-rev-a"})
	p.dispatch(previewRetryDownload{id: "dl-rev-a"})
	if len(p.state.downloads) != 7 || p.state.downloads[0].SourceUrl != pinnedA {
		t.Fatalf("expected single retry for pinnedA alongside active pinnedB, got %+v", p.state.downloads)
	}
}

func TestPreviewPolystrataStarterAndManagedSetup(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "首次使用 · 无资源"))
	p.state.selected = "downloads"
	tt := previewTester(p, 1120, 2600)
	tt.Frame()

	if !tt.HasText("一键安装 PolyStrata") {
		t.Fatalf("expected PolyStrata starter button in preview first-use downloads: %v", tt.Texts())
	}
	click(t, tt, "一键安装 PolyStrata")
	if len(p.state.downloads) != 1 || p.state.downloads[0].Kind != "polystrata" || p.state.downloads[0].State != "downloading" {
		t.Fatalf("expected in-memory polystrata setup task: %+v", p.state.downloads)
	}
	if p.state.service != nil {
		t.Fatal("preview must never initialize real service")
	}
	p.advance(5 * time.Second)
	tt.Frame()
	if p.state.downloads[0].State != "done" || len(p.state.registry.Engines) != 1 || p.state.registry.Engines[0].Family != "polystrata" {
		t.Fatalf("expected simulated polystrata setup to finish and register engine: downloads=%+v engines=%+v", p.state.downloads, p.state.registry.Engines)
	}

	// Pre-seeded PolyStrata scene advances cleanly and retry preserves original family/model
	p.loadScene(previewSceneIndex(t, "下载 · PolyStrata 引导安装"))
	p.advance(5 * time.Second)
	tt.Frame()
	active := p.state.downloads[0]
	if active.State != "done" || active.Stage != "" || active.InstallationPath == nil || *active.InstallationPath == "" {
		t.Fatalf("pre-seeded polystrata task did not finish cleanly: %+v", active)
	}
	p.dispatch(previewRetryDownload{id: "preview-polystrata-blocked"})
	if p.state.downloads[0].DisplayName != "PolyStrata · qwen · Q2_0" {
		t.Fatalf("retry lost original polystrata family/model: %+v", p.state.downloads[0])
	}
	if p.state.unresolvedDownloadFailures() != 0 {
		t.Fatalf("expected retry to resolve failed polystrata count, got %d", p.state.unresolvedDownloadFailures())
	}

	// Retry of a failed preview-created setup preserves exact Backend, Context, and ModelSource
	p.cancelDownload(p.state.downloads[0].TaskId)
	origReq := core.PolystrataSetupRequest{Family: "coder", Model: "IQ1_M", Context: 65536, Backend: "hip", ModelSource: "huggingface"}
	p.failNext = true
	p.dispatch(previewPolystrataSetup{req: origReq})
	failedID := p.state.downloads[0].TaskId
	p.advance(2 * time.Second)
	p.state.ensureDownloadsUI().polystrataModelSource = "modelscope"
	p.dispatch(previewRetryDownload{id: failedID})
	retryID := p.state.downloads[0].TaskId
	if got := p.polystrataReqs[retryID]; got != origReq {
		t.Fatalf("preview retry lost original request fields: got %+v want %+v", got, origReq)
	}

	// Long-running model download preview advances smoothly, shows subtle log, and cancels via merged header button
	p.loadScene(previewSceneIndex(t, "下载 · PolyStrata 模型下载"))
	tt.Frame()
	beforeBytes := p.state.downloads[0].DownloadedBytes
	for _, want := range []string{"PolyStrata · qwen · Q2_0", "4 / 4", "下载模型", "安装中", "Qwen3.8-Flash-Next-GSQ-RCO-Q2_0-00001-of-00002.gguf"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in model download preview scene: %v", want, tt.Texts())
		}
	}
	if tt.HasText("任务详情") {
		t.Fatalf("PolyStrata model download preview must not show 任务详情: %v", tt.Texts())
	}
	p.advance(2 * time.Second)
	tt.Frame()
	if p.state.downloads[0].DownloadedBytes <= beforeBytes {
		t.Fatalf("expected preview model download bytes to advance: before=%d after=%d", beforeBytes, p.state.downloads[0].DownloadedBytes)
	}
	click(t, tt, "查看安装日志")
	if !tt.HasText("polystrata-server 构建就绪") {
		t.Fatalf("expected simulated PolyStrata install log lines: %v", tt.Texts())
	}
	click(t, tt, "取消安装")
	click(t, tt, "确认取消安装")
	tt.Frame()
	if p.state.downloads[0].State != "cancelled" || !tt.HasText("继续安装") {
		t.Fatalf("expected cancelled state and 继续安装 action after confirming header cancel: state=%q texts=%v", p.state.downloads[0].State, tt.Texts())
	}
}

func TestPreviewFirstUsePolystrataUnlocksAndRunsBenchmark(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "首次使用 · 无资源"))
	tt := previewTester(p, 1120, 2600)
	tt.Frame()

	click(t, tt, "准备环境")
	p.advance(1500 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("准备模型与引擎") {
		t.Fatalf("expected 准备模型与引擎 after 准备环境: %v", tt.Texts())
	}

	click(t, tt, "到下载页")
	if !tt.HasText("推荐组合") || !tt.HasText("PolyStrata") || !tt.HasText("一键安装 PolyStrata") {
		t.Fatalf("expected PolyStrata starter card on downloads page: %v", tt.Texts())
	}
	if tt.HasText("Windows x64") || tt.HasText("Linux x64") {
		t.Fatalf("unexpected non-macOS platform labels on downloads page: %v", tt.Texts())
	}

	click(t, tt, "一键安装 PolyStrata")
	click(t, tt, "返回任务")
	p.advance(6 * time.Second)
	tt.Frame()
	p.advance(2 * time.Second)
	tt.Frame()
	if !tt.HasText("开始测评") || !p.state.preflightReady() {
		t.Fatalf("expected 开始测评 unlocked after PolyStrata setup: ready=%v engine=%q model=%q texts=%v",
			p.state.preflightReady(), p.state.engine, p.state.model, tt.Texts())
	}

	click(t, tt, "开始测评")
	p.advance(2500 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("zh-core · 运行中") || !tt.HasText("zh-svg-pelican · 排队中") {
		t.Fatalf("expected running zh-core and queued zh-svg-pelican: %v", tt.Texts())
	}
	if p.state.engineState.State != "running" {
		t.Fatalf("expected running engine state, got %+v", p.state.engineState)
	}

	p.advance(30 * time.Second)
	tt.Frame()
	if !tt.HasText("查看报告") {
		t.Fatalf("expected 查看报告 after benchmark completion: %v", tt.Texts())
	}
	click(t, tt, "查看报告")
	if !tt.HasText("各类任务表现") {
		t.Fatalf("expected report categories after clicking 查看报告: %v", tt.Texts())
	}
}

func TestPreviewThemeSwitcherSyncsWithSidebarAndStaysInMemory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	p := newPreviewSession(context.Background())
	p.state.selected = "settings"
	var frameDark bool
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.themeFor(c))
		frameDark = c.Theme().Dark
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(205).Shrink(0).Children(func() { p.left(c) })
			p.content(c)
		})
	}, 1120, 2200)
	tt.Frame()

	// Switching in Settings updates previewSession and themeFor immediately without dirtying Settings.
	click(t, tt, "深色")
	if p.themeMode != 2 || p.state.themeMode != core.ThemeDark || p.state.settings.Effective.Theme != core.ThemeDark || !frameDark {
		t.Fatalf("settings dark switch failed: p.themeMode=%d state=%q eff=%q dark=%v",
			p.themeMode, p.state.themeMode, p.state.settings.Effective.Theme, frameDark)
	}
	if p.state.settingsUnsaved() || p.state.service != nil {
		t.Fatalf("preview settings should stay clean and service-free: unsaved=%v service=%v",
			p.state.settingsUnsaved(), p.state.service)
	}

	// Switching in debug sidebar syncs back to Settings state.
	click(t, tt, "系统")
	if p.themeMode != 0 || p.state.themeMode != core.ThemeSystem || p.state.settings.Effective.Theme != core.ThemeSystem {
		t.Fatalf("sidebar system switch did not sync: p.themeMode=%d state=%q eff=%q",
			p.themeMode, p.state.themeMode, p.state.settings.Effective.Theme)
	}

	click(t, tt, "深灰")
	if p.themeMode != 2 || p.state.themeMode != core.ThemeDark || p.state.settings.Effective.Theme != core.ThemeDark || !frameDark {
		t.Fatalf("sidebar dark switch did not sync: p.themeMode=%d state=%q eff=%q dark=%v",
			p.themeMode, p.state.themeMode, p.state.settings.Effective.Theme, frameDark)
	}

	click(t, tt, "跟随系统")
	if p.themeMode != 0 || p.state.themeMode != core.ThemeSystem || p.state.settings.Effective.Theme != core.ThemeSystem {
		t.Fatalf("settings system switch did not sync: p.themeMode=%d state=%q eff=%q",
			p.themeMode, p.state.themeMode, p.state.settings.Effective.Theme)
	}

	settingsOnly := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.themeFor(c))
		p.state.settingsView(c)
	}, 1120, 1800)
	settingsOnly.Frame()
	click(t, settingsOnly, "浅色")
	if p.themeMode != 1 || p.state.themeMode != core.ThemeLight || p.state.settings.Effective.Theme != core.ThemeLight {
		t.Fatalf("settings light switch failed: p.themeMode=%d state=%q eff=%q",
			p.themeMode, p.state.themeMode, p.state.settings.Effective.Theme)
	}

	// Simulated failure rolls back without losing current preview theme.
	p.failNext = true
	click(t, tt, "深色")
	if p.state.failure == nil || p.themeMode != 1 || p.state.themeMode != core.ThemeLight {
		t.Fatalf("failed preview switch did not roll back: failure=%+v p.themeMode=%d state=%q",
			p.state.failure, p.themeMode, p.state.themeMode)
	}

	// Scene switch preserves active preview theme.
	p.loadScene(1)
	if p.state.themeMode != core.ThemeLight || p.state.settings.Effective.Theme != core.ThemeLight {
		t.Fatalf("scene reload lost active preview theme: state=%q eff=%q",
			p.state.themeMode, p.state.settings.Effective.Theme)
	}

	var files []string
	if err := filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("preview mode wrote real files: %v", files)
	}
}
