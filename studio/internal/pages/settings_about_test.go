package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestSettingsAboutTabDisplaysBuildInfoAndCopies(t *testing.T) {
	s := settingsState()
	s.buildInfo = func() core.BuildInfo {
		return core.BuildInfo{
			Version:   "0.0.14",
			BuildTime: "2026-11-25T03:30:00Z",
			GitCommit: "abcdef0123456789abcdef0123456789abcdef01",
		}
	}

	tt := settingsTester(s)
	tt.Frame()
	if !tt.HasText("通用") || !tt.HasText("关于") {
		t.Fatalf("settings tabs missing: %v", tt.Texts())
	}
	if !tt.HasText("外观") {
		t.Fatalf("default settings tab should show general settings: %v", tt.Texts())
	}

	// Unsaved edits in General tab should survive switching to About and back.
	s.workspace = "/draft/workspace"
	click(t, tt, "关于")
	if s.settingsTab != settingsTabAbout {
		t.Fatalf("expected settingsTabAbout, got %d", s.settingsTab)
	}
	for _, want := range []string{
		"关于 InferYard Studio",
		"版本",
		"v0.0.14",
		"构建时间",
		"2026-11-25 03:30:00 UTC",
		"Git Commit ID",
		"abcdef0123456789abcdef0123456789abcdef01",
		"复制版本信息",
	} {
		if !tt.HasText(want) {
			t.Fatalf("about tab missing %q: %v", want, tt.Texts())
		}
	}
	if tt.HasText("外观") {
		t.Fatalf("about tab should not render general settings: %v", tt.Texts())
	}

	click(t, tt, "复制版本信息")
	gotClip := tt.Clipboard()
	for _, want := range []string{
		"版本: v0.0.14",
		"构建时间: 2026-11-25 03:30:00 UTC",
		"Git Commit ID: abcdef0123456789abcdef0123456789abcdef01",
	} {
		if !strings.Contains(gotClip, want) {
			t.Fatalf("clipboard %q missing %q", gotClip, want)
		}
	}

	click(t, tt, "通用")
	if s.settingsTab != settingsTabGeneral {
		t.Fatalf("expected settingsTabGeneral, got %d", s.settingsTab)
	}
	if s.workspace != "/draft/workspace" || !s.settingsUnsaved() {
		t.Fatalf("switching tabs lost unsaved workspace draft: workspace=%q unsaved=%v", s.workspace, s.settingsUnsaved())
	}
}

func TestSettingsAboutMissingAndDirtyFormatting(t *testing.T) {
	s := settingsState()
	s.settingsTab = settingsTabAbout
	s.buildInfo = func() core.BuildInfo {
		return core.BuildInfo{
			Version:   "dev",
			BuildTime: "",
			GitCommit: "0123456789abcdef0123456789abcdef01234567",
			Dirty:     true,
		}
	}

	tt := settingsTester(s)
	tt.Frame()
	if !tt.HasText("dev") || !tt.HasText("未知") || !tt.HasText("0123456789abcdef0123456789abcdef01234567-dirty") {
		t.Fatalf("unexpected dev/dirty about rendering: %v", tt.Texts())
	}
	if tt.HasText("vdev") {
		t.Fatalf("dev version must not have leading v prefix: %v", tt.Texts())
	}

	if got := formatAboutBuildTime("custom-stamp"); got != "custom-stamp" {
		t.Fatalf("non-RFC3339 build time should be preserved verbatim, got %q", got)
	}
	if got := formatAboutVersion(""); got != "" {
		t.Fatalf("empty version should stay empty, got %q", got)
	}
	if got := formatAboutCommit("", true); got != "" {
		t.Fatalf("empty commit must stay empty even when dirty, got %q", got)
	}
}

func TestSettingsAboutTabEnglishHasNoChineseLeakage(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageEn)
	defer restore()

	s := settingsState()
	s.settingsTab = settingsTabAbout
	s.buildInfo = func() core.BuildInfo {
		return core.BuildInfo{
			Version:   "0.0.14",
			BuildTime: "2026-11-25T03:30:00Z",
			GitCommit: "1234567890abcdef1234567890abcdef12345678",
		}
	}

	tt := ui.NewTester(s.settingsView, 1120, 1200)
	tt.SetLocale(l10n.LanguageEn)
	tt.Frame()

	for _, want := range []string{"General", "About", "About InferYard Studio", "Version", "Build Time", "Git Commit ID", "Copy Version Info"} {
		if !tt.HasText(want) {
			t.Fatalf("English about tab missing %q: %v", want, tt.Texts())
		}
	}
	for _, txt := range tt.Texts() {
		if containsCJK(txt) {
			t.Errorf("English about tab leaked CJK text: %q", txt)
		}
	}
}

func TestDirectedSettingsShortcutReturnsToGeneralTabFromAbout(t *testing.T) {
	s := settingsState()
	tt := ui.NewTester(s.view, 1200, 1800)
	tt.Frame()

	click(t, tt, "关于")
	if s.settingsTab != settingsTabAbout {
		t.Fatalf("expected settingsTabAbout, got %d", s.settingsTab)
	}

	click(t, tt, "模型库")
	if s.selected != "library" {
		t.Fatalf("expected library page, got %q", s.selected)
	}

	click(t, tt, "下载设置")
	if s.selected != "settings" || s.settingsTab != settingsTabGeneral {
		t.Fatalf("directed settings button did not open General tab: selected=%q tab=%d", s.selected, s.settingsTab)
	}
	if !tt.HasText("网络与代理") || tt.HasText("关于 InferYard Studio") {
		t.Fatalf("expected General settings content after Download Settings shortcut: %v", tt.Texts())
	}
}
