//go:build ui_debug && !release

package pages

import (
	"bytes"
	"context"
	"image"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// pageProbe renders the shared surfaces that depend on the installed theme and
// token set, so two install entries can be compared by their visible output.
func pageProbe(c *ui.Context) {
	t := tokens(c)
	ui.Column(c).Fill().Padding(t.Density.Page).Gap(t.Density.Section).Children(func() {
		card(c, "组合状态", func() {
			ui.Row(c).Gap(t.Density.Row).Children(func() {
				ui.Text(c, "Preview-Model × preview-engine").Bold()
				statusBadge(c, "运行中", stateColor(c, stateActive)).Description("当前状态")
			})
			pairedBar(c, "通过率", pairedBarValue{Label: "A", Value: .72, Text: "72%"}, pairedBarValue{Label: "B", Value: .54, Text: "54%"})
		})
		stepTrack(c, "5", "运行", func() { ui.Text(c, "42 / 120 题") })
	})
}

func probeImage(install func(*ui.Context) *ui.Theme) *image.RGBA {
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(install(c))
		pageProbe(c)
	}, 480, 600)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Advance(2 * time.Second)
	return tt.Image()
}

func sameImage(a, b *image.RGBA) bool {
	return a.Rect == b.Rect && bytes.Equal(a.Pix, b.Pix)
}

// TestPreviewDefaultMatchesProduction covers that the debug window installs
// the production scheme through its own shell entry. Both sides render the
// same probe with their real install paths and must paint the same result.
func TestPreviewDefaultMatchesProduction(t *testing.T) {
	production := probeImage(productionThemeInstall())
	preview := probeImage(newPreviewSession(context.Background()).shellOptions().Theme)
	if !sameImage(production, preview) {
		t.Fatal("preview default paints differently from the production window")
	}
}

// TestPreviewCandidateSwitchChangesRendering covers that choosing a candidate
// changes the visible scheme in this window.
func TestPreviewCandidateSwitchChangesRendering(t *testing.T) {
	p := newPreviewSession(context.Background())
	current := probeImage(p.shellOptions().Theme)
	p.selectCandidate(1)
	comparison := probeImage(p.shellOptions().Theme)
	if sameImage(current, comparison) {
		t.Fatal("candidate switch did not change the rendered scheme")
	}
}

// TestCandidateStyleDoesNotLeakIntoDefaultTheme covers that installing a
// comparison candidate in the preview window never changes the production
// window's tokens.
func TestCandidateStyleDoesNotLeakIntoDefaultTheme(t *testing.T) {
	production := probeImage(productionThemeInstall())
	p := newPreviewSession(context.Background())
	p.selectCandidate(1)
	comparison := probeImage(p.shellOptions().Theme)
	if comparison == nil {
		t.Fatal("comparison candidate did not render")
	}
	after := probeImage(productionThemeInstall())
	if !sameImage(production, after) {
		t.Fatal("preview candidate leaked into the production window")
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
