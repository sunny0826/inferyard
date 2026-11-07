//go:build ui_debug && !release

package pages

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
)

func candidateStyleFor(t *testing.T, index int, dark bool) (*ui.Theme, designTokens) {
	t.Helper()
	var theme *ui.Theme
	var set designTokens
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(candidateTheme(index, c))
		theme, set = c.Theme(), tokens(c)
	}, 200, 200)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.SetDark(dark)
	tt.Frame()
	return theme, set
}

// TestPreviewCandidateSwitchesStructure covers the product rule that a
// candidate is a token set with real layout differences, not a recolor. Every
// candidate must change at least one density, radius, card or type value.
func TestPreviewCandidateSwitchesStructure(t *testing.T) {
	_, base := candidateStyleFor(t, 0, false)
	for i := 1; i < len(previewCandidates); i++ {
		_, set := candidateStyleFor(t, i, false)
		changed := set.Density.Page != base.Density.Page ||
			set.Radius.Surface != base.Radius.Surface ||
			set.Card.Edge != base.Card.Edge ||
			set.Card.Padding != base.Card.Padding ||
			set.Type.PageTitle != base.Type.PageTitle
		if !changed {
			t.Fatalf("candidate %q only changes colors", previewCandidates[i].id)
		}
	}
}

// TestCandidateSideColorsRemainDistinguishable covers the comparison rule that
// the two sides of an A/B result must stay distinguishable in both palettes.
// Bars are graphical objects, so each side keeps 3:1 contrast with the
// candidate's own surface.
func TestCandidateSideColorsRemainDistinguishable(t *testing.T) {
	for i := range previewCandidates {
		for _, dark := range []bool{false, true} {
			assertSideColors(t, i, dark)
		}
	}
}

func assertSideColors(t *testing.T, index int, dark bool) {
	t.Helper()
	theme, set := candidateStyleFor(t, index, dark)
	if set.Sides.A == set.Sides.B {
		t.Fatalf("candidate %q shares a side color", previewCandidates[index].id)
	}
	for side, color := range map[string]ui.Color{"A": set.Sides.A, "B": set.Sides.B} {
		if ratio := contrastRatio(color, theme.Surface); ratio < 3 {
			t.Fatalf("candidate %q side %s contrast %.2f on dark=%v", previewCandidates[index].id, side, ratio, dark)
		}
	}
}

// TestCandidateStyleDoesNotLeakIntoDefaultTheme covers the boundary that a
// candidate is installed only in the preview session: the stock theme must
// still resolve the default tokens after a candidate was rendered.
func TestCandidateStyleDoesNotLeakIntoDefaultTheme(t *testing.T) {
	_, _ = candidateStyleFor(t, 2, true)
	var set designTokens
	tt := ui.NewTester(func(c *ui.Context) { set = tokens(c) }, 200, 200)
	tt.Frame()
	if set.Card.Edge != 0 || set.Radius.Surface != 12 {
		t.Fatalf("preview candidate leaked into the default theme: %+v", set.Card)
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

func contrastRatio(a, b ui.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + .05) / (lb + .05)
}

func relativeLuminance(c ui.Color) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= .03928 {
			return s / 12.92
		}
		return math.Pow((s+.055)/1.055, 2.4)
	}
	return .2126*channel(c.R) + .7152*channel(c.G) + .0722*channel(c.B)
}
