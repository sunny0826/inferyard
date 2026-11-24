//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPreviewTraySwitchAndRollback(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.state.selected = "settings"

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.themeFor(c))
		p.state.settingsView(c)
	}, 1120, 1800)
	tt.Frame()

	if p.state.minimizeToTray || p.state.settings.Effective.MinimizeToTray {
		t.Fatalf("expected preview minimizeToTray initially false")
	}

	click(t, tt, "最小化到系统托盘")
	tt.Frame()
	if !p.state.minimizeToTray || !p.state.settings.Settings.MinimizeToTray || !p.state.settings.Effective.MinimizeToTray {
		t.Fatalf("expected preview tray toggle to persist in session: state=%v settings=%v eff=%v",
			p.state.minimizeToTray, p.state.settings.Settings.MinimizeToTray, p.state.settings.Effective.MinimizeToTray)
	}

	p.failNext = true
	click(t, tt, "最小化到系统托盘")
	tt.Frame()
	if p.state.failure == nil || !p.state.minimizeToTray || !p.state.settings.Settings.MinimizeToTray {
		t.Fatalf("expected failed preview tray toggle to roll back to true: failure=%+v state=%v saved=%v",
			p.state.failure, p.state.minimizeToTray, p.state.settings.Settings.MinimizeToTray)
	}
}
