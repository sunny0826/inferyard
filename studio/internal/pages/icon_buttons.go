package pages

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/icons/reicon"
)

// outlineIconButton renders a bordered icon-only button with an accessible label and hover tooltip.
func outlineIconButton(c *ui.Context, icon func() *ui.SVG, label string) ui.Element {
	t := tokens(c)
	b := iconButton(c, icon, label, components.IconButtonPlain).
		Size(t.Control.MD, t.Control.MD).
		Border(1, t.Color.BorderDefault).
		Background(t.Color.Surface).
		TextColor(t.Color.FgPrimary)
	if !b.IsDisabled() {
		switch {
		case b.Pressed():
			b.Background(t.Color.SurfacePressed)
		case b.Hovered():
			b.Background(t.Color.SurfaceHover)
		}
	}
	return b
}

// primaryIconButton renders an accent-tinted icon-only button with an accessible label and hover tooltip.
func primaryIconButton(c *ui.Context, icon func() *ui.SVG, label string) ui.Element {
	t := tokens(c)
	accent := t.Color.Accent
	b := iconButton(c, icon, label, components.IconButtonPlain).
		Size(t.Control.MD, t.Control.MD).
		Background(accent).
		TextColor(c.Theme().Surface).
		Border(1, accent)
	if !b.IsDisabled() {
		switch {
		case b.Pressed():
			b.Background(accent.Mix(c.Theme().Text, .18))
		case b.Hovered():
			b.Background(accent.Mix(c.Theme().Surface, .12))
		}
	}
	return b
}

// dangerIconButton renders a danger-tinted icon-only button with an accessible label and hover tooltip.
func dangerIconButton(c *ui.Context, icon func() *ui.SVG, label string) ui.Element {
	theme := c.Theme()
	t := tokens(c)
	danger := t.State.Danger
	background := t.Color.Surface
	border := t.Color.BorderDefault
	if c.Preferences().HighContrast {
		border = danger
	}
	b := iconButton(c, icon, label, components.IconButtonPlain).
		Size(t.Control.MD, t.Control.MD).
		TextColor(danger.Mix(theme.Text, .18))
	if !b.IsDisabled() {
		switch {
		case b.Pressed():
			background = theme.Surface.Mix(danger, .22)
			border = danger.Alpha(.72)
		case b.Hovered():
			background = theme.Surface.Mix(danger, .12)
			border = danger.Alpha(.55)
		}
	}
	return b.Background(background).Border(1, border)
}

// refreshIconButton renders an icon-only refresh button with spinning RefreshCw feedback,
// accessible label, and hover tooltip.
func refreshIconButton(c *ui.Context, label string, active bool) ui.Element {
	t := tokens(c)
	b := ui.ButtonBase(c).
		Size(t.Control.MD, t.Control.MD).
		Radius(t.Radius.Control).
		AlignSelf(ui.Start).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Border(1, t.Color.BorderDefault).
		Background(t.Color.Surface).
		TextColor(t.Color.FgPrimary).
		Label(label)
	if !b.IsDisabled() {
		switch {
		case b.Pressed():
			b.Background(t.Color.SurfacePressed)
		case b.Hovered():
			b.Background(t.Color.SurfaceHover)
		}
	}
	state := ui.Local(b, "refresh-icon-spin", func() spinIconState { return spinIconState{} })
	now := c.Now()
	if active {
		if !state.spinning {
			state.spinning = true
			state.startedAt = now
			state.angle = 0
		}
		state.stopAt = 0
	} else if state.spinning && state.stopAt == 0 {
		turns := math.Max(1, math.Ceil(state.angle/360))
		state.stopAt = turns * 360
	}
	angle := float32(0)
	if state.spinning {
		if c.Preferences().ReduceMotion {
			if !active {
				state.spinning = false
				state.angle = 0
			}
		} else {
			elapsed := now.Sub(state.startedAt).Seconds()
			deg := elapsed * 600
			if elapsed < 0.64 && state.stopAt == 360 {
				p := elapsed / 0.64
				deg = float64(ui.EaseInOut(float32(p))) * 360
			}
			state.angle = deg
			if state.stopAt > 0 && state.angle >= state.stopAt {
				state.spinning = false
				state.angle = 0
				state.stopAt = 0
			} else {
				angle = float32(math.Mod(state.angle, 360))
				c.AnimationFrame()
			}
		}
	}
	b.Children(func() {
		icon := components.Icon(c, reicon.RefreshCw).Size(15, 15).TextColor(t.Color.FgPrimary)
		if angle != 0 {
			icon.Rotate(angle)
		}
	})
	studioTooltip(c, b, label)
	return b
}

// iconButtonWithTooltip renders an icon button and attaches a custom tooltip (including when disabled).
func iconButtonWithTooltip(c *ui.Context, tip string, render func() ui.Element) ui.Element {
	return withDisabledTooltip(c, tip, render)
}
