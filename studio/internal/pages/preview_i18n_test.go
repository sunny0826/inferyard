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

	// Benchmark dataset labels coming from testdata/studio.json (Chinese benchmark content, not UI chrome).
	allowedFixtureData := map[string]bool{
		"简体中文": true,
		"数学计算": true,
		"知识问答": true,
	}

	// Render every preview scene in English and confirm no unintended CJK leaks in UI chrome.
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
}
