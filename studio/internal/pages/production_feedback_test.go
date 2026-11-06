package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// The production window and the preview window build the page content through
// contentView, not through the test-only view. The completion toast and the
// assistive-technology announcements are consumed there, so a success message
// reaches the user in the window too.
func TestProductionContentPathShowsToastAndAnnounces(t *testing.T) {
	s := testState()
	closeToast := false
	tt := ui.NewTester(func(c *ui.Context) {
		s.contentView(c)
		studioToasts(c)
		if closeToast {
			c.CloseToast("设置已保存。")
		}
	}, 1120, 800)

	s.busy = true
	s.finishWork("设置已保存。", nil)
	tt.Frame()
	if !tt.HasText("设置已保存。") {
		t.Fatalf("the window path dropped the completion message: %v", tt.Texts())
	}
	closeToast = true
	tt.Frame()
	closeToast = false
	for range 3 {
		tt.Frame()
	}
	if tt.HasText("设置已保存。") {
		t.Fatal("toast reappeared after dismissal")
	}
}

// The page announcement belongs to the shared content path as well, so a
// screen reader learns about the page that the window shows.
func TestProductionContentPathAnnouncesPage(t *testing.T) {
	s := testState()
	s.detailError = &core.ApiError{Code: "internal_error", Message: "hidden detail"}
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
	if !tt.HasText("请修正路径") {
		t.Fatal("failure is not readable inline", tt.Texts())
	}
	got := tt.Announcements()
	if len(got) != 2 || !strings.Contains(got[1], "请修正路径") {
		t.Fatalf("failure announcement = %v", got)
	}
}
