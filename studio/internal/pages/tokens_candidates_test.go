//go:build ui_debug && !release

package pages

import (
	"context"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestPreviewDefaultThemeKeepsProductionReadability covers the preview window's
// first candidate: it must install a scheme whose body and muted text stay
// readable, whose states stay distinguishable and whose A/B sides remain
// visible against the card surface, in both appearances. It does not pin the
// exact palette or compare rendered images.
func TestPreviewDefaultThemeKeepsProductionReadability(t *testing.T) {
	for _, dark := range []bool{false, true} {
		assertPreviewDefaultTheme(t, dark)
	}
}

func assertPreviewDefaultTheme(t *testing.T, dark bool) {
	t.Helper()
	var theme *ui.Theme
	var a, b, active, neutral ui.Color
	install := newPreviewSession(context.Background()).shellOptions().Theme
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(install(c))
		theme = c.Theme()
		a, b = sideColor(c, sideA), sideColor(c, sideB)
		active, neutral = stateColor(c, stateActive), stateColor(c, stateNeutral)
	}, 40, 40)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.SetDark(dark)
	tt.Frame()
	if theme == nil {
		t.Fatalf("dark=%v preview default did not install a theme", dark)
	}
	if ratio := contrastRatio(theme.Text, theme.Background); ratio < 7 {
		t.Fatalf("dark=%v preview body text %.2f", dark, ratio)
	}
	for _, surface := range []ui.Color{theme.Background, theme.Surface} {
		if ratio := contrastRatio(theme.TextMuted, surface); ratio < 4.5 {
			t.Fatalf("dark=%v preview muted text %.2f", dark, ratio)
		}
	}
	if active == neutral {
		t.Fatalf("dark=%v preview active and neutral states share a color", dark)
	}
	if a == b {
		t.Fatalf("dark=%v preview A/B sides share a color", dark)
	}
	for name, color := range map[string]ui.Color{"A": a, "B": b} {
		if ratio := contrastRatio(color, theme.Surface); ratio < 3 {
			t.Fatalf("dark=%v preview side %s contrast against surface %.2f < 3", dark, name, ratio)
		}
	}
}

// TestPreviewCandidateSwitchKeepsSessionIsolated covers the preview boundary:
// choosing a candidate changes presentation only, never page data, settings or
// a service.
func TestPreviewCandidateSwitchKeepsSessionIsolated(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "下载 · 进行中与失败"))
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.theme(c))
		p.content(c)
	}, 1120, 2400)
	tt.Frame()
	before := tt.Texts()
	settings, downloads := p.state.settings, p.state.downloads
	p.selectCandidate(2)
	tt.Frame()
	if p.state.service != nil || p.state.busy || p.state.preview == nil {
		t.Fatal("candidate switch escaped the preview session")
	}
	if !reflect.DeepEqual(settings, p.state.settings) || !reflect.DeepEqual(downloads, p.state.downloads) {
		t.Fatal("candidate switch changed page data")
	}
	after := tt.Texts()
	if len(before) != len(after) {
		t.Fatalf("candidate switch changed page content: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("candidate switch changed page text %q -> %q", before[i], after[i])
		}
	}
	if p.scene != previewSceneIndex(t, "下载 · 进行中与失败") {
		t.Fatal("candidate switch reloaded the scene")
	}
}
