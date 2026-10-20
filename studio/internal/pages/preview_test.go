//go:build ui_debug && !release

package pages

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func previewTester(p *previewSession, width, height int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
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

func TestPreviewScenesAndNavigation(t *testing.T) {
	wants := []string{"准备环境", "42 / 120 题", "查看报告", "模型服务", "题目进度表", "历史任务", "各类任务表现", "preview-case-039", "重新读取报告", "暂无运行记录。", "选择模型与引擎", "下载失败", "Preview CPU", "路径设置", "专项历史任务", "准备环境", "采样记录：316 · 每个来源保留最近 600 点", "传感器温度 °C来源"}
	for i, scene := range previewScenes {
		t.Run(scene.label, func(t *testing.T) {
			p := newPreviewSession(context.Background())
			p.loadScene(i)
			tt := previewTester(p, 1120, 2400)
			if !tt.HasText(wants[i]) || !tt.HasText("全部为模拟数据；操作仅改变本窗口，不运行模型或写入文件。") {
				t.Fatal("scene not rendered", tt.Texts())
			}
			if p.state.service != nil || p.shellOptions().StateKey != "" {
				t.Fatal("preview can reach a real service or persisted window state")
			}
			for _, label := range []string{"设备", "模型", "下载", "设置", "报告", "新任务"} {
				click(t, tt, label)
			}
			p.advance(time.Second)
			if p.state.service != nil || p.state.selected != "dashboard" {
				t.Fatal("preview navigation escaped fixtures")
			}
		})
	}
}

func TestPreviewSelectorAndCompletedReport(t *testing.T) {
	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1120, 1600)
	click(t, tt, "模拟场景")
	click(t, tt, "任务 · 已完成")
	click(t, tt, "查看报告")
	if !tt.HasText("Fixture Model Q4") || !tt.HasText("1.24 s") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "返回报告列表")
	if !tt.HasText("历史任务") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "模拟场景")
	click(t, tt, "任务 · 运行中")
	click(t, tt, "查看实时详情")
	if !tt.HasText("题目进度表") || !tt.HasText("preview-case-001") {
		t.Fatal(tt.Texts())
	}
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
		{"任务 · 准备", "准备环境"},
		{"任务 · 失败", "启动服务"},
		{"模型 · 可用组合", "刷新服务列表"},
		{"下载 · 进行中与失败", "下载引擎"},
		{"下载 · 进行中与失败", "下载模型"},
		{"下载 · 进行中与失败", "取消下载"},
		{"下载 · 进行中与失败", "重新创建下载任务"},
		{"设备 · 含缺测", "刷新设备"},
		{"设置 · 路径与目录名", "保存"},
		{"设置 · 路径与目录名", "保存下载目录名"},
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

func TestPreviewRunAndStopActions(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(1)
	s := p.state
	s.task.stage, s.tasks = "", nil
	s.busy = false
	p.cancelEvents("task")
	s.progress.Bundles[0].NextCommand = previewValue("inferyard run --config /preview/config.toml")
	tt := previewTester(p, 1120, 2000)
	click(t, tt, "开始运行")
	if !s.busy || s.taskCancel != nil {
		t.Fatal("run action did not schedule memory-only work")
	}
	p.advance(20 * time.Second)
	tt.Frame()
	click(t, tt, "引擎 · 运行中")
	click(t, tt, "停止引擎")
	if s.engineState.State != "stopping" {
		t.Fatal("stop action did not transition")
	}
	p.advance(time.Second)
	if s.engineState.State != "stopped" {
		t.Fatal("stop did not complete")
	}
	p.loadScene(1)
	tt.Frame()
	click(t, tt, "取消运行")
	if !p.state.newTaskPending {
		t.Fatal("cancel did not enter draining")
	}
	p.advance(time.Second)
	if p.state.running() != nil || p.state.service != nil || p.state.task.stage != "prepare_candidate" {
		t.Fatal("cancel did not return to a fresh fixture")
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

func TestPreviewLayout(t *testing.T) {
	for _, width := range []int{800, 1120} {
		for _, scale := range []float32{1, 2} {
			for _, dark := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%.0f/%t", width, scale, dark), func(t *testing.T) {
					p := newPreviewSession(context.Background())
					p.loadScene(1)
					tt := previewTester(p, width, 600)
					tt.SetDark(dark)
					tt.SetPreferences(ui.Preferences{TextScale: scale, ReduceMotion: true})
					tt.Scroll(100, 450, 0, 2000)
					settings, _ := tt.Find("设置")
					debug, _ := tt.Find("UI 调试")
					if settings.Y+settings.H > debug.Y {
						t.Error("debug controls overlap navigation")
					}
					for _, label := range []string{"模拟场景", "重置场景"} {
						bounds, ok := tt.Find(label)
						if !ok || bounds.Y < 0 || bounds.Y+bounds.H > 600 {
							t.Errorf("preview control cannot be reached at this size: %s %+v", label, bounds)
						}
					}
					designNodes(tt, func(n reflect.Value, table bool) {
						if table {
							return
						}
						x, w := n.FieldByName("x").Float(), n.FieldByName("w").Float()
						if w > 0 && (x < -.1 || x+w > float64(width)+.1) {
							t.Errorf("overflow %s: %.1f %.1f", n.FieldByName("text").String(), x, w)
						}
					})
					if tt.HasText("UI 调试") && strings.Contains(strings.Join(tt.Texts(), " "), "42 / 120 题") {
						return
					}
					t.Fatal("preview lost scenario or controls", tt.Texts())
				})
			}
		}
	}
}
