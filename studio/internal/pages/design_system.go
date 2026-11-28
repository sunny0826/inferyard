package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
	"math"
)

func heading(c *ui.Context, title string, level int) ui.Element {
	scale := tokens(c).Type
	size := scale.Section
	if level == 1 {
		size = scale.PageTitle
	} else if level == 2 {
		size = scale.CardTitle
	}
	return ui.Text(c, title).Bold().FontSize(size).Role(ui.RoleHeading).Level(level)
}

func section(c *ui.Context, title string, fn func()) {
	ui.Column(c).Gap(tokens(c).Density.CardGap).Children(func() { heading(c, title, 3); fn() })
}

func configuredValue(value string) string {
	if value == "" {
		return l10n.TR("common.not_configured")
	}
	return value
}

func applyDeferredButtonFace(b ui.Element, rad float32, hoverBg, pressedBg, hoverBorder, pressedBorder ui.Color) {
	var faceBg, faceBorder ui.Color
	resolved := false
	b.Draw(func(p *ui.Painter, r ui.Rect) {
		if !resolved {
			resolved = true
			if b.Valid() && !b.IsDisabled() {
				switch {
				case b.Pressed():
					faceBg, faceBorder = pressedBg, pressedBorder
				case b.Hovered():
					faceBg, faceBorder = hoverBg, hoverBorder
				}
			}
		}
		if faceBg.A > 0 {
			p.Fill(r, faceBg, rad)
		}
		if faceBorder.A > 0 {
			inset := ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: max(0, r.W-1), H: max(0, r.H-1)}
			p.Stroke(inset, faceBorder, max(0, rad-0.5), 1)
		}
	})
}

func applySecondaryButtonPalette(c *ui.Context, b ui.Element) ui.Element {
	if c.Preferences().HighContrast {
		return b
	}
	t := tokens(c)
	bp := paletteFor(c.Theme().Dark).buttons
	if bp.secondaryBg.A == 0 {
		return b
	}
	hovBorder := bp.secondaryBorder
	if bp.secondaryBorderHover.A > 0 {
		hovBorder = bp.secondaryBorderHover
	}
	b.Background(bp.secondaryBg).TextColor(c.Theme().Text).Border(1, bp.secondaryBorder)
	applyDeferredButtonFace(b, t.Radius.Control, bp.secondaryHover, bp.secondaryPressed, hovBorder, hovBorder)
	if !b.IsDisabled() {
		applyShadow(b, t.Elevation.XS)
	}
	return b
}

type buttonIconSide int

const (
	buttonIconNone buttonIconSide = iota
	buttonIconLeading
	buttonIconTrailing
)

// applyButtonOpticalPadding preserves Control.SM/MD/LG vertical sizing while
// tightening horizontal padding on the icon side to compensate for SVG viewBox
// margin (detail.design: Visual Alignment for Buttons with Icon).
func applyButtonOpticalPadding(b ui.Element, t designTokens, size controlSize, side buttonIconSide) ui.Element {
	padY, padX, inset := t.Density.Tight, t.Density.Stack, float32(3)
	switch size {
	case sizeSM:
		padY, padX, inset = t.Density.Hairline, t.Density.Row, 2
	case sizeLG:
		padY, padX, inset = t.Density.Row, t.Density.Section, 3
	}
	padIcon := max(4, padX-inset)
	switch side {
	case buttonIconLeading:
		return b.Padding(padY, padX, padY, padIcon)
	case buttonIconTrailing:
		return b.Padding(padY, padIcon, padY, padX)
	default:
		return b.Padding(padY, padX)
	}
}

func iconLabelButton(c *ui.Context, variant components.ButtonVariant, side buttonIconSide, icon func() *ui.SVG, label string) ui.Element {
	t := tokens(c)
	b := styleButtonSize(c, components.Button(c, "", variant), sizeMD).Label(label)
	if variant == components.ButtonSecondary {
		applySecondaryButtonPalette(c, b)
	}
	applyButtonOpticalPadding(b, t, sizeMD, side)
	iconSlot := func() {
		ui.Box(c).Size(14, 14).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			components.Icon(c, icon).Size(14, 14).Role(ui.RoleNone)
		})
	}
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight + 2).Children(func() {
			if side == buttonIconLeading {
				iconSlot()
			}
			ui.Text(c, label).SingleLine()
			if side == buttonIconTrailing {
				iconSlot()
			}
		})
	})
	return b
}

func leadingIconButton(c *ui.Context, variant components.ButtonVariant, icon func() *ui.SVG, label string) ui.Element {
	return iconLabelButton(c, variant, buttonIconLeading, icon, label)
}

