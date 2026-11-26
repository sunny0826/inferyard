//go:build ui_debug && !release

package pages

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func previewSceneZhLabel(labelKey string) string {
	return l10n.T(l10n.LanguageZhHans, labelKey)
}

func previewTester(p *previewSession, width, height int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.themeFor(c))
		opts := p.shellOptions()
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(205).Shrink(0).Children(func() { p.left(c) })
			ui.Box(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				p.content(c)
				if opts.ContentTitle != nil {
					ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(sidebar.HeaderHeight(c)).
						AlignItems(ui.Center).Padding(6, 16, 6, 16).Children(func() {
						opts.ContentTitle(c)
					})
				}
			})
		})
	}, width, height)
}

func previewSceneIndex(t *testing.T, label string) int {
	t.Helper()
	for i, scene := range previewScenes {
		if previewSceneZhLabel(scene.labelKey) == label {
			return i
		}
	}
	t.Fatal("unknown scene", label)
	return -1
}

func TestPreviewOutOfBandActionsStayInMemory(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "测评 · 待运行"))
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

func TestPreviewScenesAllLoadAndSaveSettingsPreservesProxy(t *testing.T) {
	if len(previewScenes) == 0 || previewSceneZhLabel(previewScenes[0].labelKey) != "测评 · 环境未就绪" {
		t.Fatalf("previewScenes[0] must be 测评 · 环境未就绪, got %+v", previewScenes)
	}

	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1120, 2200)
	for i, scene := range previewScenes {
		p.loadScene(i)
		label := previewSceneZhLabel(scene.labelKey)
		if label != "模型库 · 下载进行中与失败" && label != "模型库 · PolyStrata 引导安装与网络提示" && label != "模型库 · PolyStrata 模型包下载" && len(p.state.downloads) != 0 {
			t.Fatalf("scene %q unexpectedly seeded %d downloads", label, len(p.state.downloads))
		}
		p.themeMode = 1
		tt.Frame()
		for _, retiredFold := range []string{"报告详情", "间隔详情", "任务详情", "原始字段", "运行原始详情", "查看准备详情", "测评条件"} {
			if tt.HasText(retiredFold) {
				t.Fatalf("scene %q still renders retired card fold %q", label, retiredFold)
			}
		}
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
	p.loadScene(previewSceneIndex(t, "测评 · 待运行"))
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

func TestPreviewCreateReportCancelledPelicanDoesNotFabricateOKSVG(t *testing.T) {
	p := newPreviewSession(context.Background())
	s := p.state
	runPath := "/preview/results/pelican-cancelled"
	bundle := "zh-svg-pelican"
	s.history.StandaloneRuns = append(s.history.StandaloneRuns, core.RunSummary{
		RunId:  "pelican-cancelled",
		Path:   runPath,
		Bundle: &bundle,
		Status: "cancelled",
	})
	p.details["pelican-cancelled"] = &core.RunTaskDetail{
		Task: core.RunTask{TaskId: "pelican-cancelled", State: "cancelled", Bundle: &bundle},
		Progress: &core.RunProgress{
			Done:  0,
			Total: previewValue(int64(1)),
			Cases: []core.CaseProgress{{
				CaseId:   "svg-pelican-01",
				Category: previewValue("svg"),
				State:    "cancelled",
			}},
		},
	}
	id := p.createReport([]string{runPath})
	report, err := p.report(id)
	if err != nil || len(report.Runs) != 1 || len(report.Runs[0].SVGOutputs) != 1 {
		t.Fatalf("unexpected cancelled pelican preview report: report=%+v err=%v", report, err)
	}
	got := report.Runs[0].SVGOutputs[0]
	if got.Status != "not_found" || got.SVG != nil || got.Bytes != 0 {
		t.Fatalf("cancelled pelican run must not fabricate ok SVG, got %+v", got)
	}

	// Completed history entry without matching SVG request details must not fabricate ok SVG either.
	noDetailPath := "/preview/results/pelican-no-detail"
	s.history.StandaloneRuns = append(s.history.StandaloneRuns, core.RunSummary{
		RunId:  "pelican-no-detail",
		Path:   noDetailPath,
		Bundle: &bundle,
		Status: "complete",
	})
	noDetailID := p.createReport([]string{noDetailPath})
	noDetailReport, err := p.report(noDetailID)
	if err != nil || len(noDetailReport.Runs) != 1 {
		t.Fatalf("unexpected no-detail pelican preview report: report=%+v err=%v", noDetailReport, err)
	}
	if len(noDetailReport.Runs[0].SVGOutputs) != 0 {
		t.Fatalf("run without svg request details must not fabricate SVG outputs, got %+v", noDetailReport.Runs[0].SVGOutputs)
	}
}

func TestPreviewBenchmarkElapsedUsesVirtualClock(t *testing.T) {
	p := newPreviewSession(context.Background())
	readyIdx := previewSceneIndex(t, "测评 · 待运行")
	p.loadScene(readyIdx)
	// Advance to a running benchmark via preview clock:
	// 400ms (prepare->bind) + 800ms (bind->running) = 1.2s start timestamp.
	p.beginBenchmark(previewBenchmark{
		model:   p.state.model,
		engine:  p.state.engine,
		bundles: []string{inferyard.DefaultBundlePelican},
	})
	// At 2.0s virtual clock, only 0.8s has elapsed since 1.2s start -> must stay "< 1s", not premature "1s".
	p.advance(2000 * time.Millisecond)

	tt := ui.NewTester(p.content, 1120, 800)
	tt.Frame()

	wantUnder1s := l10n.TRf("common.elapsed.running", "duration", formatElapsedDurationFor(l10n.LanguageZhHans, 800*time.Millisecond))
	textsAt2s := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(textsAt2s, wantUnder1s) {
		t.Fatalf("expected sub-second preview elapsed %q at 0.8s into run, got:\n%s", wantUnder1s, textsAt2s)
	}

	// Advancing another 300ms (total 1.1s into run) crosses 1 full second.
	p.advance(300 * time.Millisecond)
	tt.Frame()

	want1s := l10n.TRf("common.elapsed.running", "duration", formatElapsedDurationFor(l10n.LanguageZhHans, 1100*time.Millisecond))
	textsAt2300ms := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(textsAt2300ms, want1s) {
		t.Fatalf("expected preview virtual clock elapsed %q at 1.1s into run, got:\n%s", want1s, textsAt2300ms)
	}

	// Verify preview context speed return-to-list -> completion -> open result from Recent row.
	p.loadScene(readyIdx)
	p.state.speed.selected = true
	p.startSpeed(previewSpeedStart{
		model:   p.state.model,
		engine:  p.state.engine,
		context: inferyard.ContextSpeed64K,
	})
	p.state.benchFlowOpen = false
	// Advance past all 5 samples + reading stage so speed task finishes while user is on Benchmarks list.
	p.advance(10 * time.Second)
	tt.Frame()
	afterSpeedDone := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(afterSpeedDone, l10n.TR("benchmarks.group.running_empty")) || strings.Contains(afterSpeedDone, l10n.TR("benchmarks.badge.active_flow")) {
		t.Fatalf("expected preview Benchmarks list Running section empty after speed completion, got:\n%s", afterSpeedDone)
	}
	click(t, tt, l10n.TR("shell.header.context_speed"))
	tt.Frame()
	if p.state.selected != "reports" || p.state.speed.opened == nil {
		t.Fatalf("expected clicking completed speed row in Recent to open speed result, got selected=%q opened=%v", p.state.selected, p.state.speed.opened)
	}
}
