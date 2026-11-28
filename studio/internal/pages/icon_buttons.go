package pages

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/icons/reicon"
)

// studioSquareIconButton builds a Control.MD square icon button with deferred hover/press
// face resolution so chained .Disabled(true) calls never trigger hover tinting.
func studioSquareIconButton(
	c *ui.Context,
	icon func() *ui.SVG,
	label string,
	bg, hoverBg, pressedBg, border, hoverBorder, pressedBorder, fg ui.Color,
) ui.Element {
	t := tokens(c)
	rad := t.Radius.Control
	b := ui.ButtonBase(c).
		Size(t.Control.MD, t.Control.MD).
		Shrink(0).
		Radius(rad).
		AlignSelf(ui.Start).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Background(bg).
		Border(1, border).
		TextColor(fg)
	if label != "" {
		b.Label(label)
		studioTooltip(c, b, label)
	}
	applyDeferredButtonFace(b, rad, hoverBg, pressedBg, hoverBorder, pressedBorder)
	b.Children(func() {
		components.Icon(c, icon).Size(15, 15).TextColor(fg).Role(ui.RoleNone)
	})
	return b
}

// outlineIconButton renders a bordered icon-only button with an accessible label and hover tooltip.
func outlineIconButton(c *ui.Context, icon func() *ui.SVG, label string) ui.Element {
	t := tokens(c)
	return studioSquareIconButton(
		c, icon, label,
		t.Color.Surface, t.Color.SurfaceHover, t.Color.SurfacePressed,
		t.Color.BorderDefault, t.Color.BorderStrong, t.Color.BorderStrong,
		t.Color.FgPrimary,
	)
}

// primaryIconButton renders an accent-tinted icon-only button with an accessible label and hover tooltip.
func primaryIconButton(c *ui.Context, icon func() *ui.SVG, label string) ui.Element {
	t := tokens(c)
	return studioSquareIconButton(
		c, icon, label,
		t.Color.Accent, t.Color.AccentHover, t.Color.AccentPressed,
		t.Color.Accent, t.Color.AccentHover, t.Color.AccentPressed,
		t.Color.FgOnAccent,
	)
}

// dangerIconButton renders a danger-tinted icon-only button with an accessible label and hover tooltip.
func dangerIconButton(c *ui.Context, icon func() *ui.SVG, label string) ui.Element {
	theme := c.Theme()
	t := tokens(c)
	danger := t.State.Danger
	bp := paletteFor(theme.Dark).buttons
	border := t.Color.BorderDefault
	fg := danger.Mix(theme.Text, .18)
	hoverBg := theme.Surface.Mix(danger, .12)
	pressedBg := theme.Surface.Mix(danger, .22)
	hoverBorder := danger.Alpha(.55)
	pressedBorder := danger.Alpha(.72)
	if bp.dangerFg.A > 0 && !c.Preferences().HighContrast {
		fg = bp.dangerFg
		if bp.dangerBg.A > 0 {
			hoverBg = bp.dangerBg
		}
		if bp.dangerPressed.A > 0 {
			pressedBg = bp.dangerPressed
		}
		if bp.dangerBorder.A > 0 {
			hoverBorder = bp.dangerBorder
			pressedBorder = bp.dangerBorder
		}
	}
	if c.Preferences().HighContrast {
		border = danger
		hoverBorder = danger
		pressedBorder = danger
	}
	return studioSquareIconButton(
		c, icon, label,
		t.Color.Surface, hoverBg, pressedBg,
		border, hoverBorder, pressedBorder,
		fg,
	)
}

// refreshIconButton renders an icon-only refresh button with spinning RefreshCw feedback,
// accessible label, and hover tooltip.
func refreshIconButton(c *ui.Context, label string, active bool) ui.Element {
	t := tokens(c)
	rad := t.Radius.Control
	b := ui.ButtonBase(c).
		Size(t.Control.MD, t.Control.MD).
		Shrink(0).
		Radius(rad).
		AlignSelf(ui.Start).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Border(1, t.Color.BorderDefault).
		Background(t.Color.Surface).
		TextColor(t.Color.FgPrimary).
		Label(label)
	applyDeferredButtonFace(b, rad, t.Color.SurfaceHover, t.Color.SurfacePressed, t.Color.BorderStrong, t.Color.BorderStrong)
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
