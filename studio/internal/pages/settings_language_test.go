package pages

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func containsCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func TestLanguageSettingsViewSwitchAndPersist(t *testing.T) {
	s := settingsState()
	var applied []string
	saved := make(chan string, 2)
	s.applyNativeLocale = func(mode string) {
		applied = append(applied, mode)
	}
	s.persistLanguage = func(patch map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError) {
		var lang string
		_ = json.Unmarshal(patch["language"], &lang)
		saved <- lang
		next := s.settings
		next.Settings.Language = lang
		next.Effective.Language = lang
		return next, nil
	}

	tt := ui.NewTester(s.languageSettingsView, 800, 300)
	tt.SetLocale(l10n.LanguageZhHans)
	tt.Frame()
	if !tt.HasText("语言") || !tt.HasText("跟随系统") || !tt.HasText("English") || !tt.HasText("简体中文") {
		t.Fatalf("expected Chinese language settings card labels")
	}

	click(t, tt, "English")
	tt.SetLocale(l10n.LanguageEn)
	tt.Frame()

	select {
	case got := <-saved:
		if got != core.LanguageEn {
			t.Fatalf("persisted language = %q, want %q", got, core.LanguageEn)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for language persist")
	}
	if len(applied) == 0 || applied[len(applied)-1] != core.LanguageEn {
		t.Fatalf("expected applyNativeLocale(%q), got %v", core.LanguageEn, applied)
	}
	if !tt.HasText("Language") || !tt.HasText("Follow System") {
		t.Fatalf("expected English language settings card labels after switching")
	}
}

func TestAllMainPagesRenderEnglishWithoutChineseLeakage(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageEn)
	defer restore()

	pagesToCheck := []struct {
		id           string
		expectedText string
	}{
		{"benchmarks", "Benchmarks"},
		{"services", "Services"},
		{"library", "Library"},
		{"reports", "Reports"},
		{"settings", "Settings"},
	}

	for _, tc := range pagesToCheck {
		t.Run(tc.id, func(t *testing.T) {
			s := settingsState()
			s.selected = tc.id
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
					ui.Column(c).Width(205).Shrink(0).Children(func() { s.leftNav(c) })
					ui.Column(c).Grow(1).Children(func() { s.contentView(c) })
				})
			}, 1200, 1800)
			tt.SetLocale(l10n.LanguageEn)
			tt.Frame()

			if !tt.HasText(tc.expectedText) {
				t.Fatalf("page %s did not render expected English label %q; texts=%v", tc.id, tc.expectedText, tt.Texts())
			}
			for _, txt := range tt.Texts() {
				// "简体中文" is the autonym shown intentionally in the Language switcher.
				if strings.TrimSpace(txt) == "简体中文" {
					continue
				}
				if containsCJK(txt) {
					t.Errorf("page %s leaked CJK text in English locale: %q", tc.id, txt)
				}
			}
		})
	}
}
