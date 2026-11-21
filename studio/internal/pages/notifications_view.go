package pages

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
)

var notifStackEase = ui.CubicBezier(0.32, 0.72, 0, 1)

func (s *viewState) announceNotifications(c *ui.Context) {
	nc := s.notifications()
	for _, item := range nc.items {
		if !item.Announced {
			item.Announced = true
			text := item.Title
			if item.Body != "" {
				text += "。" + item.Body
			}
			c.Announce(text)
		}
	}
}

func (s *viewState) studioNotifications(c *ui.Context) {
	s.announceNotifications(c)
	nc := s.notifications()
	screen := nc.screenItems()
	if len(screen) == 0 {
		nc.tappedExpanded = false
		return
	}
	visible := screen
	if len(visible) > 3 {
		visible = visible[:3]
	}

	t := tokens(c)
	reduced := c.Preferences().ReduceMotion
	topInset := sidebar.HeaderHeight(c) + 12
	width, _ := c.Size()
	cardWidth := min(float32(344), max(240, width-32))

	anim := ui.Animation{Duration: 380 * time.Millisecond, Ease: notifStackEase}
	if reduced {
		anim = ui.Animation{Duration: 150 * time.Millisecond, Ease: ui.EaseOut}
	}

	ui.Overlay(c, func() {
		ui.Box(c).Fill().PassThrough().AlignItems(ui.End).Justify(ui.Start).Padding(topInset, 16, 16, 16).Children(func() {
			stack := ui.Box(c.Key("studio-notif-stack")).Width(cardWidth)
			expanded := stack.Hovered() || stack.FocusWithin() || nc.tappedExpanded
			n := len(visible)
			heights := make([]float32, n)
			roots := make([]ui.Element, n)

			stack.Children(func() {
				// Render oldest first so newest (index 0) sits in front.
				for i := n - 1; i >= 0; i-- {
					item := visible[i]
					roots[i] = s.notificationStackCard(c, item, i, expanded, reduced, func(h float32) {
						heights[i] = h
					})
				}
				if expanded && n > 0 {
					clearRow := ui.Row(c.Key("notif-clear-all")).Absolute().Left(0).Right(0).Justify(ui.End)
					clearRow.Children(func() {
						btn := components.Button(c, "全部清除", components.ButtonGhost).
							Padding(4, 10).Radius(10).FontSize(t.Type.Caption)
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

func (s *viewState) notificationStackCard(c *ui.Context, item *studioNotification, depth int, expanded, reduced bool, recordHeight func(float32)) ui.Element {
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
		Radius(24).Background(t.Color.Elevated).Border(1, t.Color.BorderDefault).Clip().
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

	showClose := root.Hovered() || root.FocusWithin()
	root.Children(func() {
		body := ui.Column(c).FillWidth().Padding(12).Gap(8)
		if !expanded && depth == 0 {
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
		if showClose {
			closeBtn := components.Button(c, "", components.ButtonGhost).
				Absolute().Top(6).Left(6).Size(24, 24).Padding(0).Radius(10).
				Background(t.Color.Surface).Border(1, t.Color.BorderDefault).
				Label("关闭通知：" + item.Title)
			closeBtn.Children(func() {
				components.Icon(c, lucide.X).Size(12, 12).TextColor(t.Color.FgSecondary).Role(ui.RoleNone)
			})
			components.Tooltip(c, closeBtn, "关闭通知")
			if closeBtn.Clicked() {
				s.dismissNotificationFromScreen(item.ID)
			}
		}
	})
	return root
}

func (s *viewState) notificationCardInner(c *ui.Context, item *studioNotification, onScreen bool) {
	t := tokens(c)
	scale := stateScale(c, item.Kind)
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(10).Children(func() {
		ui.Box(c).Size(32, 32).Radius(12).Shrink(0).
			Background(scale.SubtleBg).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			components.Icon(c, defaultStateIcon(item.Kind)).Size(16, 16).TextColor(scale.Fg).Role(ui.RoleNone)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, item.Title).Bold().FontSize(14).FixedLineHeight(20).TextColor(t.Color.FgPrimary).Grow(1).MinWidth(0).SingleLine()
				ui.Text(c, relativeNotificationTime(item.CreatedAt)).FontSize(12).TextColor(t.Color.FgTertiary).SingleLine().Shrink(0)
			})
			if item.Body != "" {
				ui.Text(c, item.Body).FontSize(13).FixedLineHeight(20).TextColor(t.Color.FgSecondary).MaxLines(3)
			}
			if item.Secondary != nil || item.Primary != nil {
				ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(6).Padding(4, 0, 0, 0).Children(func() {
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
	variant := components.ButtonSecondary
	if primary {
		variant = components.ButtonPrimary
	}
	return components.Button(c, label, variant).Height(32).Padding(0, 12).Radius(12).FontSize(13)
}

func (s *viewState) notificationBell(c *ui.Context) {
	t := tokens(c)
	nc := s.notifications()
	unread := nc.unreadCount()
	label := "通知"
	if unread > 0 {
		label = fmt.Sprintf("通知（%d 条未处理）", unread)
	}
	c.Key("notification-bell")
	ui.Column(c).Children(func() {
		anchor := components.Button(c, "", components.ButtonGhost).
			Padding(c.Theme().Space(.75), c.Theme().Space(1.5)).Radius(c.Theme().Space(4.5)).
			Label(label).Description("点击查看通知记录")
		anchor.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
				iconColor := t.Color.FgSecondary
				if unread > 0 {
					iconColor = t.Color.FgPrimary
				}
				components.Icon(c, lucide.Bell).Size(c.Theme().Space(4.5), c.Theme().Space(4.5)).TextColor(iconColor)
				if unread > 0 {
					countText := fmt.Sprintf("%d", unread)
					if unread > 99 {
						countText = "99+"
					}
					ui.Box(c).MinWidth(18).Height(18).Padding(0, 5).Radius(t.Radius.Full).
						Background(t.Color.Danger.Solid).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Text(c, countText).FontSize(11).Bold().FontFeatures("tnum").TextColor(ui.Hex("#ffffff")).SingleLine()
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
		components.Popover(c, anchor, &nc.bellOpen, func(panel ui.Element) {
			panel.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight).
				Width(360).MaxWidthPercent(100).Label("通知列表")
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
	ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, "通知", 3).Grow(1)
			if len(inbox) > 0 {
				if ghostButton(c, "全部已读").FontSize(t.Type.Caption).Clicked() {
					s.markAllNotificationsRead()
				}
			}
		})
		if len(inbox) == 0 {
			ui.Text(c, "暂无未处理通知。").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			return
		}
		ui.Scroll(c.Key("notif-inbox-scroll")).FillWidth().MaxHeight(380).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for _, item := range inbox {
					item := item
					cardBox := ui.Box(c.Key("inbox-"+item.ID)).FillWidth().Padding(12).Radius(16).
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
	if at.IsZero() {
		return "刚刚"
	}
	diff := time.Since(at)
	switch {
	case diff < time.Minute:
		return "刚刚"
	case diff < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(diff.Hours()))
	default:
		return at.Format("01-02 15:04")
	}
}
