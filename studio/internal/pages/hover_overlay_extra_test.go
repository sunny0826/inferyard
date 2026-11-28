package pages

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func pushTestStackNotifications(s *viewState) {
	s.pushNotification(studioNotification{
		Key:   "n-1",
		Title: "通知一",
		Body:  "第一条消息。",
		Kind:  stateInfo,
	})
	s.pushNotification(studioNotification{
		Key:   "n-2",
		Title: "通知二",
		Body:  "第二条消息。",
		Kind:  stateSuccess,
	})
}

func TestScreenNotificationStackHoverIntentGraceAndImmediateCollapse(t *testing.T) {
	s := readyState("dashboard")
	pushTestStackNotifications(s)
	clearAllLabel := tr(nil, "notif.clear_all")

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()

	topCard, ok := tt.AccessByRole(ui.RoleStatus, "通知二")
	if !ok {
		t.Fatalf("expected top notification card visible, texts=%v", tt.Texts())
	}
	cx := topCard.Bounds.X + topCard.Bounds.W/2
	cy := topCard.Bounds.Y + topCard.Bounds.H/2

	// 1. Fast pointer sweep across the notification corner does NOT expand the stack.
	tt.Move(topCard.Bounds.X-40, cy)
	tt.Frame()
	for x := topCard.Bounds.X - 10; x <= topCard.Bounds.X+topCard.Bounds.W; x += 45 {
		tt.Advance(16 * time.Millisecond)
		tt.Move(x, cy)
		tt.Frame()
		if tt.HasText(clearAllLabel) {
			t.Fatalf("fast sweep across notification corner must not expand stack at x=%.0f", x)
		}
	}

	// Move out and settle velocity; advance 3s of screen lifetime.
	tt.Move(20, 20)
	tt.Advance(3 * time.Second)

	// 2. Hover pauses countdown at 0ms without expanding before 700ms.
	tt.Move(cx, cy)
	tt.Frame()
	tt.Advance(400 * time.Millisecond)
	if tt.HasText(clearAllLabel) {
		t.Fatal("notification stack expanded before 700ms hover delay")
	}
	if len(s.notifications().screenItems()) != 2 {
		t.Fatal("expected both notifications still on screen while hovered")
	}

	// 3. Reaching 700ms rest expands stack and reveals Clear All.
	tt.Advance(360 * time.Millisecond)
	if !tt.HasText(clearAllLabel) {
		t.Fatalf("expected notification stack expanded after 700ms rest, texts=%v", tt.Texts())
	}
	// Even 8s passing while expanded keeps auto-dismiss paused.
	tt.Advance(8 * time.Second)
	if len(s.notifications().screenItems()) != 2 {
		t.Fatalf("expected expanded stack to pause auto-dismiss, got %d items", len(s.notifications().screenItems()))
	}

	// 4. Slipping pointer outside stack for 200ms (< 300ms grace) and returning keeps stack expanded
	// and Clear All clickable.
	tt.Move(20, 20)
	tt.Advance(200 * time.Millisecond)
	clearBtn, ok := tt.AccessByRole(ui.RoleButton, clearAllLabel)
	if !ok {
		t.Fatalf("expected Clear All button still mounted during 300ms close grace, texts=%v", tt.Texts())
	}
	tt.Move(clearBtn.Bounds.X+clearBtn.Bounds.W/2, clearBtn.Bounds.Y+clearBtn.Bounds.H/2)
	tt.Advance(250 * time.Millisecond)
	if !tt.HasText(clearAllLabel) {
		t.Fatal("returning within 300ms grace failed to cancel stack collapse")
	}
	click(t, tt, clearAllLabel)
	tt.Frame()
	if len(s.notifications().screenItems()) != 0 {
		t.Fatalf("expected Clear All click to clear screen notifications, got %d", len(s.notifications().screenItems()))
	}

	// 5. Leaving stack for > 300ms collapses it cleanly.
	pushTestStackNotifications(s)
	tt.Move(20, 20)
	tt.Advance(200 * time.Millisecond)
	topCard, _ = tt.AccessByRole(ui.RoleStatus, "通知二")
	cx, cy = topCard.Bounds.X+topCard.Bounds.W/2, topCard.Bounds.Y+topCard.Bounds.H/2
	tt.Move(cx, cy)
	tt.Advance(760 * time.Millisecond)
	if !tt.HasText(clearAllLabel) {
		t.Fatal("expected stack expanded after 700ms rest")
	}
	tt.Move(20, 20)
	tt.Advance(360 * time.Millisecond)
	tt.Advance(160 * time.Millisecond)
	if tt.HasText(clearAllLabel) {
		t.Fatal("expected stack collapsed after >300ms outside")
	}

	// 6. Click opens at 0ms; Escape and outside click collapse at 0ms without deleting cards.
	tt.ClickAt(cx, cy)
	tt.Frame()
	if !tt.HasText(clearAllLabel) {
		t.Fatal("expected clicking top collapsed card to expand stack at 0ms")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(160 * time.Millisecond)
	if tt.HasText(clearAllLabel) {
		t.Fatal("expected Escape to collapse expanded stack immediately")
	}
	if len(s.notifications().screenItems()) != 2 {
		t.Fatalf("Escape on expanded stack must collapse without dismissing cards, got %d", len(s.notifications().screenItems()))
	}

	tt.ClickAt(cx, cy)
	tt.Frame()
	if !tt.HasText(clearAllLabel) {
		t.Fatal("expected click to re-expand stack")
	}
	tt.ClickAt(20, 20)
	tt.Advance(160 * time.Millisecond)
	if tt.HasText(clearAllLabel) {
		t.Fatal("expected outside click to collapse expanded stack immediately")
	}
}

func TestResourceChartHoverIntentWarmHandoffAndKeyboard(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*3600)
	t.Cleanup(func() { time.Local = oldLocal })

	samples := chartSamples()
	s := testState()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(1080).Children(func() { s.resourceView(c, samples) })
	}, 1120, 1400)
	tt.Frame()
	tt.Frame()

	plot1, ok1 := tt.Find("可用内存 GiB曲线")
	plot2, ok2 := tt.Find("服务 RSS GiB曲线")
	if !ok1 || !ok2 {
		t.Fatalf("missing resource plots: plot1=%v plot2=%v", ok1, ok2)
	}
	if _, okRole := tt.AccessByRole(ui.RoleGroup, "可用内存 GiB曲线"); !okRole {
		t.Fatalf("expected resource plot to keep RoleGroup accessibility semantics, access=%+v", tt.AccessAll())
	}
	y1 := plot1.Y + plot1.H/2
	y2 := plot2.Y + plot2.H/2

	// 1. Fast sweep across both sibling plots opens no tooltip.
	tt.Move(10, y1)
	tt.Frame()
	for x := float32(40); x <= plot2.X+plot2.W; x += 80 {
		tt.Advance(16 * time.Millisecond)
		tt.Move(x, y1)
		tt.Frame()
		if hasOpenTooltip(tt, "") {
			t.Fatalf("fast sweep across resource charts opened tooltip %+v", tt.AccessAll())
		}
	}

	tt.Move(4, 4)
	tt.Advance(500 * time.Millisecond)

	// 2. Hover < 500ms opens no tooltip; reaching 500ms rest opens sample 0 tooltip.
	leftX := plot1.X + plot1.W*0.18
	rightX := plot1.X + plot1.W*0.82
	tt.Move(leftX, y1)
	tt.Advance(250 * time.Millisecond)
	if hasOpenTooltip(tt, "2026-10-14 09:00:00 · 1.00 GiB") {
		t.Fatal("resource chart tooltip opened before 500ms cold hover delay")
	}
	tt.Advance(310 * time.Millisecond)
	if !hasOpenTooltip(tt, "2026-10-14 09:00:00 · 1.00 GiB") {
		t.Fatalf("expected first sample tooltip after 500ms rest, access=%+v", tt.AccessAll())
	}

	// 3. Moving across X positions inside the active chart updates the sample at 0ms.
	tt.Move(rightX, y1)
	tt.Frame()
	if !hasOpenTooltip(tt, "2026-10-14 09:00:09 · 2.00 GiB") {
		t.Fatalf("expected 0ms sample update on horizontal scrub, access=%+v", tt.AccessAll())
	}

	// 4. Moving to sibling resource chart within 400ms opens its tooltip at 0ms.
	tt.Advance(40 * time.Millisecond)
	tt.Move(plot2.X+plot2.W*0.18, y2)
	tt.Frame()
	if !hasOpenTooltip(tt, "2026-10-14 09:00:00 · 0.50 GiB") {
		t.Fatalf("expected 0ms warm handoff to sibling RSS chart, access=%+v", tt.AccessAll())
	}

	// 5. Leaving all charts > 400ms restores the 500ms rest wait.
	tt.Move(4, 4)
	tt.Advance(460 * time.Millisecond)
	tt.Move(leftX, y1)
	tt.Advance(220 * time.Millisecond)
	if hasOpenTooltip(tt, "2026-10-14 09:00:00 · 1.00 GiB") {
		t.Fatal("expected cold 500ms delay after >400ms outside all resource charts")
	}

	// 6. Keyboard focus + Home/End/Escape work at 0ms.
	ttKey := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(1080).Children(func() { s.resourceView(c, samples) })
	}, 1120, 1400)
	ttKey.Frame()
	ttKey.Key(0, ui.KeyTab)
	ttKey.Key(0, ui.KeyHome)
	if !hasOpenTooltip(ttKey, "2026-10-14 09:00:00 · 1.00 GiB") {
		t.Fatalf("expected 0ms keyboard Home tooltip, access=%+v", ttKey.AccessAll())
	}
	ttKey.Key(0, ui.KeyEnd)
	if !hasOpenTooltip(ttKey, "2026-10-14 09:00:09 · 2.00 GiB") {
		t.Fatalf("expected 0ms keyboard End tooltip, access=%+v", ttKey.AccessAll())
	}
	ttKey.Key(0, ui.KeyEscape)
	ttKey.Advance(80 * time.Millisecond)
	if hasOpenTooltip(ttKey, "") {
		t.Fatalf("expected Escape to clear chart selection and hide tooltip, access=%+v", ttKey.AccessAll())
	}
}

