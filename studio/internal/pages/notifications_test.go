package pages

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestTopRightNotificationCloseButtonPositionAndDismiss(t *testing.T) {
	s := readyState("dashboard")
	s.pushNotification(studioNotification{
		Key:   "report:r-1",
		Title: "报告已生成",
		Body:  "测评报告 r-1 已生成，可随时打开查看。",
		Kind:  stateSuccess,
	})

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()

	cardInfo, ok := tt.AccessByRole(ui.RoleStatus, "报告已生成")
	if !ok {
		t.Fatalf("expected notification card to be visible, texts=%v", tt.Texts())
	}
	cardRect := cardInfo.Bounds
	closeLabel := "关闭通知：报告已生成"
	closeRect, ok := tt.Find(closeLabel)
	if !ok {
		t.Fatalf("expected close button %q to be visible without hovering first", closeLabel)
	}
	timeRect, ok := tt.Find("刚刚")
	if !ok {
		t.Fatalf("expected relative timestamp to be visible, texts=%v", tt.Texts())
	}

	// Close button must sit in the top-right corner of the card, to the right of
	// both the card midpoint and the timestamp, aligned with the top header row.
	cardMidX := cardRect.X + cardRect.W/2
	if closeRect.X <= cardMidX {
		t.Fatalf("expected close button on right half of card: close=%+v card=%+v", closeRect, cardRect)
	}
	if closeRect.X+closeRect.W > cardRect.X+cardRect.W+1 {
		t.Fatalf("expected close button inside right edge of card: close=%+v card=%+v", closeRect, cardRect)
	}
	if closeRect.X < timeRect.X+timeRect.W-1 {
		t.Fatalf("expected close button to the right of timestamp without overlap: close=%+v time=%+v", closeRect, timeRect)
	}
	if closeRect.Y > cardRect.Y+cardRect.H/2 {
		t.Fatalf("expected close button in top half of card: close=%+v card=%+v", closeRect, cardRect)
	}

	// Clicking the top-right close button dismisses the popup immediately.
	click(t, tt, closeLabel)
	tt.Advance(500 * time.Millisecond)
	if len(s.notifications().screenItems()) != 0 {
		t.Fatalf("expected notification dismissed from screen, remaining=%d", len(s.notifications().screenItems()))
	}
	if tt.HasText("测评报告 r-1 已生成，可随时打开查看。") {
		t.Fatalf("expected dismissed notification body gone from screen: %v", tt.Texts())
	}
}

func TestTopRightNotificationAutoDismissesAndPreservesInbox(t *testing.T) {
	s := readyState("dashboard")
	s.pushNotification(studioNotification{
		Key:   "report:auto-1",
		Title: "报告已生成",
		Body:  "测评报告 auto-1 已生成。",
		Kind:  stateSuccess,
	})
	s.pushNotification(studioNotification{
		Key:           "engine:failed",
		Title:         "托管引擎异常退出",
		Body:          "请检查引擎状态并重新启动。",
		Kind:          stateDanger,
		NeedsDecision: true,
	})

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()
	if len(s.notifications().screenItems()) != 2 {
		t.Fatalf("expected 2 notifications on screen initially, got %d", len(s.notifications().screenItems()))
	}

	// Still visible before the default 6s timeout.
	tt.Advance(5 * time.Second)
	if len(s.notifications().screenItems()) != 2 {
		t.Fatalf("expected both notifications visible at 5s, got %d", len(s.notifications().screenItems()))
	}

	// At 6s, the standard success notification auto-dismisses while the 8s
	// attention/error notification stays briefly longer.
	tt.Advance(1 * time.Second)
	screen := s.notifications().screenItems()
	if len(screen) != 1 || screen[0].Key != "engine:failed" {
		t.Fatalf("expected only attention notification remaining at 6s, got %+v", screen)
	}

	// At 8s, the attention/error notification also auto-dismisses so nothing stays
	// stuck in the top-right corner.
	tt.Advance(2 * time.Second)
	if len(s.notifications().screenItems()) != 0 {
		t.Fatalf("expected all top-right notifications auto-dismissed by 8s, got %d", len(s.notifications().screenItems()))
	}

	// Auto-dismissed notifications stay unread in the bell inbox for later review.
	if got := s.notifications().unreadCount(); got != 2 {
		t.Fatalf("expected 2 unread notifications preserved in inbox, got %d", got)
	}
	if got := len(s.notifications().inboxItems()); got != 2 {
		t.Fatalf("expected 2 notifications in inbox, got %d", got)
	}

	// Opening the inbox popover renders the preserved items without screen close buttons.
	s.notifications().bellOpen = true
	tt.Frame()
	if !tt.HasText("测评报告 auto-1 已生成。") || !tt.HasText("请检查引擎状态并重新启动。") {
		t.Fatalf("expected auto-dismissed notifications in bell inbox, texts=%v", tt.Texts())
	}
	if _, ok := tt.Find("关闭通知：报告已生成"); ok {
		t.Fatal("inbox notification cards should not render the screen popup close button")
	}
}

