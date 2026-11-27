package pages

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestFontSizeSettingsDefaultPersistScaleAndUI(t *testing.T) {
	restoreLocale := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restoreLocale()
	defer setActiveFontSize(core.FontSizeDefault)

	service := settingsTestService(t, t.TempDir())
	if got := service.FontSize(); got != core.FontSizeDefault {
		t.Fatalf("default service.FontSize() = %q, want %q", got, core.FontSizeDefault)
	}
	if got := service.Settings().Effective.FontSize; got != core.FontSizeDefault {
		t.Fatalf("default Effective.FontSize = %q, want %q", got, core.FontSizeDefault)
	}

	wantPx := map[string]float32{
		core.FontSizeSmaller: 12,
		core.FontSizeSmall:   13,
		core.FontSizeDefault: 14,
		core.FontSizeLarge:   15,
		core.FontSizeLarger:  16,
	}
	for _, tier := range []string{core.FontSizeSmaller, core.FontSizeSmall, core.FontSizeDefault, core.FontSizeLarge, core.FontSizeLarger} {
		raw, _ := json.Marshal(tier)
		resp, apiErr := service.UpdateSettings(map[string]json.RawMessage{"font_size": raw})
		if apiErr != nil {
			t.Fatalf("UpdateSettings(%q) failed: %+v", tier, apiErr)
		}
		if resp.Settings.FontSize != tier || resp.Effective.FontSize != tier || service.FontSize() != tier {
			t.Fatalf("persisted font_size mismatch for %q: settings=%q effective=%q service=%q",
				tier, resp.Settings.FontSize, resp.Effective.FontSize, service.FontSize())
		}
		setActiveFontSize(tier)
		var gotBase, gotHeading float32
		tt := ui.NewTester(func(c *ui.Context) {
			th := studioThemeFor(c, false)
			gotBase = th.FontSize
			gotHeading = typeTokensFor(th).PageTitle
		}, 400, 120)
		tt.Frame()
		if math.Abs(float64(gotBase-wantPx[tier])) > 0.01 {
			t.Fatalf("tier %q base FontSize = %.2f, want %.2f", tier, gotBase, wantPx[tier])
		}
		wantHeading := wantPx[tier] * (18.0 / 14.0)
		if math.Abs(float64(gotHeading-wantHeading)) > 0.05 {
			t.Fatalf("tier %q PageTitle = %.2f, want %.2f", tier, gotHeading, wantHeading)
		}
	}

	// Resetting to null restores default.
	resp, apiErr := service.UpdateSettings(map[string]json.RawMessage{"font_size": json.RawMessage("null")})
	if apiErr != nil || resp.Effective.FontSize != core.FontSizeDefault {
		t.Fatalf("null font_size reset failed: err=%+v effective=%q", apiErr, resp.Effective.FontSize)
	}

	// Invalid tier is rejected.
	badRaw, _ := json.Marshal("giant")
	if _, apiErr := service.UpdateSettings(map[string]json.RawMessage{"font_size": badRaw}); apiErr == nil {
		t.Fatal("expected invalid font_size to return error")
	}

	// UI dropdown renders in Appearance card and switches font size on selection.
	setActiveFontSize(core.FontSizeDefault)
	s := settingsState()
	s.service = nil
	s.settings = resp
	s.loadThemeSettings(resp)
	tt := settingsTester(s)
	tt.Frame()
	if !tt.HasText("字号") || !tt.HasText("默认") {
		t.Fatalf("expected Appearance card to render 字号 and 默认, got %v", tt.Texts())
	}

	click(t, tt, "默认")
	tt.Frame()
	for _, optionLabel := range []string{"更小", "小", "默认", "大", "更大"} {
		if !tt.HasText(optionLabel) {
			t.Fatalf("expected open font-size popup to show %q, got %v", optionLabel, tt.Texts())
		}
	}

	click(t, tt, "大")
	tt.Frame()
	if s.fontSizeMode != core.FontSizeLarge || activeFontSize() != core.FontSizeLarge {
		t.Fatalf("expected selecting 大 to set fontSizeMode=%q active=%q", s.fontSizeMode, activeFontSize())
	}
}
