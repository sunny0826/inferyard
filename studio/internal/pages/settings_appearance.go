package pages

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

type appearanceOption struct {
	value    string
	labelKey string
	icon     func() *ui.SVG
}

var appearanceOptions = []appearanceOption{
	{value: core.ThemeSystem, labelKey: "settings.appearance.system", icon: lucide.Monitor},
	{value: core.ThemeLight, labelKey: "settings.appearance.light", icon: lucide.Sun},
	{value: core.ThemeDark, labelKey: "settings.appearance.dark", icon: lucide.Moon},
}

func themeFromSettings(v core.SettingsResponse) string {
	if strings.TrimSpace(v.Effective.Theme) != "" {
		return normalizeThemeMode(v.Effective.Theme)
	}
	return normalizeThemeMode(v.Settings.Theme)
}

func (s *viewState) syncNativeThemeIfNeeded() {
	mode := normalizeThemeMode(s.themeMode)
	if s.appliedThemeMode == mode {
		return
	}
	if s.applyNativeTheme != nil {
		s.appliedThemeMode = mode
		s.applyNativeTheme(mode)
		return
	}
	if s.win == nil {
		return
	}
	s.appliedThemeMode = mode
	syncNativeTheme(mode)
}

func (s *viewState) themeFor(c *ui.Context) *ui.Theme {
	setActiveAccent(s.accentMode)
	s.syncNativeThemeIfNeeded()
	return resolveStudioTheme(c, s.themeMode)
}

func (s *viewState) loadThemeSettings(v core.SettingsResponse) {
	s.loadAccentSettings(v)
	if s.themeSaving {
		return
	}
	s.themeMode = themeFromSettings(v)
	s.syncNativeThemeIfNeeded()
}

func (s *viewState) appearanceSettingsView(c *ui.Context) {
	t := tokens(c)
	selected := normalizeThemeMode(s.themeMode)
	idx := 0
	for i, opt := range appearanceOptions {
		if opt.value == selected {
			idx = i
			break
		}
	}

	applyMaterial(c, ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				heading(c, tr(c, "settings.appearance.heading"), 2).Grow(1)
				if selected == core.ThemeSystem {
					current := tr(c, "settings.appearance.current_light")
					if c.Theme().Dark {
						current = tr(c, "settings.appearance.current_dark")
					}
					ui.Text(c, current).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
				}
			})

			seg := ui.SegmentedBase(c, &idx, len(appearanceOptions))
			seg.Track.FillWidth().Gap(t.Density.CardGap).AlignItems(ui.Stretch).Label(tr(c, "settings.appearance.mode_label")).
				Children(func() {
					for i, opt := range appearanceOptions {
						s.appearanceOptionTile(c, seg.Segment(i), opt, i == idx)
					}
				}).
				FillEqually()
			if seg.Track.Changed() && idx >= 0 && idx < len(appearanceOptions) {
				s.changeThemeMode(appearanceOptions[idx].value)
			}

			ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
			s.accentColorRow(c)
		})
}

func (s *viewState) appearanceOptionTile(c *ui.Context, item ui.Element, opt appearanceOption, active bool) {
	t := tokens(c)
	optLabel := tr(c, opt.labelKey)
	bg := t.Color.Sunken
	border := t.Color.BorderSubtle
	if c.Preferences().HighContrast {
		border = t.Color.BorderDefault
	}
	if active {
		bg = t.Color.Surface
		if c.Theme().Dark {
			bg = t.Color.Elevated
		}
		border = t.Color.Accent
	} else if item.Hovered() {
		bg = t.Color.SurfaceHover
		border = t.Color.BorderStrong
	}

	item.MinWidth(0).Padding(t.Density.Row).Radius(t.Radius.Floating).
		Background(bg).Border(1.5, border).Label(optLabel)
	if active {
		applyShadow(item, t.Elevation.XS)
	}

	iconColor := t.Color.FgSecondary
	if active {
		iconColor = t.Color.FgPrimary
	}

	item.Children(func() {
		ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
			appearancePreviewThumb(c, opt.value, t.Color.BorderDefault)
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Padding(0, 2).Children(func() {
				components.Icon(c, opt.icon).Size(14, 14).TextColor(iconColor)
				label := ui.Text(c, optLabel).SingleLine()
				if active {
					label.Bold().TextColor(t.Color.FgPrimary)
				} else {
					label.TextColor(t.Color.FgSecondary)
				}
				ui.Spacer(c)
				checkCircle := ui.Box(c).Size(16, 16).Radius(t.Radius.Full).
					AlignItems(ui.Center).Justify(ui.Center)
				if active {
					checkCircle.Background(t.Color.Accent).Children(func() {
						components.Icon(c, lucide.Check).Size(10, 10).TextColor(t.Color.FgOnAccent)
					})
				} else {
					checkCircle.Border(1.5, t.Color.BorderStrong)
				}
			})
		})
	})
}

