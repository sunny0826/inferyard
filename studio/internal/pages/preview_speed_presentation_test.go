//go:build ui_debug && !release

package pages

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestPreviewSpeedDefaultTableFits(t *testing.T) {
	for _, width := range []int{1024, 1120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			p, tt := prepareSpeed(t, "128k")
			click(t, tt, "开始专项测评")
			p.advance(10 * time.Second)
			tt.Frame()
			click(t, tt, "查看结果")
			tt = speedTester(p, width, 800)
			nodes := modelUINodes(tt, "label", "专项六项指标")
			if len(nodes) != 1 {
				t.Fatal("missing result table", len(nodes))
			}
			table := nodes[0]
			viewport := table.FieldByName("parent").Elem()
			right := viewport.FieldByName("x").Float() + viewport.FieldByName("w").Float()
			tableRight := table.FieldByName("x").Float() + table.FieldByName("w").Float()
			if tableRight > right+.1 {
				t.Fatalf("window clips table: right %.1f > viewport %.1f", tableRight, right)
			}
			for _, label := range []string{"模型 / 引擎", "Context", "Prompt length (tokens)", "Time (s)", "Prefill (tok/s)", "Decode (tok/s)"} {
				header, ok := tt.Find(label)
				if !ok || float64(header.X+header.W) > right+.1 {
					t.Errorf("header %q is clipped: %+v, right %.1f", label, header, right)
				}
			}
			saveSpeedImage(t, tt, fmt.Sprintf("default-result-%d", width))
		})
	}
}

func TestPreviewSpeedNarrowTableCanReachDecode(t *testing.T) {
	p, tt := prepareSpeed(t, "128k")
	click(t, tt, "开始专项测评")
	p.advance(10 * time.Second)
	p.state.openSpeed(p.state.speed.current.id)
	tt = speedTester(p, 800, 1200)
	tt.SetPreferences(ui.Preferences{TextScale: 2, ReduceMotion: true})
	bounds, ok := tt.Find("专项六项指标")
	if !ok {
		t.Fatal("missing table")
	}
	tt.Scroll(bounds.X+40, bounds.Y+20, 10000, 0)
	header, ok := tt.Find("Decode (tok/s)")
	if !ok || header.X < bounds.X || header.X+header.W > bounds.X+bounds.W+.1 {
		t.Fatal("last column is not reachable by scrolling", header, bounds)
	}
	saveSpeedImage(t, tt, "narrow-scrolled-result")
}

func TestPreviewSpeedCancelledDiagnosticsAreCollapsed(t *testing.T) {
	p, tt := prepareSpeed(t, "64k")
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	click(t, tt, "开始专项测评")
	p.advance(200 * time.Millisecond)
	tt.Frame()
	click(t, tt, "取消专项测评")
	p.advance(time.Second)
	tt.Frame()
	click(t, tt, "查看结果")
	if !tt.HasText("任务已取消。已完成的测量已保留；未完成项显示为 —。") {
		t.Fatal("missing readable cancellation notice", tt.Texts())
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, "not_executed") || strings.Contains(text, "five_valid_samples_required") {
			t.Fatal("raw diagnostics expanded by default", text)
		}
	}
	saveSpeedImage(t, tt, "cancelled-collapsed")
	click(t, tt, "查看诊断详情")
	if !tt.HasText("第 1 次缺测：Context: not_executed；Prompt length: not_executed；Time: not_executed；Prefill: not_executed；Decode: not_executed") {
		t.Fatal("original diagnostic detail was lost", tt.Texts())
	}
	click(t, tt, "查看诊断详情")
	if tt.HasText("not_executed") {
		t.Fatal("diagnostics did not collapse")
	}
}
