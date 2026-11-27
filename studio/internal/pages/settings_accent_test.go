package pages

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func TestAccentSettingsDefaultPersistAndRejectInvalid(t *testing.T) {
	defer setActiveAccent(core.AccentCobalt)

	service := settingsTestService(t, t.TempDir())
	if got := service.Accent(); got != core.AccentCobalt {
		t.Fatalf("default service.Accent() = %q, want %q", got, core.AccentCobalt)
	}
	if got := service.Settings().Effective.Accent; got != core.AccentCobalt {
		t.Fatalf("default Effective.Accent = %q, want %q", got, core.AccentCobalt)
	}

	raw, _ := json.Marshal(core.AccentTeal)
	resp, apiErr := service.UpdateSettings(map[string]json.RawMessage{"accent": raw})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(teal) failed: %+v", apiErr)
	}
	if resp.Settings.Accent != core.AccentTeal || resp.Effective.Accent != core.AccentTeal || service.Accent() != core.AccentTeal {
		t.Fatalf("persisted accent mismatch: settings=%q effective=%q service=%q",
			resp.Settings.Accent, resp.Effective.Accent, service.Accent())
	}

	badRaw, _ := json.Marshal("neon-pink")
	if _, apiErr := service.UpdateSettings(map[string]json.RawMessage{"accent": badRaw}); apiErr == nil {
		t.Fatalf("expected invalid accent to return error")
	}

	s := settingsState()
	s.service = service
	s.settings = resp
	s.loadThemeSettings(resp)
	tt := settingsTester(s)
	if _, ok := tt.Find("强调色"); !ok {
		t.Fatal("settings page must show 强调色")
	}
	if s.accentMode != core.AccentTeal || activeAccent() != core.AccentTeal {
		t.Fatalf("loaded accent mismatch: state=%q active=%q", s.accentMode, activeAccent())
	}

	// Verify optical icon buttons stay locked to Control.MD (36px).
	iconTT := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			primaryButton(c, "主按钮")
			leadingIconButton(c, components.ButtonPrimary, reicon.Play, "左图标")
			trailingIconButton(c, components.ButtonOutline, "右图标", reicon.ArrowUpRight)
			quietDangerButton(c, "静默危险")
		})
	}, 800, 200)
	iconTT.Frame()
	ref, _ := iconTT.AccessByRole(ui.RoleButton, "主按钮")
	for _, lbl := range []string{"左图标", "右图标", "静默危险"} {
		got, ok := iconTT.AccessByRole(ui.RoleButton, lbl)
		if !ok || math.Abs(float64(got.Bounds.H-ref.Bounds.H)) > 0.01 {
			t.Fatalf("button %q height = %.2f (ok=%v), want %.2f", lbl, got.Bounds.H, ok, ref.Bounds.H)
		}
	}
}
