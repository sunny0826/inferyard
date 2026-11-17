//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The preview shares the same sidebar builder; clicking 任务 switches the page
// and leaves the running preview task alone.
func TestPreviewTaskNavSwitchesWithoutReset(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "报告 · 列表"))
	nav := ui.NewTester(p.state.leftNav, 240, 900)
	click(t, nav, "任务")
	nav.Frame()
	if p.state.selected != "dashboard" {
		t.Fatalf("preview task nav did not select the task page: %q", p.state.selected)
	}
	if p.state.newTaskPending || p.state.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("preview task nav reset the run: %+v", p.state.pendingDestructive)
	}
}
