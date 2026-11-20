//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

// 新任务 returns the preview to the regular benchmark, matching production, and
// 用此组合新开任务 still reopens the specialty round it was started from.
func TestPreviewNewTaskResetsBenchmarkTypeParity(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	p.state.speed.selected = true
	p.state.newBenchmark()
	p.advance(time.Second)
	if p.state.speed.selected {
		t.Fatal("preview 新任务 kept the specialty type")
	}
	p.state.speed.selected = true
	p.state.newSpeedTask(&speedTask{id: "speed", model: "Model-A", engine: "Engine-A", context: inferyard.ContextSpeed128K, selectionModel: "Model-A", selectionEngine: "Engine-A"})
	p.advance(time.Second)
	if !p.state.speed.selected || p.state.speed.context != inferyard.ContextSpeed128K {
		t.Fatalf("preview 用此组合新开任务 lost the specialty type: %v %d", p.state.speed.selected, p.state.speed.context)
	}
}
