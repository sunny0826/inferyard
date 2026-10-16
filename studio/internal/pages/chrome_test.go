package pages

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// Record refresh entry and stop before any backend or native-window work.
type refreshContext struct {
	context.Context
	entered chan struct{}
}

func (c refreshContext) Err() error {
	c.entered <- struct{}{}
	return context.Canceled
}

func TestChromeRefreshShortcutRemoved(t *testing.T) {
	s := testState()
	entered := make(chan struct{}, 10)
	s.ctx = refreshContext{context.Background(), entered}
	tt := ui.NewTester(s.view, 1120, 800)
	for _, page := range []string{"dashboard", "settings"} {
		s.selected = page
		tt.Frame()
		if page == "settings" {
			click(t, tt, "CLI 绝对路径（留空使用 PATH）")
		}
		tt.Key(ui.Cmd, ui.KeyR)
		select {
		case <-entered:
			t.Fatal("Cmd+R still refreshes")
		case <-time.After(30 * time.Millisecond):
		}
		if _, ok := tt.Find("刷新"); ok {
			t.Fatal("global refresh button remains")
		}
	}
}

func TestCompletionToastAndInlineError(t *testing.T) {
	s := testState()
	closeToast := false
	tt := ui.NewTester(func(c *ui.Context) {
		s.view(c)
		if closeToast {
			c.CloseToast("设置已保存。")
		}
	}, 1120, 800)
	s.busy = true
	s.finishWork("设置已保存。", nil)
	tt.Frame()
	if !tt.HasText("设置已保存。") || s.pendingToast != "" || s.busy {
		t.Fatal("completion was not consumed as a toast", tt.Texts())
	}
	toast, ok := tt.Find("设置已保存。")
	if !ok || toast.Y < 600 {
		t.Fatalf("message is not in toast viewport: %+v", toast)
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
	s.finishWork("must not toast", &core.ApiError{Code: "invalid_request", Message: "输入错误"})
	tt.Frame()
	if tt.HasText("must not toast") || !tt.HasText("输入错误 · invalid_request") {
		t.Fatal("failure did not remain inline", tt.Texts())
	}
}

func TestChromeLayoutAcrossPages(t *testing.T) {
	for _, page := range []string{"settings", "dashboard", "models", "downloads", "reports", "detail"} {
		t.Run(page, func(t *testing.T) {
			s := testState()
			s.selected = page
			labels := map[string][]string{
				"settings":  {"保存"},
				"dashboard": {"启动运行"}, "models": {"到下载页"}, "downloads": {"下载引擎"},
				"reports": {"打开报告"},
				"detail":  {"返回 Dashboard", "取消任务"},
			}
			if page == "detail" {
				s.selected, s.detailID = "dashboard", "fixture"
				s.detail = &core.RunTaskDetail{Task: core.RunTask{TaskId: "fixture", State: "running"}}
			}
			tt := ui.NewTester(s.view, 1120, 2400)
			count := 0
			for _, text := range tt.Texts() {
				if text == "刷新" {
					count++
				}
			}
			if count != 0 {
				t.Fatalf("refresh count = %d", count)
			}
			for _, label := range labels[page] {
				bounds, ok := tt.Find(label)
				if !ok || bounds.W >= 600 {
					t.Fatalf("%s is missing or stretched: %+v", label, bounds)
				}
			}
			tt.SetSize(1120, 800)
			s.busy = true
			tt.Frame()
			before, ok := tt.Find("执行中")
			if !ok {
				t.Fatal("missing busy status")
			}
			tt.Scroll(700, 500, 0, 600)
			after, ok := tt.Find("执行中")
			if !ok || before != after {
				t.Fatal("toolbar scrolled with content")
			}
			s.busy = false
			tt.Frame()
			if page == "settings" || page == "models" || page == "reports" {
				tt.Scroll(700, 500, 0, -1000)
				if dir := os.Getenv("STUDIO_UI_PREVIEW_DIR"); dir != "" {
					f, err := os.Create(filepath.Join(dir, "chrome-"+page+".png"))
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					if err := png.Encode(f, tt.Image()); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
