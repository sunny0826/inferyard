package pages

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/l10n"
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

	t := tokens(c)
	reduced := c.Preferences().ReduceMotion
	topInset := sidebar.HeaderHeight(c) + 12
	width, _ := c.Size()
	cardWidth := min(float32(344), max(240, width-32))

	if len(screen) == 0 {
		nc.tappedExpanded = false
		nc.screenPaused = false
		ui.Overlay(c, func() {
			ui.Box(c).Fill().PassThrough().AlignItems(ui.End).Justify(ui.Start).Padding(topInset, 16, 16, 16).Children(func() {
				ui.Box(c.Key("studio-notif-stack")).Width(cardWidth).Height(0).PassThrough()
			})
		})
		return
	}

	anim := ui.Animation{Duration: 380 * time.Millisecond, Ease: notifStackEase}
	if reduced {
		anim = ui.Animation{Duration: 150 * time.Millisecond, Ease: ui.EaseOut}
	}
	now := c.Now()
	if now.IsZero() {
		now = time.Now()
	}

	ui.Overlay(c, func() {
		ui.Box(c).Fill().PassThrough().AlignItems(ui.End).Justify(ui.Start).Padding(topInset, 16, 16, 16).Children(func() {
			stack := ui.Box(c.Key("studio-notif-stack")).Width(cardWidth)
			paused := stack.Hovered() || stack.FocusWithin()
			if nextWake := nc.advanceScreenTimers(now, maxVisibleScreenNotifications, paused); nextWake > 0 {
				c.After(nextWake)
			}
			screen = nc.screenItems()
			if len(screen) == 0 {
				nc.tappedExpanded = false
				stack.Height(0).PassThrough()
				return
			}
			visible := screen
			if len(visible) > maxVisibleScreenNotifications {
				visible = visible[:maxVisibleScreenNotifications]
			}
			n := len(visible)
			if n <= 1 {
				nc.tappedExpanded = false
			}
			expanded := paused || nc.tappedExpanded
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
				if expanded && n > 0 {
					clearRow := ui.Row(c.Key("notif-clear-all")).Absolute().Left(0).Right(0).Justify(ui.End)
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
					pose = ui.Pose{Y: peek * depth, Scale: 1 - 0.05*depth}
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
	enter := &ui.Motion{Y: -56, Blur: 4, Opacity: 0}
	exit := &ui.Motion{X: 48, Opacity: 0}
	enterDur := 560 * time.Millisecond
	exitDur := 260 * time.Millisecond
	if reduced {
		enter = &ui.Motion{Opacity: 0}
		exit = &ui.Motion{Opacity: 0}
		enterDur = 150 * time.Millisecond
		exitDur = 150 * time.Millisecond
	}

	root := ui.Box(c.Key("notif-card-"+item.ID)).Absolute().Top(0).Left(0).Right(0).
		Radius(t.Radius.Surface).Background(t.Color.Elevated).Border(1, t.Color.BorderDefault).Clip().
		Role(ui.RoleStatus).Label(item.Title).
		TransformOrigin(.5, 0).
		Transition(ui.ElementTransition{
			Duration:     enterDur,
			Ease:         notifStackEase,
			ExitDuration: exitDur,
			ExitEase:     ui.EaseIn,
			Height:       true,
			Shadows:      true,
			Enter:        enter,
			Exit:         exit,
		})

	if expanded {
		applyShadow(root, t.Elevation.XS)
	} else if depth == 0 {
		applyShadow(root, t.Elevation.LG)
	} else {
		root.NoShadow()
	}

	if root.Shortcut(0, ui.KeyEscape) {
		s.dismissNotificationFromScreen(item.ID)
	}

	root.Children(func() {
		body := ui.Column(c).FillWidth().Padding(t.Card.Padding).Gap(t.Density.Row)
		if canExpand && !expanded && depth == 0 {
			body.OnClick(func() {
				s.notifications().tappedExpanded = !s.notifications().tappedExpanded
			})
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
							components.Icon(c, lucide.X).Size(12, 12).TextColor(t.Color.FgSecondary).Role(ui.RoleNone)
						})
						components.Tooltip(c, closeBtn, tr(c, "notif.dismiss"))
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
	return components.Button(c, label, variant).Height(32).Padding(0, t.Density.Stack).Radius(t.Radius.Control).FontSize(13)
}

func (s *viewState) notificationBell(c *ui.Context, maxCanvasWidth float32) {
	t := tokens(c)
	theme := c.Theme()
	nc := s.notifications()
	unread := nc.unreadCount()
	label := tr(c, "notif.title")
	if unread > 0 {
		label = trf(c, "notif.bell_unread", "count", unread)
	}
	c.Key("notification-bell")
	ui.Column(c).Shrink(0).Children(func() {
		size := max(float32(32), theme.Rem(2))
		anchor := ui.ButtonBase(c).Height(size).Radius(t.Radius.Control).Label(label)
		if unread == 0 {
			anchor.Width(size).AlignItems(ui.Center).Justify(ui.Center)
		} else {
			anchor.MinWidth(size).Padding(0, t.Density.Row).AlignItems(ui.Center).Justify(ui.Center)
		}
		var face ui.Color
		resolved := false
		anchor.Draw(func(p *ui.Painter, r ui.Rect) {
			if !resolved {
				resolved = true
				switch {
				case anchor.Pressed():
					face = theme.Text.Alpha(0.14)
				case nc.bellOpen:
					face = theme.Text.Alpha(0.10)
				case anchor.Hovered():
					face = theme.Text.Alpha(0.08)
				}
			}
			if face.A > 0 {
				p.Fill(r, face, t.Radius.Control)
			}
		})
		anchor.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Justify(ui.Center).Gap(t.Density.Tight).Children(func() {
				iconColor := t.Color.FgSecondary
				if unread > 0 || nc.bellOpen || anchor.Hovered() {
					iconColor = t.Color.FgPrimary
				}
				components.Icon(c, lucide.Bell).Size(16, 16).TextColor(iconColor).Role(ui.RoleNone)
				if unread > 0 {
					countText := fmt.Sprintf("%d", unread)
					if unread > 99 {
						countText = "99+"
					}
					ui.Box(c).MinWidth(18).Height(18).Padding(0, t.Density.Tight).Radius(t.Radius.Full).
						Background(t.Color.Danger.Solid).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Text(c, countText).FontSize(11).Bold().FontFeatures("tnum").TextColor(t.Color.FgOnAccent).SingleLine()
					})
				}
			})
		})
		if anchor.Clicked() {
			nc.bellOpen = !nc.bellOpen
		}
		if !nc.bellOpen {
			components.Tooltip(c, anchor, label)
		}
		studioPopover(c, anchor, &nc.bellOpen, func(panel ui.Element) {
			popWidth := float32(344)
			if maxCanvasWidth > 0 {
				popWidth = min(popWidth, max(float32(200), maxCanvasWidth))
			}
			panel.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight).
				Margin(t.Density.Row, 0, 0, 0).
				Padding(0).Gap(0).
				Width(popWidth).MaxWidthPercent(100).Label(tr(c, "notif.list_label"))
			panel.Children(func() {
				s.notificationInboxPanel(c)
			})
		})
	})
}