func TestPopoverSuppressesAnchorTooltipAndReduceMotionWorks(t *testing.T) {
	t.Run("PopoverSuppression", func(t *testing.T) {
		popOpen := true
		view := func(c *ui.Context) {
			ui.Column(c).Padding(24).Children(func() {
				anchor := outlineIconButton(c, lucide.Bell, "Bell Tooltip")
				if anchor.Clicked() {
					popOpen = !popOpen
				}
				studioPopover(c, anchor, &popOpen, func(panel ui.Element) {
					panel.Label("Bell Panel")
					panel.Children(func() {
						ui.Text(c, "Popover Content")
					})
				})
			})
		}

		tt := ui.NewTester(view, 480, 320)
		tt.Frame()

		btn, ok := tt.AccessByRole(ui.RoleButton, "Bell Tooltip")
		if !ok {
			t.Fatal("missing Bell anchor button")
		}
		// While popover is open, hovering anchor > 500ms must NOT show anchor tooltip.
		tt.Move(btn.Bounds.X+btn.Bounds.W/2, btn.Bounds.Y+btn.Bounds.H/2)
		tt.Advance(600 * time.Millisecond)
		if hasOpenTooltip(tt, "") {
			t.Fatalf("anchor tooltip must stay suppressed while studioPopover is open, got %+v", tt.AccessAll())
		}

		// Closing popover allows anchor tooltip to open on subsequent hover.
		popOpen = false
		tt.Frame()
		tt.Move(4, 4)
		tt.Advance(460 * time.Millisecond)
		tt.Move(btn.Bounds.X+btn.Bounds.W/2, btn.Bounds.Y+btn.Bounds.H/2)
		tt.Advance(560 * time.Millisecond)
		if !hasOpenTooltip(tt, "Bell Tooltip") {
			t.Fatalf("expected anchor tooltip once popover is closed, access=%+v", tt.AccessAll())
		}
	})

	t.Run("ReduceMotion", func(t *testing.T) {
		s := readyState("dashboard")
		pushTestStackNotifications(s)
		drawerOpen := true
		caseOpen := true
		popOpen := true

		view := func(c *ui.Context) {
			ui.Column(c).Padding(16).Gap(12).Children(func() {
				studioTooltipGroup(c, ui.Row(c).Gap(8), func() {
					outlineIconButton(c, lucide.Copy, "Reduced Copy")
					anchor := outlineIconButton(c, lucide.Info, "Reduced Info")
					studioPopover(c, anchor, &popOpen, func(panel ui.Element) {
						panel.Children(func() { ui.Text(c, "Reduced Popover") })
					})
				})
				s.resourceView(c, chartSamples())
			})
			s.studioNotifications(c)
			drawer(c, &drawerOpen, drawerOptions{Title: "Reduced Drawer"}, func(ui.Element) {
				ui.Text(c, "Drawer Body")
			})
			s.caseDetailDrawer(c, "reduced-case-modal", &caseOpen, caseDetailItem{
				Key:      "c1",
				CaseID:   "case-001",
				Prompt:   "Hello",
				Response: "World",
				Total:    2,
			})
		}

		tt := ui.NewTesterWith(view, 1120, 900, ui.TesterOptions{
			Preferences: ui.Preferences{ReduceMotion: true},
		})
		tt.Frame()

		if !tt.HasText("case-001") {
			t.Fatalf("missing case-001 modal under ReduceMotion, texts=%v", tt.Texts())
		}
		caseOpen = false
		drawerOpen = false
		popOpen = false
		tt.Frame()
		tt.Advance(250 * time.Millisecond)

		copyBtn, ok := tt.AccessByRole(ui.RoleButton, "Reduced Copy")
		if !ok {
			t.Fatalf("missing Reduced Copy button after modal close, texts=%v", tt.Texts())
		}
		tt.Move(copyBtn.Bounds.X+copyBtn.Bounds.W/2, copyBtn.Bounds.Y+copyBtn.Bounds.H/2)
		tt.Advance(560 * time.Millisecond)
		if !hasOpenTooltip(tt, "Reduced Copy") {
			t.Fatalf("expected tooltip under ReduceMotion, access=%+v", tt.AccessAll())
		}
	})
}