func trailingIconButton(c *ui.Context, variant components.ButtonVariant, label string, icon func() *ui.SVG) ui.Element {
	return iconLabelButton(c, variant, buttonIconTrailing, icon, label)
}

func button(c *ui.Context, label string) ui.Element {
	return applySecondaryButtonPalette(c, styleButtonSize(c, components.Button(c, label, components.ButtonSecondary), sizeMD))
}

func primaryButton(c *ui.Context, label string) ui.Element {
	return styleButtonSize(c, components.Button(c, label, components.ButtonPrimary), sizeMD)
}

func dangerButton(c *ui.Context, label string) ui.Element {
	theme := c.Theme()
	t := tokens(c)
	danger := t.State.Danger
	b := styleButtonSize(c, components.Button(c, label, components.ButtonOutline), sizeMD)
	if bp := paletteFor(theme.Dark).buttons; bp.dangerBg.A > 0 && !c.Preferences().HighContrast {
		b.Background(bp.dangerBg).TextColor(bp.dangerFg).Border(1, bp.dangerBorder)
		applyDeferredButtonFace(b, t.Radius.Control, bp.dangerHover, bp.dangerPressed, bp.dangerBorder, bp.dangerBorder)
		return b
	}
	baseBg := theme.Surface.Mix(danger, .10)
	hovBg := theme.Surface.Mix(danger, .18)
	prsBg := theme.Surface.Mix(danger, .28)
	bdr := danger.Alpha(.7)
	b.Background(baseBg).TextColor(danger.Mix(theme.Text, .2)).Border(1, bdr)
	applyDeferredButtonFace(b, t.Radius.Control, hovBg, prsBg, bdr, bdr)
	return b
}

func quietDangerButton(c *ui.Context, label string) ui.Element {
	theme := c.Theme()
	t := tokens(c)
	danger := t.State.Danger
	bp := paletteFor(theme.Dark).buttons
	b := styleButtonSize(c, components.Button(c, label, components.ButtonOutline), sizeMD)
	background := t.Color.Surface
	border := t.Color.BorderDefault
	fg := danger.Mix(theme.Text, .18)
	if bp.dangerFg.A > 0 && !c.Preferences().HighContrast {
		fg = bp.dangerFg
	}
	if c.Preferences().HighContrast {
		border = danger
	}
	hovBg := theme.Surface.Mix(danger, .10)
	prsBg := theme.Surface.Mix(danger, .20)
	hovBorder := danger.Alpha(.55)
	prsBorder := danger.Alpha(.70)
	if bp.dangerBg.A > 0 && !c.Preferences().HighContrast {
		hovBg = bp.dangerBg
		hovBorder = bp.dangerBorder
	}
	if bp.dangerPressed.A > 0 && !c.Preferences().HighContrast {
		prsBg = bp.dangerPressed
		prsBorder = bp.dangerBorder
	}
	b.Background(background).TextColor(fg).Border(1, border)
	applyDeferredButtonFace(b, t.Radius.Control, hovBg, prsBg, hovBorder, prsBorder)
	return b
}

func activitySpinner(c *ui.Context, label string) ui.Element {
	return components.Spinner(c, components.SpinnerRing).Label(label)
}

func compactLayout(c *ui.Context) bool {
	width, _ := c.Size()
	return width < 1050 || c.Preferences().TextScale > 1
}

func progressValue(done int64, total *int64) float64 {
	if total == nil || *total <= 0 || done < 0 || done > *total {
		return -1
	}
	return float64(done) / float64(*total)
}

func progressBar(c *ui.Context, value float64, label string) ui.Element {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		value = -1
	} else if value > 1 {
		value = -1
	}
	t := tokens(c)
	reduced := c.Preferences().ReduceMotion
	borderColor := t.Color.BorderSubtle
	trackColor := t.Color.Sunken
	fillColor := t.Color.Active.Solid
	if value >= 1 {
		fillColor = t.Color.Success.Solid
	}
	sheenColor := fillColor.Mix(ui.RGB(255, 255, 255), .24)
	e := ui.Box(c).Height(c.Theme().Space(2)).FillWidth().Role(ui.RoleProgress).Range(0, 1, value).Label(label)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 || r.H <= 0 {
			return
		}
		rad := r.H / 2
		if r.H >= 5 {
			p.Fill(r, borderColor, rad)
			inner := ui.Rect{X: r.X + 1, Y: r.Y + 1, W: max(0, r.W-2), H: max(0, r.H-2)}
			p.Fill(inner, trackColor, inner.H/2)
		} else {
			p.Fill(r, borderColor, rad)
		}
		x, w := r.X, r.W*float32(value)
		if value < 0 {
			phase := 0.25
			if !reduced {
				p.AnimationFrame()
				phase = float64(p.Now().UnixMilli()%1400) / 1400
			}
			w = r.W * 0.32
			x = r.X + (r.W-w)*float32((1-math.Cos(phase*2*math.Pi))/2)
		} else if value > 0 && value < 1 {
			w = min(r.W, max(r.H, w))
		}
		if w > 0 {
			p.Clip(r, rad, func() {
				fillRect := ui.Rect{X: x, Y: r.Y, W: w, H: r.H}
				p.Fill(fillRect, fillColor, rad)
				if !reduced && value > 0 && value < 1 && w >= 24 {
					p.AnimationFrame()
					phase := float32(p.Now().UnixMilli()%1800) / 1800
					bandW := max(18, w*0.35)
					bandX := x - bandW + (w+bandW*2)*phase
					p.Clip(fillRect, rad, func() {
						p.Fill(ui.Rect{X: bandX, Y: r.Y, W: bandW, H: r.H}, sheenColor, rad)
					})
				}
			})
		}
	})
	return e
}

