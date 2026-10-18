//go:build ui_debug && !release

package pages

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func prepareSpeed(t *testing.T, tier string) (*previewSession, *ui.Tester) {
	t.Helper()
	p, _ := preparePreview(t)
	tt := speedTester(p, 1120, 2200)
	click(t, tt, "测评类型")
	if err := tt.ChooseMenuItem("长上下文速度"); err != nil {
		t.Fatal(err)
	}
	if tier != "64K · 65,536 tokens" {
		click(t, tt, "上下文档位")
		if err := tt.ChooseMenuItem(tier); err != nil {
			t.Fatal(err)
		}
	}
	return p, tt
}

func speedTester(p *previewSession, width, height int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.shellOptions().Theme(c))
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(205).Shrink(0).Children(func() { p.left(c) })
			p.content(c)
		})
	}, width, height)
}

func TestPreviewSpeedCompleteAndReopen(t *testing.T) {
	for _, tc := range []struct {
		label           string
		context, tokens int
	}{
		{"64K · 65,536 tokens", 65536, 52625}, {"128K · 131,072 tokens", 131072, 106294}, {"256K · 262,144 tokens", 262144, 213612},
	} {
		t.Run(tc.label, func(t *testing.T) {
			p, tt := prepareSpeed(t, tc.label)
			click(t, tt, "开始专项测评")
			r := p.state.speed.current
			if r.context != tc.context || !p.state.busy {
				t.Fatal("selection was not frozen")
			}
			p.advance(1500 * time.Millisecond)
			tt.Frame()
			if !tt.HasText("取消专项测评") || tt.HasText("停止") {
				t.Fatal("active measurement controls are inconsistent", tt.Texts())
			}
			p.advance(10 * time.Second)
			tt.Frame()
			click(t, tt, "查看结果")
			for _, text := range []string{"Card", "Context", "Prompt length (tokens)", "Time (s)", "Prefill (tok/s)", "Decode (tok/s)", "有效 5/5 · 中位数", fmt.Sprint(tc.tokens)} {
				if !tt.HasText(text) {
					t.Fatal("missing result", text, tt.Texts())
				}
			}
			if tt.HasText("各类任务表现") || len(p.state.reports) != 0 || p.state.task.report != "" || len(p.state.historyRuns()) != 0 {
				t.Fatal("specialist generated a generic report or grade")
			}
			click(t, tt, "查看逐次测量")
			if !tt.HasText("按第 1 至第 5 次测量顺序排列。") || len(r.samples) != 5 {
				t.Fatal("samples unavailable")
			}
			click(t, tt, "返回历史任务")
			click(t, tt, r.id)
			click(t, tt, "打开专项结果")
			if p.state.speed.opened != r {
				t.Fatal("history opened another task")
			}
			click(t, tt, "用此组合新开任务")
			if p.state.speed.context != tc.context || !p.state.speed.selected || len(p.state.speed.history) != 1 {
				t.Fatal("new task discarded history or selection")
			}
			click(t, tt, "停止当前服务")
			p.advance(time.Second)
			tt.Frame()
			click(t, tt, "开始专项测评")
			p.advance(10 * time.Second)
			if p.state.speed.current.id == r.id || len(p.state.speed.history) != 2 || r.summary == nil {
				t.Fatal("rerun overwrote original")
			}
			if p.state.service != nil || p.state.taskCancel != nil {
				t.Fatal("simulation reached a real service")
			}
		})
	}
}

func TestPreviewSpeedCancellationAndReset(t *testing.T) {
	for _, elapsed := range []time.Duration{200 * time.Millisecond, 700 * time.Millisecond, 2800 * time.Millisecond, 6700 * time.Millisecond} {
		t.Run(elapsed.String(), func(t *testing.T) {
			p, tt := prepareSpeed(t, "64K · 65,536 tokens")
			click(t, tt, "开始专项测评")
			p.advance(elapsed)
			tt.Frame()
			r := p.state.speed.current
			count := len(r.samples)
			click(t, tt, "取消专项测评")
			if r.stage != speedCancelling || !p.state.busy {
				t.Fatal("cancellation skipped draining")
			}
			click(t, tt, "新任务")
			p.advance(20 * time.Second)
			tt.Frame()
			if r.stage != speedCancelled || len(r.samples) != count || r.summary != nil || len(p.state.speed.history) != 1 || p.state.speed.current != nil || p.state.busy || p.state.newTaskPending {
				t.Fatal("cancel/new task did not converge")
			}
			click(t, tt, "报告")
			click(t, tt, r.id)
			click(t, tt, "打开专项结果")
			if !tt.HasText(fmt.Sprintf("有效 %d/5 · 未形成完整汇总", count)) {
				t.Fatal("partial result missing", tt.Texts())
			}
			click(t, tt, "重置场景")
			p.advance(time.Minute)
			if len(p.state.speed.history) != 0 || p.state.speed.current != nil {
				t.Fatal("reset kept stale specialist work")
			}
		})
	}
}

