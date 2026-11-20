package pages

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestDelayedVisibleDelaysAndHoldsMinimumDuration(t *testing.T) {
	active := true
	visible := false
	tt := ui.NewTester(func(c *ui.Context) {
		host := ui.Column(c.Key("delay-host"))
		visible = delayedVisible(c, host, "op", active)
		host.Children(func() {
			if visible {
				ui.Text(c, "loading-indicator")
			}
		})
	}, 400, 200)

	if visible {
		t.Fatal("indicator appeared immediately at 0ms")
	}
	tt.Advance(200 * time.Millisecond)
	if visible {
		t.Fatal("indicator appeared before 300ms")
	}
	tt.Advance(150 * time.Millisecond)
	if !visible {
		t.Fatal("indicator did not appear after 300ms")
	}
	// Work finishes 100ms after indicator appeared: must hold for at least 500ms.
	tt.Advance(100 * time.Millisecond)
	active = false
	tt.Frame()
	if !visible {
		t.Fatal("indicator disappeared before 500ms minimum")
	}
	tt.Advance(450 * time.Millisecond)
	if visible {
		t.Fatal("indicator stayed visible after 500ms minimum")
	}
}

func TestSpeedResultReadingHidesFalseZeroSummaryAndDelaysSkeleton(t *testing.T) {
	s := testState()
	task := &speedTask{id: "speed-1", model: "Preview-Model-Q4.gguf", engine: "preview-engine", stage: speedDone}
	s.speed = &speedWorkspace{opened: task, reading: true}
	tt := ui.NewTester(s.speedResultView, 1120, 1200)

	if tt.HasText("有效 0/5") || tt.HasText("测量结果不满足完整汇总条件") {
		t.Fatalf("reading state showed misleading 0/5 warning: %v", tt.Texts())
	}
	if tt.HasText("测量结果") {
		t.Fatalf("skeleton appeared before 300ms delay: %v", tt.Texts())
	}
	tt.Advance(350 * time.Millisecond)
	if !tt.HasText("测量结果") {
		t.Fatalf("skeleton card did not appear after 300ms: %v", tt.Texts())
	}
}

func TestDetailLoadingKeepsHeaderAndDelaysSkeleton(t *testing.T) {
	s := testState()
	s.detailID = "task-1"
	tt := ui.NewTester(s.detailView, 1120, 1400)
	if !tt.HasText("运行详情") || !tt.HasText("← 返回任务") {
		t.Fatalf("header missing during detail load: %v", tt.Texts())
	}
	if tt.HasText("任务概要") {
		t.Fatalf("detail skeleton appeared before 300ms: %v", tt.Texts())
	}
	tt.Advance(350 * time.Millisecond)
	if !tt.HasText("任务概要") || !tt.HasText("实时进度") || !tt.HasText("资源曲线") {
		t.Fatalf("detail skeleton cards missing after 300ms: %v", tt.Texts())
	}
}

func TestFormatRemainingDurationAndETA(t *testing.T) {
	if got := formatRemainingDuration(500 * time.Millisecond); got != "不足 1 秒" {
		t.Fatalf("sub-second = %q", got)
	}
	if got := formatRemainingDuration(18 * time.Second); got != "18 秒" {
		t.Fatalf("seconds = %q", got)
	}
	if got := formatRemainingDuration(2*time.Minute + 15*time.Second); got != "2 分 15 秒" {
		t.Fatalf("minutes = %q", got)
	}
	if got := formatRemainingDuration(time.Hour + 12*time.Minute); got != "1 小时 12 分" {
		t.Fatalf("hours = %q", got)
	}

	done := int64(0)
	eta := ""
	tt := ui.NewTester(func(c *ui.Context) {
		host := ui.Row(c.Key("eta-host"))
		eta = formatETA(c, host, "job-1", done, 6)
		host.Children(func() { ui.Text(c, eta) })
	}, 400, 120)
	if eta != "估算中…" {
		t.Fatalf("initial eta = %q, want 估算中…", eta)
	}
	tt.Advance(2 * time.Second)
	done = 2
	tt.Frame()
	if eta != "约剩余 4 秒" {
		t.Fatalf("eta after 2s for 2/6 = %q, want 约剩余 4 秒", eta)
	}
}
