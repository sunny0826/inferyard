package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestModelsUnifiedDownloadMeasurements(t *testing.T) {
	for _, task := range []core.DownloadTask{
		{TaskId: "engine", Kind: "engine", OutName: "engine-fixture", State: "downloading", ProgressReason: ptr("engine_transfer_progress_unavailable")},
		{TaskId: "model", Kind: "model", OutName: "model-fixture", State: "downloading", DownloadedBytes: 1572864, BytesPerSecond: ptr(2048.0)},
	} {
		t.Run(task.Kind, func(t *testing.T) {
			s := testState()
			s.downloads = []core.DownloadTask{task}
			tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 1200)
			for _, want := range []string{task.OutName, "取消下载"} {
				if !tt.HasText(want) {
					t.Fatal(want, tt.Texts())
				}
			}
			if task.Kind == "engine" {
				if !tt.HasText("正在下载引擎归档…") || tt.HasText("缺测") {
					t.Fatal(tt.Texts())
				}
				if tt.HasText("已下载 0 bytes") {
					t.Fatal(tt.Texts())
				}
				if got := exactTextCount(tt.Texts(), "下载中"); got != 1 {
					t.Fatalf("activity indicators = %d: %v", got, tt.Texts())
				}
				if got := exactTextCount(tt.Texts(), "下载任务活动"); got != 1 {
					t.Fatalf("engine activity bars = %d: %v", got, tt.Texts())
				}
			} else if !tt.HasText("已下载") || !tt.HasText("速率") || !tt.HasText("总量") || !tt.HasText("—") ||
				!tt.HasText("1.50 MiB") || !tt.HasText("2.0 KiB/s") {
				t.Fatal(tt.Texts())
			}
			if !keyboardReaches(tt, "取消下载", 16) {
				t.Fatal("cancel is not keyboard accessible")
			}
			for _, state := range []string{"failed", "cancelled", "done"} {
				s.downloads[0].State = state
				tt.Frame()
				if _, ok := tt.Find("下载任务活动"); ok {
					t.Fatal("terminal activity progress")
				}
				if tt.HasText("取消下载") {
					t.Fatal("terminal cancel")
				}
				if task.Kind == "engine" {
					want := map[string]string{
						"done":      "引擎归档下载完成。",
						"failed":    "引擎归档下载失败。",
						"cancelled": "引擎归档下载已取消。",
					}[state]
					if !tt.HasText(want) {
						t.Fatal(want, tt.Texts())
					}
				}
				if state == "failed" && !tt.HasText("重新创建下载任务") {
					t.Fatal("failed task must offer recreate", tt.Texts())
				}
				if state != "failed" && tt.HasText("重新创建下载任务") {
					t.Fatal("recreate is only for failed tasks", tt.Texts())
				}
			}
			s.downloads[0].State = task.State
			tt.Frame()
			click(t, tt, "任务详情")
			assertDownloadDetails(t, tt, task.Kind)
		})
	}
}

func activityBarSegments(tt *ui.Tester, label string, accent ui.Color) (rect ui.Rect, start, end int) {
	r, ok := tt.Find(label)
	if !ok {
		panic("missing activity bar: " + label)
	}
	img := tt.Image()
	y := int(r.Y + r.H/2)
	start, end = -1, -1
	for x := int(r.X); x < int(r.X+r.W); x++ {
		o := img.PixOffset(x, y)
		if img.Pix[o] == accent.R && img.Pix[o+1] == accent.G && img.Pix[o+2] == accent.B {
			if start < 0 {
				start = x - int(r.X)
			}
			end = x - int(r.X)
		}
	}
	return r, start, end
}

func TestEngineActivityBarStaysIndeterminate(t *testing.T) {
	s := testState()
	s.downloads = []core.DownloadTask{{TaskId: "engine", Kind: "engine", OutName: "engine-fixture", State: "downloading"}}
	var accent ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		accent = stateColor(c, stateActive)
		ui.Column(c).Children(func() { s.downloadsView(c) })
	}, 1000, 1200)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	rect, start, end := activityBarSegments(tt, "下载任务活动", accent)
	if start < 0 || end < 0 {
		t.Fatalf("activity bar drew no accent segment: %v", rect)
	}
	width := int(rect.W)
	leftGap, rightGap := start, width-1-end
	if leftGap < width/10 || rightGap < width/10 {
		t.Fatalf("bar is not indeterminate: left %d, right %d of %d", leftGap, rightGap, width)
	}
	if diff := leftGap - rightGap; diff < -2 || diff > 2 {
		t.Fatalf("indeterminate segment is not centered: left %d, right %d", leftGap, rightGap)
	}
	if filled := end - start + 1; float64(filled)/float64(width) < .25 || float64(filled)/float64(width) > .35 {
		t.Fatalf("indeterminate segment width = %d/%d", filled, width)
	}
}

func keyboardReaches(tt *ui.Tester, label string, limit int) bool {
	for i := 0; i < limit; i++ {
		tt.Key(0, ui.KeyTab)
		if tt.Focused(label) {
			return true
		}
	}
	return false
}

func exactTextCount(texts []string, s string) int {
	count := 0
	for _, text := range texts {
		if text == s {
			count++
		}
	}
	return count
}

func assertDownloadDetails(t *testing.T, tt *ui.Tester, kind string) {
	t.Helper()
	if kind == "engine" {
		if !tt.HasText("已下载 — · 速率 —") || tt.HasText("已下载 0 bytes") {
			t.Fatal(tt.Texts())
		}
		return
	}
	if !tt.HasText("速率 2,048 bytes/s") || !tt.HasText("已下载 1572864 bytes") {
		t.Fatal(tt.Texts())
	}
}

func TestDownloadCardDisplaysReadableNameProgressBarStagesAndETA(t *testing.T) {
	total := int64(104857600)
	s := testState()
	s.downloads = []core.DownloadTask{
		{
			TaskId:          "engine-1",
			Kind:            "engine",
			DisplayName:     "llama-server · macOS arm64",
			OutName:         "llama-prism-b10743",
			State:           "downloading",
			Stage:           "downloading",
			DownloadedBytes: 52428800,
			TotalBytes:      &total,
			BytesPerSecond:  ptr(10485760.0),
		},
		{
			TaskId:          "model-1",
			Kind:            "model",
			DisplayName:     "SmolLM2-135M-Instruct-Q4_K_M（GGUF）",
			OutName:         "SmolLM2-135M-Instruct-Q4_K_M",
			State:           "downloading",
			Stage:           "verifying",
			DownloadedBytes: total,
			TotalBytes:      &total,
		},
		{
			TaskId:          "model-cancelled",
			Kind:            "model",
			DisplayName:     "Qwen3-0.6B-Q8_0（GGUF）",
			OutName:         "Qwen3-0.6B-Q8_0",
			State:           "cancelled",
			DownloadedBytes: 1024,
			TotalBytes:      &total,
		},
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 1400)
	for _, want := range []string{
		"llama-server · macOS arm64",
		"50.00 MiB / 100.00 MiB",
		"50.0%",
		"10.00 MiB/s",
		"剩余 5s",
		"SmolLM2-135M-Instruct-Q4_K_M（GGUF）",
		"校验中",
		"Qwen3-0.6B-Q8_0（GGUF）",
		"继续下载",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	if _, ok := tt.Find("下载进度"); !ok {
		t.Fatalf("determinate progress bar missing: %v", tt.Texts())
	}
	if _, ok := tt.Find("下载任务活动"); ok {
		t.Fatalf("unexpected indeterminate bar when total_bytes is known: %v", tt.Texts())
	}
}
