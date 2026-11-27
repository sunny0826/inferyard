package pages

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/l10n"
)

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
			studioTooltip(c, anchor, label)
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