func TestTopRightNotificationHoverPausesAutoDismiss(t *testing.T) {
	s := readyState("dashboard")
	s.pushNotification(studioNotification{
		Key:   "dl:1:done",
		Title: "模型下载完成",
		Body:  "qwen 已就绪。",
		Kind:  stateSuccess,
	})

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()

	// Let 3 seconds pass, then hover over the card.
	tt.Advance(3 * time.Second)
	rect, ok := tt.Find("模型下载完成")
	if !ok {
		t.Fatalf("expected notification still visible at 3s")
	}
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()

	// While hovered, even 10 seconds passing must not dismiss the card.
	tt.Advance(10 * time.Second)
	if len(s.notifications().screenItems()) != 1 {
		t.Fatalf("expected hovered notification to remain visible, got %d", len(s.notifications().screenItems()))
	}

	// Move pointer away; remaining ~3s resumes and auto-dismisses on expiry.
	tt.Move(20, 20)
	tt.Frame()
	tt.Advance(2 * time.Second)
	if len(s.notifications().screenItems()) != 1 {
		t.Fatalf("expected notification still visible with ~1s remaining, got %d", len(s.notifications().screenItems()))
	}
	tt.Advance(1500 * time.Millisecond)
	if len(s.notifications().screenItems()) != 0 {
		t.Fatalf("expected notification auto-dismissed after hover ended, got %d", len(s.notifications().screenItems()))
	}
}

func TestTopRightNotificationOverflowQueueAndKeyRefreshReset(t *testing.T) {
	s := readyState("dashboard")
	for _, key := range []string{"k1", "k2", "k3", "k4"} {
		s.pushNotification(studioNotification{
			Key:   key,
			Title: "通知 " + key,
			Body:  "正文 " + key,
			Kind:  stateInfo,
		})
	}

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()

	// First 3 (k4, k3, k2) expire after 6s; queued oldest item (k1) then enters
	// screen and gets a fresh 6s window.
	tt.Advance(6 * time.Second)
	screen := s.notifications().screenItems()
	if len(screen) != 1 || screen[0].Key != "k1" {
		t.Fatalf("expected queued notification k1 to remain after first batch expires, got %+v", screen)
	}

	// Refreshing k1 mid-countdown resets its auto-dismiss timer.
	tt.Advance(4 * time.Second)
	s.pushNotification(studioNotification{
		Key:   "k1",
		Title: "通知 k1 更新",
		Body:  "正文 k1 更新",
		Kind:  stateInfo,
	})
	tt.Frame()

	tt.Advance(4 * time.Second)
	if len(s.notifications().screenItems()) != 1 {
		t.Fatalf("expected refreshed notification to still be visible at 4s after refresh")
	}
	tt.Advance(2500 * time.Millisecond)
	if len(s.notifications().screenItems()) != 0 {
		t.Fatalf("expected refreshed notification to auto-dismiss after full window")
	}
}

func TestTopRightNotificationEvictedCardResumesRemainingTime(t *testing.T) {
	s := readyState("dashboard")
	s.pushNotification(studioNotification{
		Key:   "dl:a",
		Title: "下载 A 完成",
		Body:  "模型 A 已就绪。",
		Kind:  stateSuccess,
	})

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()

	// Notification A displays for 1s, leaving ~5s remaining.
	tt.Advance(1 * time.Second)
	if !tt.HasText("模型 A 已就绪。") {
		t.Fatalf("expected notification A visible at 1s")
	}

	// Three newer notifications arrive at the front of the stack, evicting A
	// from the top-3 visible window while it waits in the queue.
	for _, key := range []string{"dl:b", "dl:c", "dl:d"} {
		s.pushNotification(studioNotification{
			Key:   key,
			Title: "下载 " + key,
			Body:  "正文 " + key,
			Kind:  stateSuccess,
		})
	}
	tt.Frame()

	// After 6s, the 3 newer notifications auto-dismiss and A returns to the
	// screen; its queued time must not have counted against its remaining ~5s.
	tt.Advance(6 * time.Second)
	screen := s.notifications().screenItems()
	if len(screen) != 1 || screen[0].Key != "dl:a" {
		t.Fatalf("expected notification A to return to screen after newer batch expires, got %+v", screen)
	}

	// Still visible 4s after returning (1s before eviction + 4s after = 5s < 6s).
	tt.Advance(4 * time.Second)
	if len(s.notifications().screenItems()) != 1 {
		t.Fatalf("expected notification A still visible with ~1s remaining after re-entering screen")
	}

	// Auto-dismisses once the remaining ~1s elapses.
	tt.Advance(1500 * time.Millisecond)
	if len(s.notifications().screenItems()) != 0 {
		t.Fatalf("expected notification A auto-dismissed after completing remaining window")
	}
}