func appearancePreviewThumb(c *ui.Context, mode string, frameBorder ui.Color) {
	ui.Box(c).FillWidth().Height(72).Radius(6).Draw(func(p *ui.Painter, r ui.Rect) {
		drawThemePreview(p, r, mode, frameBorder)
	})
}

func drawThemePreview(p *ui.Painter, r ui.Rect, mode string, frameBorder ui.Color) {
	if r.W <= 2 || r.H <= 2 {
		return
	}
	const thumbRadius float32 = 6
	// Inset the clipped mockup inside the 1px frame stroke so bright fill edges
	// do not bleed through anti-aliased rounded corners on contrasting surfaces.
	inner := ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}
	innerRadius := max(1, thumbRadius-1)

	lightPal := paletteForAccent(activeAccent(), false)
	darkPal := paletteForAccent(activeAccent(), true)
	switch normalizeThemeMode(mode) {
	case core.ThemeLight:
		p.Clip(inner, innerRadius, func() {
			drawStudioMockup(p, r, lightPal)
		})
		p.Stroke(r, lightPal.border, thumbRadius, 1)
	case core.ThemeDark:
		p.Clip(inner, innerRadius, func() {
			drawStudioMockup(p, r, darkPal)
		})
		p.Stroke(r, darkPal.border, thumbRadius, 1)
	default:
		mid := r.W / 2
		leftClip := ui.Rect{X: inner.X, Y: inner.Y, W: mid - 1, H: inner.H}
		rightClip := ui.Rect{X: r.X + mid, Y: inner.Y, W: r.W - mid - 1, H: inner.H}
		p.Clip(inner, innerRadius, func() {
			p.Clip(leftClip, 0, func() {
				drawStudioMockup(p, r, lightPal)
			})
			p.Clip(rightClip, 0, func() {
				drawStudioMockup(p, r, darkPal)
			})
			p.Fill(ui.Rect{X: r.X + mid - 0.5, Y: inner.Y, W: 1, H: inner.H}, frameBorder, 0)
		})
		p.Stroke(r, frameBorder, thumbRadius, 1)
	}
}

func drawStudioMockup(p *ui.Painter, r ui.Rect, pal paletteColors) {
	p.Fill(r, pal.background, 0)

	sideW := max(28, r.W*0.24)
	p.Fill(ui.Rect{X: r.X, Y: r.Y, W: sideW, H: r.H}, pal.sunken, 0)
	p.Fill(ui.Rect{X: r.X + sideW, Y: r.Y, W: 1, H: r.H}, pal.border, 0)

	for i := range 3 {
		p.Fill(ui.Rect{X: r.X + 7 + float32(i)*6, Y: r.Y + 7, W: 3.5, H: 3.5}, pal.borderStrong, 1.75)
	}

	navY := r.Y + 17
	p.Fill(ui.Rect{X: r.X + 6, Y: navY, W: sideW - 12, H: 8}, pal.accent, 3)
	p.Fill(ui.Rect{X: r.X + 6, Y: navY + 13, W: (sideW - 14) * 0.85, H: 4}, pal.borderStrong, 2)
	p.Fill(ui.Rect{X: r.X + 6, Y: navY + 21, W: (sideW - 14) * 0.70, H: 4}, pal.borderStrong, 2)

	contentX := r.X + sideW + 1
	contentW := r.W - sideW - 1
	const pad float32 = 8
	cx := contentX + pad
	cw := contentW - 2*pad
	if cw <= 16 {
		return
	}

	p.Fill(ui.Rect{X: cx, Y: r.Y + 7, W: min(44, cw*0.32), H: 4.5}, pal.text.Alpha(.82), 2)

	cardY := r.Y + 16
	const cardH float32 = 24
	cardRect := ui.Rect{X: cx, Y: cardY, W: cw, H: cardH}
	p.Fill(cardRect, pal.surface, 5)
	p.Stroke(cardRect, pal.border, 5, 1)
	p.Fill(ui.Rect{X: cx + 7, Y: cardY + 6, W: cw * 0.42, H: 4}, pal.text.Alpha(.75), 2)
	p.Fill(ui.Rect{X: cx + 7, Y: cardY + 14, W: cw * 0.56, H: 3}, pal.muted.Alpha(.55), 1.5)
	p.Fill(ui.Rect{X: cx + cw - 24, Y: cardY + 7, W: 17, H: 10}, pal.accent, 4)

	kpiY := cardY + cardH + 5
	kpiH := r.Y + r.H - pad - kpiY
	kpiW := (cw - 6) / 2
	if kpiH < 12 || kpiW <= 10 {
		return
	}
	for i, dotColor := range [2]ui.Color{pal.info, pal.success} {
		kx := cx + float32(i)*(kpiW+6)
		kpiRect := ui.Rect{X: kx, Y: kpiY, W: kpiW, H: kpiH}
		p.Fill(kpiRect, pal.surface, 4)
		p.Stroke(kpiRect, pal.border, 4, 1)
		p.Fill(ui.Rect{X: kx + 6, Y: kpiY + 5, W: 4, H: 4}, dotColor, 2)
		p.Fill(ui.Rect{X: kx + 13, Y: kpiY + 5.5, W: max(8, kpiW*0.38), H: 3}, pal.muted.Alpha(.55), 1.5)
		p.Fill(ui.Rect{X: kx + 6, Y: kpiY + 12, W: max(12, kpiW*0.52), H: 4}, pal.text.Alpha(.75), 2)
	}
}

