//go:build ui_debug && !release

package pages

import (
	"context"
	"strings"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestPreviewLanguageSwitcherAndAllScenesEnglish(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageEn)
	defer restore()

	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1200, 2200)
	tt.SetLocale(l10n.LanguageEn)

	// Verify sidebar language switcher updates preview state and syncs with Settings.
	p.state.selected = "settings"
	tt.Frame()
	click(t, tt, "English")
	if p.languageMode != 1 || p.state.languageMode != core.LanguageEn || p.state.settings.Effective.Language != core.LanguageEn {
		t.Fatalf("preview English switch failed: mode=%d state=%q eff=%q",
			p.languageMode, p.state.languageMode, p.state.settings.Effective.Language)
	}

	// Only the Chinese language option button itself renders CJK in English mode.
	allowedFixtureData := map[string]bool{
		"简体中文": true,
	}

	// Render every preview scene in English and confirm no unintended CJK leaks.
	for i, scene := range previewScenes {
		p.loadScene(i)
		tt.Frame()
		for _, txt := range tt.Texts() {
			trimmed := strings.TrimSpace(txt)
			if trimmed == "" || allowedFixtureData[trimmed] {
				continue
			}
			if containsCJK(trimmed) {
				t.Errorf("preview scene %q leaked CJK text in English mode: %q", scene.labelKey, trimmed)
			}
		}
	}

	// Verify live hot-switching from zh-Hans -> en WITHOUT resetting the scene updates all seeded strings.
	for _, sceneKey := range []string{
		"preview.scene.engines_local_and_dir",
		"preview.scene.downloads_polystrata_setup",
		"preview.scene.report_compare_multi",
		"preview.scene.notifications_center",
	} {
		idx := -1
		for i, sc := range previewScenes {
			if sc.labelKey == sceneKey {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("missing scene %s", sceneKey)
		}
		_ = l10n.SetTestLocale(l10n.LanguageZhHans)
		tt.SetLocale(l10n.LanguageZhHans)
		p.languageMode = 2
		p.loadScene(idx)
		tt.Frame()

		// Hot-switch to English without calling loadScene.
		_ = l10n.SetTestLocale(l10n.LanguageEn)
		tt.SetLocale(l10n.LanguageEn)
		p.languageMode = 1
		tt.Frame()
		for _, txt := range tt.Texts() {
			trimmed := strings.TrimSpace(txt)
			if trimmed == "" || allowedFixtureData[trimmed] {
				continue
			}
			if containsCJK(trimmed) {
				t.Errorf("hot-switching scene %q from zh-Hans to English left stale CJK text: %q", sceneKey, trimmed)
			}
		}
	}
}
