package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

// The production window and the preview window build the page content through
// contentView, not through the test-only view. The completion toast and the
// assistive-technology announcements are consumed there, so a success message
// reaches the user in the window too.
func TestProductionContentPathShowsToastAndAnnounces(t *testing.T) {
	s := testState()
	tt := ui.NewTester(func(c *ui.Context) {
		s.contentView(c)
		studioToasts(c)
	}, 1120, 800)

	s.busy = true
	s.finishWork("设置已保存。", nil)
	tt.Frame()
	if !tt.HasText("设置已保存。") {
		t.Fatalf("the window path dropped the completion message: %v", tt.Texts())
	}
	if s.pendingToast != "" {
		t.Fatal("the completion message was not consumed")
	}
	toast, ok := tt.Find("设置已保存。")
	if !ok || toast.Y > 200 {
		t.Fatalf("the message is not in the top toast viewport: %+v", toast)
	}
}

// The page announcement belongs to the shared content path as well, so a
// screen reader learns about the page that the window shows.
func TestProductionContentPathAnnouncesPage(t *testing.T) {
	s := testState()
	tt := ui.NewTester(func(c *ui.Context) {
		s.contentView(c)
		studioToasts(c)
	}, 1120, 800)

	if got := tt.Announcements(); len(got) != 1 || got[0] != "任务" {
		t.Fatalf("page announcement = %v", got)
	}
	for range 3 {
		tt.Frame()
	}
	if got := tt.Announcements(); len(got) != 0 {
		t.Fatalf("idle frames repeat the page announcement: %v", got)
	}
	s.selected = "settings"
	tt.Frame()
	if got := tt.Announcements(); len(got) != 1 || got[0] != "设置" {
		t.Fatalf("page change announcement = %v", got)
	}
}

// An error stays in the page and is announced once, not turned into a toast.
func TestProductionContentPathKeepsErrorsInline(t *testing.T) {
	s := testState()
	tt := ui.NewTester(func(c *ui.Context) {
		s.contentView(c)
		studioToasts(c)
	}, 1120, 800)

	s.busy = true
	s.finishWork("不应弹出", &core.ApiError{Code: "invalid_request", Message: "请修正路径"})
	tt.Frame()
	if tt.HasText("不应弹出") {
		t.Fatal("a failure was shown as a toast")
	}
	got := tt.Announcements()
	if len(got) != 2 || !strings.Contains(got[1], "请修正路径") {
		t.Fatalf("failure announcement = %v", got)
	}
}

// The shell stacks the toasts at the top center, and the placement is set
// rather than left to the library's default, which the upgrade changed.
func TestShellPlacesToastsAtTheTop(t *testing.T) {
	options := pageShellOptions("InferYard Studio", "", nil, nil)
	if options.Toasts.Placement != components.ToastTop {
		t.Fatalf("toast placement = %v, want the top center", options.Toasts.Placement)
	}
}
