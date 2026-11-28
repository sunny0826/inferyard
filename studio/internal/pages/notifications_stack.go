package pages

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/icons/reicon"
)

var notifStackEase = ui.CubicBezier(0.32, 0.72, 0, 1)

func (s *viewState) announceNotifications(c *ui.Context) {
	nc := s.notifications()
	for _, item := range nc.items {
		if !item.Announced {
			item.Announced = true
			text := item.Title
			if item.Body != "" {
				text = trf(c, "notif.announce_with_body", "title", item.Title, "body", item.Body)
			}
			c.Announce(text)
		}
	}
}

func (s *viewState) studioNotifications(c *ui.Context) {
	s.announceNotifications(c)
	nc := s.notifications()
	screen := nc.screenItems()
	coord := syncHoverCoordinator(c)

	t := tokens(c)
	reduced := c.Preferences().ReduceMotion
	topInset := sidebar.HeaderHeight(c) + 12
	width, _ := c.Size()
	cardWidth := min(float32(344), max(240, width-32))

	if len(screen) == 0 {
		nc.resetStackExpansion()
		nc.screenPaused = false
		ui.Overlay(c, func() {
			ui.Box(c.Key("studio-notif-overlay")).Absolute().Left(0).Top(0).Right(0).Bottom(0).PassThrough().
				AlignItems(ui.End).Justify(ui.Start).Padding(topInset, 16, 16, 16).Children(func() {
				ui.Box(c.Key("studio-notif-stack")).Width(cardWidth).Height(0).PassThrough()
			})
		})
		return
	}

	now := frameNow(c)

	ui.Overlay(c, func() {
		ui.Box(c.Key("studio-notif-overlay")).Absolute().Left(0).Top(0).Right(0).Bottom(0).PassThrough().
			AlignItems(ui.End).Justify(ui.Start).Padding(topInset, 16, 16, 16).Children(func() {
			stack := ui.Box(c.Key("studio-notif-stack")).Width(cardWidth)
			stackHovered := stack.Hovered()
			stackFocused := stack.FocusWithin()
			if !stackHovered {
				nc.hoverSuppressed = false
				nc.hoverEnteredAt = time.Time{}
			} else if !nc.wasStackHovered {
				nc.hoverEnteredAt = now
			}
			if !stackFocused {
				nc.focusEnteredAt = time.Time{}
			} else if !nc.wasStackFocused {
				nc.focusEnteredAt = now
			}
			nc.wasStackHovered = stackHovered
			nc.wasStackFocused = stackFocused

			visible := screen
			if len(visible) > maxVisibleScreenNotifications {
				visible = visible[:maxVisibleScreenNotifications]
			}
			if len(visible) <= 1 {
				nc.tappedExpanded = false
				nc.hoverExpanded = false
				nc.engagedInExpanded = false
				nc.graceStartedAt = time.Time{}
			} else {
				if (nc.hoverExpanded || nc.tappedExpanded) && (stack.OverlayShortcut(0, ui.KeyEscape) || stack.PressedOutside()) {
					nc.hoverExpanded = false
					nc.tappedExpanded = false
					nc.engagedInExpanded = false
					nc.graceStartedAt = time.Time{}
					nc.hoverSuppressed = true
				}
				if !(nc.hoverExpanded || nc.tappedExpanded) {
					if stackHovered && !nc.hoverSuppressed {
						if wait := cardOpenDelay - now.Sub(nc.hoverEnteredAt); wait > 0 {
							c.After(wait)
						} else if !coord.isFast(c, now) {
							nc.hoverExpanded = true
						}
					}
					if !(nc.hoverExpanded || nc.tappedExpanded) && stackFocused && !nc.hoverSuppressed {
						if wait := cardOpenDelay - now.Sub(nc.focusEnteredAt); wait > 0 {
							c.After(wait)
						} else {
							nc.hoverExpanded = true
						}
					}
				}
				if nc.hoverExpanded || nc.tappedExpanded {
					if stackHovered || stackFocused {
						nc.engagedInExpanded = true
						nc.graceStartedAt = time.Time{}
					} else if nc.engagedInExpanded {
						if nc.graceStartedAt.IsZero() {
							nc.graceStartedAt = now
						}
						if wait := cardCloseGrace - now.Sub(nc.graceStartedAt); wait > 0 {
							c.After(wait)
						} else {
							nc.hoverExpanded = false
							nc.tappedExpanded = false
							nc.engagedInExpanded = false
							nc.graceStartedAt = time.Time{}
						}
					}
				}
			}

			expanded := len(visible) > 1 && (nc.hoverExpanded || nc.tappedExpanded)
			paused := stackHovered || stackFocused || expanded
			if nextWake := nc.advanceScreenTimers(now, maxVisibleScreenNotifications, paused); nextWake > 0 {
				c.After(nextWake)
			}
			screen = nc.screenItems()
			if len(screen) == 0 {
				nc.resetStackExpansion()
				stack.Height(0).PassThrough()
				return
			}
			visible = screen
			if len(visible) > maxVisibleScreenNotifications {
				visible = visible[:maxVisibleScreenNotifications]
			}
			n := len(visible)
			if n <= 1 {
				expanded = false
				nc.tappedExpanded = false
				nc.hoverExpanded = false
				nc.engagedInExpanded = false
				nc.graceStartedAt = time.Time{}
			}
			heights := make([]float32, n)
			roots := make([]ui.Element, n)

			stack.Children(func() {
				// Render oldest first so newest (index 0) sits in front.
				for i := n - 1; i >= 0; i-- {
					item := visible[i]
					roots[i] = s.notificationStackCard(c, item, i, n > 1, expanded, reduced, func(h float32) {
						heights[i] = h
					})
				}
				if n > 1 && (nc.hoverExpanded || nc.tappedExpanded) {
					expanded = true
				}
				if expanded && n > 0 {
					clearMotion := &ui.Motion{Opacity: 0, Y: -4}
					if reduced {
						clearMotion = &ui.Motion{Opacity: 0}
					}
					clearRow := ui.Row(c.Key("notif-clear-all")).Absolute().Left(0).Right(0).Justify(ui.End).
						Transition(ui.ElementTransition{
							Duration:     cardEnterDuration,
							ExitDuration: cardExitDuration,
							Colors:       reduced,
							Enter:        clearMotion,
							Exit:         clearMotion,
						})
					clearRow.Children(func() {
						btn := components.Button(c, tr(c, "notif.clear_all"), components.ButtonGhost).
							Padding(t.Density.Tight, t.Density.Stack).Radius(t.Radius.Control).FontSize(t.Type.Caption)
						if btn.Clicked() {
							s.clearScreenNotifications()
						}
					})
					roots = append(roots, clearRow)
				}
			})

			anim := ui.Animation{Duration: cardExitDuration, Ease: ui.EaseOut}
			if expanded {
				anim = ui.Animation{Duration: cardEnterDuration, Ease: notifStackEase}
			}
			if reduced {
				anim.Ease = ui.EaseOut
			}
			front := heights[0]
			if front <= 0 {
				front = 104
			}
			const peek = float32(12)
			const listGap = float32(8)
			var offset float32
			for i := 0; i < n; i++ {
				depth := float32(i)
				h := heights[i]
				if h <= 0 {
					h = front
				}
				pose := ui.Pose{Y: offset}
				offset += h + listGap
				if depth > 0 && !expanded {
					scale := 1 - 0.05*depth
					if reduced {
						scale = 1
					}
					pose = ui.Pose{Y: peek * depth, Scale: scale}
					if front > 0 {
						roots[i].Height(front)
					}
				}
				roots[i].AnimatePose(pose, anim)
			}
			stackHeight := front + peek*float32(max(0, n-1))
			if expanded {
				clearOffset := max(0, offset)
				if len(roots) > n {
					roots[n].AnimatePose(ui.Pose{Y: clearOffset}, anim)
				}
				stackHeight = clearOffset + 32
			}
			stack.Height(stackHeight)
		})
	})
}

