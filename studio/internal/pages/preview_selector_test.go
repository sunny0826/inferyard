//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
)

func TestPreviewSceneSelectorFitsCompactWindow(t *testing.T) {
	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1024, 600)
	click(t, tt, "模拟场景")
	first, ok := tt.Find("任务 · 失败")
	if !ok || first.Y < 0 || first.Y+first.H > 600 {
		t.Fatalf("scene option falls outside the window: %+v", first)
	}
	click(t, tt, "任务 · 失败")
	if p.state.engineState.State != "failed" {
		t.Fatal("scene selection did not reach the engine failure state")
	}
	click(t, tt, "模拟场景")
	list, ok := tt.Find("模拟场景列表")
	if !ok || list.Y < 0 || list.Y+list.H > 600 {
		t.Fatalf("scene list falls outside the window: %+v", list)
	}
	x, y := list.X+list.W/2, list.Y+list.H/2
	tt.Move(x, y)
	tt.Scroll(x, y, 0, 2000)
	last := "引擎 · LM Studio 手动绑定"
	item, ok := tt.Find(last)
	if !ok || item.Y < list.Y || item.Y+item.H > list.Y+list.H {
		t.Fatalf("last scene cannot be scrolled into view: %+v in %+v", item, list)
	}
	click(t, tt, last)
	if p.scene != previewSceneIndex(t, last) || p.state.localPID != "4001" {
		t.Fatal("last scene did not load after scrolling")
	}
}