func (s *viewState) notificationInboxPanel(c *ui.Context) {
	t := tokens(c)
	nc := s.notifications()
	inbox := nc.inboxItems()
	ui.Column(c).FillWidth().Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
			Padding(t.Density.Stack, t.Density.Section).Children(func() {
			ui.Text(c, tr(c, "notif.title")).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).
				Role(ui.RoleHeading).Level(3).Grow(1).SingleLine()
			if len(inbox) > 0 {
				if inlineButton(c, components.ButtonGhost, tr(c, "notif.mark_all_read")).Clicked() {
					s.markAllNotificationsRead()
				}
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		if len(inbox) == 0 {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Justify(ui.Center).
				Padding(t.Density.Page, t.Density.Section).Gap(t.Density.Row).Children(func() {
				ui.Box(c).Size(36, 36).Radius(t.Radius.Full).Background(t.Color.Sunken).
					AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					components.Icon(c, lucide.Bell).Size(16, 16).TextColor(t.Color.FgTertiary).Role(ui.RoleNone)
				})
				ui.Text(c, tr(c, "notif.empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			return
		}
		ui.Scroll(c.Key("notif-inbox-scroll")).FillWidth().MaxHeight(380).Children(func() {
			inboxCardRadius := concentricRadius(t.Radius.Floating, t.Density.Tight)
			ui.Column(c).FillWidth().Padding(t.Density.Row).Gap(t.Density.Row).Children(func() {
				for _, item := range inbox {
					item := item
					cardBox := ui.Box(c.Key("inbox-"+item.ID)).FillWidth().Padding(t.Density.Stack).Radius(inboxCardRadius).
						Background(t.Color.Surface).Border(1, t.Color.BorderSubtle)
					cardBox.Children(func() {
						s.notificationCardInner(c, item, false)
					})
				}
			})
		})
	})
}

func relativeNotificationTime(at time.Time) string {
	return relativeNotificationTimeFor("", at)
}

func relativeNotificationTimeFor(locale string, at time.Time) string {
	if at.IsZero() {
		return l10n.T(locale, "notif.time.just_now")
	}
	diff := time.Since(at)
	switch {
	case diff < time.Minute:
		return l10n.T(locale, "notif.time.just_now")
	case diff < time.Hour:
		return l10n.Tf(locale, "notif.time.minutes_ago", "count", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return l10n.Tf(locale, "notif.time.hours_ago", "count", int(diff.Hours()))
	default:
		return at.Format("01-02 15:04")
	}
}