func errorAnnouncement(e *core.ApiError) string {
	if e.Message != "" {
		return e.Message
	}
	return human(e.Code)
}

func (s *viewState) submissionFailed(e *core.ApiError) {
	s.failure = e
	s.failurePage = s.selected
	delete(s.announcedErrors, errorAnnouncement(e))
}

func (s *viewState) consumeFeedback(c *ui.Context) {
	s.flushToasts(c)
	s.announceChanges(c)
}

func (s *viewState) announceChanges(c *ui.Context) {
	cur := canonicalPageID(s.selected)
	page := cur
	if page == "benchmarks" && s.detailID != "" {
		page += "/" + s.detailID
	}
	if page != s.announcedPage {
		title := tr(c, "shell.page.run_detail")
		if s.detailID == "" || cur != "benchmarks" {
			if cur == "settings" {
				title = tr(c, "nav.settings")
			} else {
				for _, n := range navigation {
					if n.id == cur {
						title = tr(c, n.labelKey)
					}
				}
			}
		}
		c.Announce(title)
		s.announcedPage = page
	}
	errors := []string{}
	if s.failure == nil {
		errors = append(errors, s.refreshFailure)
	}
	add := func(e *core.ApiError) {
		if e != nil {
			errors = append(errors, errorAnnouncement(e))
		}
	}
	add(s.failure)
	if s.cliUpgradeErrorVisible() {
		add(s.cliUpgrade.Error)
	}
	switch cur {
	case "services":
		add(s.engineState.Error)
	case "library":
		if !s.suppressOutdatedCLIError(s.device.LastError) {
			add(s.device.LastError)
		}
		if _, err := parseDeviceSnapshot(s.device.Snapshot); err != nil {
			errors = append(errors, tr(c, "shell.announce.device_snapshot_unreadable"))
		}
		for _, d := range s.downloads {
			add(d.Error)
		}
	case "benchmarks":
		if s.detailID != "" {
			add(s.detailError)
			if s.detail != nil {
				add(s.detail.Task.Error)
			}
		} else {
			add(s.historyError)
			for _, task := range s.tasks {
				add(task.Error)
			}
		}
	case "reports":
		add(s.historyError)
		add(s.reportsError)
	}
	if s.progress != nil && (cur == "benchmarks" && s.detailID == "") {
		for _, bundle := range s.progress.Bundles {
			if bundle.BlockedReason != nil && bundle.Phase != "run_incomplete" {
				errors = append(errors, trf(c, "shell.announce.bundle_blocked", "bundle", bundle.Bundle, "reason", human(*bundle.BlockedReason)))
			}
		}
	}
	current := map[string]bool{}
	for _, message := range errors {
		if message == "" || current[message] {
			continue
		}
		current[message] = true
		if !s.announcedErrors[message] {
			c.Announce(message)
		}
	}
	s.announcedErrors = current
}

func runColor(c *ui.Context, task core.RunTask) ui.Color {
	if task.Queued && task.State == "starting" {
		return stateColor(c, stateNeutral)
	}
	if task.Exit != nil {
		switch task.Exit.ExitCode {
		case 0:
			return stateColor(c, stateSuccess)
		case 2, 3:
			return stateColor(c, stateWarning)
		case 4:
			return stateColor(c, stateDanger)
		case 130:
			return stateColor(c, stateNeutral)
		}
	}
	switch task.State {
	case "starting", "running", "cancelling":
		return stateColor(c, stateActive)
	case "failed":
		return stateColor(c, stateDanger)
	}
	return stateColor(c, stateNeutral)
}

func progressTotal(total *int64) string {
	if total == nil || *total <= 0 {
		return "—"
	}
	return fmt.Sprint(*total)
}