func TestPreviewSpeedMissingFailureAndReadRetry(t *testing.T) {
	p, tt := prepareSpeed(t, "128K · 131,072 tokens")
	click(t, tt, "下次操作模拟失败")
	click(t, tt, "下次专项模拟缺测")
	click(t, tt, "开始专项测评")
	p.advance(10 * time.Second)
	tt.Frame()
	failed := p.state.speed.current
	if failed.stage != speedFailed || len(failed.samples) != 0 {
		t.Fatal("startup failure fabricated samples")
	}
	click(t, tt, "用此组合新开任务")
	click(t, tt, "开始专项测评")
	p.advance(10 * time.Second)
	tt.Frame()
	r := p.state.speed.current
	click(t, tt, "下次操作模拟失败")
	click(t, tt, "查看结果")
	if !tt.HasText("模拟结果读取失败；重新读取不会再次运行模型。") || tt.HasText("专项已完成测量") {
		t.Fatal("read failure displayed stale results")
	}
	click(t, tt, "重新读取结果")
	if !tt.HasText("有效 4/5 · 未形成完整汇总") || !tt.HasText("第 5 次：引擎阶段计时缺失（模拟）；速度与耗时缺测。") || !tt.HasText("—") {
		t.Fatal("missing timing was presented as a complete result", tt.Texts())
	}
	if len(p.state.speed.history) != 2 || r.summary != nil || p.state.busy || len(p.jobs) != 0 {
		t.Fatal("read retry reran measurements")
	}
	if p.state.service != nil || p.state.taskCancel != nil || len(p.state.reports) != 0 {
		t.Fatal("preview isolation failed")
	}
}

func TestPreviewSpeedStopGuard(t *testing.T) {
	p, tt := prepareSpeed(t, "64K · 65,536 tokens")
	if tt.HasText("工作区进度") {
		t.Fatal("specialist setup displayed ordinary bundle progress")
	}
	click(t, tt, "开始专项测评")
	p.advance(3 * time.Second)
	before := p.state.engineState
	p.failNext = true
	p.dispatch(previewStopEngine)
	p.advance(time.Second)
	if p.state.engineState != before || p.state.failure == nil || p.state.failure.Code != "run_active" || !p.failNext {
		t.Fatal("stop guard interrupted specialist or consumed failure")
	}
	p.advance(10 * time.Second)
	if p.state.speed.current.stage != speedDone || len(p.state.speed.history) != 1 {
		t.Fatal("rejected stop damaged measurement completion")
	}
}

func TestPreviewSpeedInvalidSubmissionKeepsFailureAndCloseStopsEvents(t *testing.T) {
	p, _ := prepareSpeed(t, "64K · 65,536 tokens")
	p.failNext = true
	p.dispatch(previewSpeedStart{model: p.state.model, engine: p.state.engine, context: 4096})
	if !p.failNext || p.state.speed.current != nil || p.state.busy {
		t.Fatal("invalid request consumed failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	p = newPreviewSession(ctx)
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	p.dispatch(previewSpeedStart{model: p.state.model, engine: p.state.engine, context: 65536})
	cancel()
	p.advance(time.Minute)
	if p.state.speed.current.stage != speedPreparing || len(p.state.speed.history) != 0 {
		t.Fatal("closed window advanced work")
	}
}

func TestPreviewSpeedLayout(t *testing.T) {
	for _, width := range []int{800, 1120, 1440} {
		for _, scale := range []float32{1, 2} {
			for _, dark := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%.0f/%t", width, scale, dark), func(t *testing.T) {
					p, _ := prepareSpeed(t, "64K · 65,536 tokens")
					p.dispatch(previewSpeedStart{model: p.state.model, engine: p.state.engine, context: 65536})
					p.advance(10 * time.Second)
					p.state.openSpeed(p.state.speed.current.id)
					tt := speedTester(p, width, 800)
					tt.SetDark(dark)
					tt.SetPreferences(ui.Preferences{TextScale: scale, ReduceMotion: true})
					designNodes(tt, func(n reflect.Value, table bool) {
						if table {
							return
						}
						x, w := n.FieldByName("x").Float(), n.FieldByName("w").Float()
						if w > 0 && (x < -.1 || x+w > float64(width)+.1) {
							t.Errorf("overflow %s %.1f %.1f", n.FieldByName("text").String(), x, w)
						}
					})
					if !tt.HasText("长上下文速度 · 结果") {
						t.Fatal("result vanished")
					}
				})
			}
		}
	}
}
