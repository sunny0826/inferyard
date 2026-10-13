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

func TestChromeRefreshActions(t *testing.T) {
	s := testState()
	entered := make(chan struct{}, 10)
	s.ctx = refreshContext{context.Background(), entered}
	tt := ui.NewTester(s.view, 1120, 800)
	for _, action := range []string{"button", "shortcut", "focused input"} {
		if action == "focused input" {
			s.selected = "settings"
			tt.Frame()
			click(t, tt, "CLI 绝对路径（留空使用 PATH）")
		}
		if action == "button" {
			click(t, tt, "刷新")
		} else {
			tt.Key(ui.Cmd, ui.KeyR)
		}
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatalf("%s did not request refresh", action)
		}
	}
	s.busy = true
	tt.Frame()
	click(t, tt, "刷新")
	tt.Key(ui.Cmd, ui.KeyR)
	select {
	case <-entered:
		t.Fatal("busy refresh was not disabled")
	case <-time.After(30 * time.Millisecond):
	}
	if !tt.HasText("执行中…") || tt.HasText("正在执行，请稍候…") {
		t.Fatal(tt.Texts())
	}
	s.busy = false
	tt.Frame()
	tt.Frame()
	if tt.HasText("执行中…") {
		t.Fatal("busy indicator remained")
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
	for _, page := range []string{"settings", "runs", "models", "reports", "wizard", "detail"} {
		t.Run(page, func(t *testing.T) {
			s := testState()
			s.selected = page
			labels := map[string][]string{
				"settings": {"保存设置", "准备目录", "修改设置"},
				"runs":     {"启动运行"}, "models": {"下载模型"},
				"reports": {"打开报告"}, "wizard": {"下一步：在运行页启动与绑定服务"},
				"detail": {"返回运行", "取消任务"},
			}
			if page == "detail" {
				s.selected, s.detailID = "runs", "fixture"
				s.detail = &core.RunTaskDetail{Task: core.RunTask{TaskId: "fixture", State: "running"}}
			}
			tt := ui.NewTester(s.view, 1120, 2400)
			count := 0
			for _, text := range tt.Texts() {
				if text == "刷新" {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("refresh count = %d", count)
			}
			for _, label := range labels[page] {
				bounds, ok := tt.Find(label)
				if !ok || bounds.W >= 600 {
					t.Fatalf("%s is missing or stretched: %+v", label, bounds)
				}
			}
			tt.SetSize(1120, 800)
			before, _ := tt.Find("刷新")
			tt.Scroll(700, 500, 0, 600)
			after, _ := tt.Find("刷新")
			if before != after {
				t.Fatal("toolbar scrolled with content")
			}
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
