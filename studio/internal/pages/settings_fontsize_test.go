package pages

import (
	"encoding/json"
	"math"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
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

	paletteBase := theme.Palette(false).FontSize
	mainDefault := paletteBase * 0.96
	tiers := []string{core.FontSizeSmaller, core.FontSizeSmall, core.FontSizeDefault, core.FontSizeLarge, core.FontSizeLarger}
	gotBases := make([]float32, len(tiers))
	for i, tier := range tiers {
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
		gotBases[i] = gotBase
		wantHeading := gotBase * (18.0 / 14.0)
		if math.Abs(float64(gotHeading-wantHeading)) > 0.05 {
			t.Fatalf("tier %q PageTitle = %.2f, want %.2f", tier, gotHeading, wantHeading)
		}
	}
	if math.Abs(float64(gotBases[2]-mainDefault)) > 0.01 {
		t.Fatalf("Default tier FontSize = %.2f, want main-branch size %.2f", gotBases[2], mainDefault)
	}
	if runtime.GOOS == "darwin" && math.Abs(float64(gotBases[4]-14.0)) > 0.01 {
		t.Fatalf("Larger tier FontSize on darwin = %.2f, want 14.00", gotBases[4])
	}
	step := gotBases[1] - gotBases[0]
	if step <= 0 {
		t.Fatalf("expected positive step between tiers, got %.4f (%v)", step, gotBases)
	}
	for i := 1; i < len(gotBases); i++ {
		if diff := gotBases[i] - gotBases[i-1]; math.Abs(float64(diff-step)) > 0.01 {
			t.Fatalf("non-uniform step between tier %d and %d: got %.4f, want %.4f (%v)", i-1, i, diff, step, gotBases)
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