func (s *viewState) bumpSettingsEpoch() {
	s.settingsEpochMu.Lock()
	s.settingsEpoch++
	s.settingsEpochMu.Unlock()
}

func (s *viewState) currentThemeSeq() uint64 {
	s.themeMu.Lock()
	defer s.themeMu.Unlock()
	return s.themeWantedSeq
}

func (s *viewState) runOnUI(fn func()) {
	if s.updateWindow != nil {
		s.updateWindow(fn)
		return
	}
	if s.win != nil {
		s.win.Update(fn)
		return
	}
	fn()
}

func (s *viewState) changeThemeMode(mode string) {
	next := normalizeThemeMode(mode)
	if next == normalizeThemeMode(s.themeMode) {
		return
	}
	s.themeMode = next
	s.syncNativeThemeIfNeeded()
	raw, _ := json.Marshal(next)
	patch := map[string]json.RawMessage{"theme": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	update := s.persistTheme
	if update == nil && s.service != nil {
		update = s.service.UpdateSettings
	}
	if update == nil {
		return
	}
	s.themeSaving = true
	s.bumpSettingsEpoch()

	s.themeMu.Lock()
	s.themeWantedSeq++
	s.themeWanted = next
	startWorker := !s.themeWorkerRunning
	s.themeWorkerRunning = true
	s.themeMu.Unlock()

	if startWorker {
		go s.runThemePersistWorker(update)
	}
}

func (s *viewState) runThemePersistWorker(update func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)) {
	var lastSaved *core.SettingsResponse
	for {
		s.themeMu.Lock()
		mode := s.themeWanted
		seq := s.themeWantedSeq
		s.themeMu.Unlock()

		if s.ctx != nil && s.ctx.Err() != nil {
			s.themeMu.Lock()
			s.themeWorkerRunning = false
			s.themeMu.Unlock()
			return
		}

		raw, _ := json.Marshal(mode)
		value, apiErr := update(map[string]json.RawMessage{"theme": raw})
		if apiErr == nil {
			v := value
			lastSaved = &v
		}

		s.themeMu.Lock()
		latest := seq == s.themeWantedSeq
		if latest {
			s.themeWorkerRunning = false
		}
		s.themeMu.Unlock()

		if latest {
			s.runOnUI(func() { s.finishThemePersist(seq, lastSaved, value, apiErr) })
			return
		}
	}
}

func (s *viewState) finishThemePersist(seq uint64, lastSaved *core.SettingsResponse, value core.SettingsResponse, apiErr *core.ApiError) {
	if lastSaved != nil {
		s.settings.Settings.Theme = lastSaved.Settings.Theme
		s.settings.Effective.Theme = themeFromSettings(*lastSaved)
		s.bumpSettingsEpoch()
	}
	if seq != s.currentThemeSeq() {
		return
	}
	s.themeSaving = false
	if apiErr != nil {
		s.themeMode = themeFromSettings(s.settings)
		s.syncNativeThemeIfNeeded()
		s.failure = apiErr
		s.failurePage = s.selected
		return
	}
	s.themeMode = themeFromSettings(value)
	s.syncNativeThemeIfNeeded()
	if s.failurePage == "settings" {
		s.failure = nil
		s.failurePage = ""
	}
}
