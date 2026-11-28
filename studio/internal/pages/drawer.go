package pages

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/icons/reicon"
)

type drawerOptions struct {
	Label          string
	Title          string
	Width          float32
	GuardClose     bool
	SuspendDismiss bool
	OnCloseAttempt func()
}

var (
	drawerFade = ui.ElementTransition{
		Duration:     180 * time.Millisecond,
		ExitDuration: 110 * time.Millisecond,
		Colors:       true,
		Enter:        &ui.Motion{Opacity: 0},
		Exit:         &ui.Motion{Opacity: 0},
	}
	drawerSlide = ui.ElementTransition{
		Duration:     160 * time.Millisecond,
		ExitDuration: 110 * time.Millisecond,
		Enter:        &ui.Motion{Opacity: 1, X: 28},
		Exit:         &ui.Motion{Opacity: 1, X: 28},
	}
)

func drawer(c *ui.Context, open *bool, options drawerOptions, build func(panel ui.Element)) ui.Element {
	var panel ui.Element
	if !*open {
		ui.Overlay(c, func() {})
		return panel
	}
	t := tokens(c)
	w, _ := c.Size()
	panelWidth := options.Width
	if panelWidth <= 0 {
		panelWidth = c.Theme().Space(130)
	}
	if w > 0 {
		panelWidth = min(panelWidth, max(320, w-48))
	}
	topPad := max(t.Card.Padding, sidebar.HeaderHeight(c)+t.Density.Row)
	label := options.Label
	if label == "" {
		label = options.Title
	}
	requestClose := func() {
		if options.GuardClose && options.OnCloseAttempt != nil {
			options.OnCloseAttempt()
			return
		}
		*open = false
	}

	reduceMotion := c.Preferences().ReduceMotion
	ui.Overlay(c, func() {
		ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(t.Color.Backdrop).
			Transition(drawerFade)
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.End).Modal()
		clicked := back.Clicked()
		escape := back.OverlayShortcut(0, ui.KeyEscape)
		if !options.SuspendDismiss && (clicked || escape) {
			requestClose()
		}
		back.Children(func() {
			panel = applyShadow(
				ui.Column(c).Role(ui.RoleDialog).Label(label).
					Width(panelWidth).MaxWidthPercent(92).FillHeight().Shrink(0).
					Padding(topPad, t.Card.Padding, t.Card.Padding, t.Card.Padding).
					Gap(t.Density.CardGap).
					Background(t.Color.Elevated).
					Border(1, t.Color.BorderDefault).
					TextColor(t.Color.FgPrimary),
				t.Elevation.LG,
			)
			if reduceMotion {
				panel.Transition(drawerFade)
			} else {
				panel.Transition(drawerSlide)
			}
			panel.Clicked()
			panel.Children(func() {
				if options.Title != "" {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						heading(c, options.Title, 2).Grow(1).MinWidth(0)
						closeBtn := components.Button(c, "", components.ButtonGhost).
							Size(28, 28).Shrink(0).Padding(0).Radius(t.Radius.Control).
							Label(trf(c, "common.drawer.close_title", "title", options.Title))
						closeBtn.Children(func() {
							components.Icon(c, reicon.X).Size(16, 16).TextColor(t.Color.FgSecondary).Role(ui.RoleNone)
						})
						studioTooltip(c, closeBtn, tr(c, "common.close"))
						if closeBtn.Clicked() {
							requestClose()
						}
					})
				}
				if build != nil {
					build(panel)
				}
			})
		})
	})
	return panel
}
