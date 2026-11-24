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
	"inferyard/studio/internal/l10n"
)

func previewSceneZhLabel(labelKey string) string {
	return l10n.T(l10n.LanguageZhHans, labelKey)
}

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
		if previewSceneZhLabel(scene.labelKey) == label {
			return i
		}
	}
	t.Fatal("unknown scene", label)
	return -1
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

func TestPreviewScenesAllLoadAndSaveSettingsPreservesProxy(t *testing.T) {
	if len(previewScenes) == 0 || previewSceneZhLabel(previewScenes[0].labelKey) != "任务 · 准备" {
		t.Fatalf("previewScenes[0] must be 任务 · 准备, got %+v", previewScenes)
	}

	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1120, 2200)
	for i, scene := range previewScenes {
		p.loadScene(i)
		label := previewSceneZhLabel(scene.labelKey)
		if label != "下载 · 进行中与失败" && label != "下载 · PolyStrata 引导安装" && label != "下载 · PolyStrata 模型下载" && len(p.state.downloads) != 0 {
			t.Fatalf("scene %q unexpectedly seeded %d downloads", label, len(p.state.downloads))
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