func (s *viewState) notificationStackCard(c *ui.Context, item *studioNotification, depth int, canExpand, expanded, reduced bool, recordHeight func(float32)) ui.Element {
	t := tokens(c)
	cardTr := ui.ElementTransition{
		Duration:     560 * time.Millisecond,
		Ease:         notifStackEase,
		ExitDuration: 260 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Height:       true,
		Shadows:      true,
		Enter:        &ui.Motion{Y: -56, Blur: 4, Opacity: 0},
		Exit:         &ui.Motion{X: 48, Opacity: 0},
	}
	if reduced {
		cardTr = ui.ElementTransition{
			Duration:     cardEnterDuration,
			ExitDuration: cardExitDuration,
			Colors:       true,
			Enter:        &ui.Motion{Opacity: 0},
			Exit:         &ui.Motion{Opacity: 0},
		}
	}

	root := ui.Box(c.Key("notif-card-"+item.ID)).Absolute().Top(0).Left(0).Right(0).
		Radius(t.Radius.Surface).Background(t.Color.Elevated).Border(1, t.Color.BorderDefault).Clip().
		Role(ui.RoleStatus).Label(item.Title).
		TransformOrigin(.5, 0).
		Transition(cardTr)

	if expanded {
		applyShadow(root, t.Elevation.XS)
	} else if depth == 0 {
		applyShadow(root, t.Elevation.LG)
	} else {
		root.NoShadow()
	}

	if !expanded && root.Shortcut(0, ui.KeyEscape) {
		s.dismissNotificationFromScreen(item.ID)
	}

	root.Children(func() {
		body := ui.Column(c).FillWidth().Padding(t.Card.Padding).Gap(t.Density.Row)
		if canExpand && !expanded && depth == 0 {
			body.Focusable()
			if body.Clicked() || body.Shortcut(0, ui.KeyEnter) {
				nc := s.notifications()
				nc.tappedExpanded = true
				nc.engagedInExpanded = true
				nc.hoverSuppressed = false
				nc.graceStartedAt = time.Time{}
				c.Invalidate()
			}
		}
		body.Children(func() {
			s.notificationCardInner(c, item, true)
		})
		if h := body.LayoutBounds().H; h > 0 {
			recordHeight(h)
		}
	})
	return root
}

