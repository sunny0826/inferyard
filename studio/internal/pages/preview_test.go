//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func previewTester(p *previewSession, width, height int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.theme(c))
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(205).Shrink(0).Children(func() { p.left(c) })
			p.content(c)
		})
	}, width, height)
}

func previewSceneIndex(t *testing.T, label string) int {
	t.Helper()
	for i, scene := range previewScenes {
		if scene.label == label {
			return i
		}
	}
	t.Fatal("unknown scene", label)
	return -1
}

func TestPreviewResetReturnsReportsToDefaultTab(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	p.state.selected = "reports"
	tt := previewTester(p, 1120, 1400)
	tt.Frame()
	click(t, tt, "运行历史")
	if !tt.HasText("运行历史表") {
		t.Fatalf("history tab did not open: %v", tt.Texts())
	}
	click(t, tt, "重置场景")
	tt.Frame()
	if p.state.reportsTab != reportsTabList {
		t.Fatalf("reset kept tab %d", p.state.reportsTab)
	}
	p.state.selected = "reports"
	tt.Frame()
	if !tt.HasText("打开报告") || tt.HasText("运行历史表") {
		t.Fatalf("reset kept the history tab: %v", tt.Texts())
	}
}

func TestPreviewOutOfBandActionsStayInMemory(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	s := p.state
	s.work(func() (string, *core.ApiError) { t.Fatal("preview executed closure"); return "", nil })
	s.startEngine()
	s.executeBinding("unused", nil)
	s.bindDiscovered(42, "http://127.0.0.1:48857")
	s.retryBenchmark()
	s.refresh(true)
	s.refreshAction()
	p.advance(20 * time.Second)
	if command, err := s.bindCommand("42", "http://127.0.0.1:48857"); command != "" || err != nil {
		t.Fatal(command, err)
	}
	if s.service != nil || s.taskCancel != nil {
		t.Fatal("preview executed a real bind")
	}
	p.advance(20 * time.Second)
	if s.busy || s.binding || s.taskCancel != nil || s.service != nil || s.engineState.State != "running" {
		t.Fatal("preview bind did not stay in memory", s.busy, s.engineState.State)
	}
}
