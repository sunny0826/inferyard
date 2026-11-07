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

func TestPreviewResetClearsFormAndWidgetState(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "设置 · 路径与目录名"))
	tt := previewTester(p, 1120, 2200)
	click(t, tt, "模型下载目录名")
	tt.Type("edited")
	if p.state.modelName == "model" {
		t.Fatal("form was not edited")
	}
	click(t, tt, "重置场景")
	if p.state.modelName != "model" {
		t.Fatal("reset kept edited value")
	}
	p.loadScene(previewSceneIndex(t, "报告 · 列表"))
	tt.Frame()
	click(t, tt, "从历史运行另做报告")
	if !tt.HasText("选择来源 run（1–100 个）") {
		t.Fatal("fold did not open")
	}
	click(t, tt, "重置场景")
	if tt.HasText("选择来源 run（1–100 个）") {
		t.Fatal("reset kept UI-local fold state")
	}
	click(t, tt, "preview-run")
	if selectedRow(&p.state.historyTable, 1) != 0 {
		t.Fatal("row did not select")
	}
	click(t, tt, "重置场景")
	if selectedRow(&p.state.historyTable, 1) != -1 {
		t.Fatal("reset kept selected row")
	}
}

func TestPreviewActionButtonsStayInMemory(t *testing.T) {
	for _, tc := range []struct{ scene, action string }{
		{"模型 · 可用组合", "刷新服务列表"},
		{"设置 · 路径与目录名", "保存"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			p := newPreviewSession(context.Background())
			p.loadScene(previewSceneIndex(t, tc.scene))
			tt := previewTester(p, 1120, 2400)
			click(t, tt, tc.action)
			p.advance(20 * time.Second)
			tt.Frame()
			if tt.HasText(previewNotice) || p.state.busy || p.state.taskCancel != nil || p.state.service != nil {
				t.Fatal("action was not simulated or escaped preview", tt.Texts())
			}
		})
	}
}

func TestPreviewOutOfBandActionsStayInMemory(t *testing.T) {
	p := newPreviewSession(context.Background())
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
	if s.busy || s.binding || s.taskCancel != nil || s.service != nil {
		t.Fatal("preview left asynchronous work")
	}
}