func (s *viewState) notificationCardInner(c *ui.Context, item *studioNotification, onScreen bool) {
	t := tokens(c)
	scale := stateScale(c, item.Kind)
	innerRadius := concentricRadius(t.Radius.Surface, t.Card.Padding)
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Density.Row).Children(func() {
		ui.Box(c).Size(32, 32).Radius(innerRadius).Shrink(0).
			Background(scale.SubtleBg).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			components.Icon(c, defaultStateIcon(item.Kind)).Size(16, 16).TextColor(scale.Fg).Role(ui.RoleNone)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, item.Title).Bold().FontSize(14).FixedLineHeight(20).TextColor(t.Color.FgPrimary).Grow(1).MinWidth(0).SingleLine()
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Shrink(0).Children(func() {
					ui.Text(c, relativeNotificationTimeFor(contextLocale(c), item.CreatedAt)).FontSize(12).TextColor(t.Color.FgTertiary).SingleLine().Shrink(0)
					if onScreen {
						closeBtn := components.Button(c, "", components.ButtonGhost).
							Size(24, 24).Padding(0).Radius(t.Radius.Full).Shrink(0).
							Label(trf(c, "notif.dismiss_title", "title", item.Title))
						closeBtn.Children(func() {
							components.Icon(c, reicon.X).Size(12, 12).TextColor(t.Color.FgSecondary).Role(ui.RoleNone)
						})
						studioTooltip(c, closeBtn, tr(c, "notif.dismiss"))
						if closeBtn.Clicked() {
							s.dismissNotificationFromScreen(item.ID)
						}
					}
				})
			})
			if item.Body != "" {
				ui.Text(c, item.Body).FontSize(13).FixedLineHeight(20).TextColor(t.Color.FgSecondary).MaxLines(3)
			}
			if item.Secondary != nil || item.Primary != nil {
				ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Tight, 0, 0, 0).Children(func() {
					if item.Secondary != nil {
						if notificationCardButton(c, item.Secondary.Label, false).Clicked() {
							fn := item.Secondary.OnClick
							s.handleNotification(item.ID)
							if fn != nil {
								fn()
							}
						}
					}
					if item.Primary != nil {
						if notificationCardButton(c, item.Primary.Label, true).Clicked() {
							fn := item.Primary.OnClick
							s.handleNotification(item.ID)
							if !onScreen {
								s.notifications().bellOpen = false
							}
							if fn != nil {
								fn()
							}
						}
					}
				})
			}
		})
	})
}

func notificationCardButton(c *ui.Context, label string, primary bool) ui.Element {
	t := tokens(c)
	variant := components.ButtonSecondary
	if primary {
		variant = components.ButtonPrimary
	}
	b := components.Button(c, label, variant).Height(32).Padding(0, t.Density.Stack).Radius(t.Radius.Control).FontSize(13)
	if !primary {
		applySecondaryButtonPalette(c, b)
	}
	return b
}
