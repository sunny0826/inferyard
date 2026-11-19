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
				// Engine archives report neither bytes nor rate: one concise
				// status line replaces the three empty metric columns.
				if !tt.HasText("正在下载引擎归档（来源未提供大小与速率）") || tt.HasText("总量未知") || tt.HasText("缺测") {
					t.Fatal(tt.Texts())
				}
				if tt.HasText("已下载 0 bytes") {
					t.Fatal(tt.Texts())
				}
				// One indeterminate progress bar carries the activity; the
				// spinner that repeated the same "下载中" label is gone.
				if got := exactTextCount(tt.Texts(), "下载中"); got != 1 {
					t.Fatalf("activity indicators = %d: %v", got, tt.Texts())
				}
			} else if !tt.HasText("已观测") || !tt.HasText("速率") || !tt.HasText("总量") || !tt.HasText("总量未知") ||
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
						"done":      "引擎归档下载完成（来源未提供大小与速率）",
						"failed":    "引擎归档下载失败。",
						"cancelled": "引擎归档下载已取消。",
					}[state]
					if !tt.HasText(want) || tt.HasText("总量未知") {
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
			click(t, tt, "观测依据与输出")
			assertDownloadDetails(t, tt, task.Kind)
		})
	}
}

// keyboardReaches tabs until the label holds focus or the limit is reached.
func keyboardReaches(tt *ui.Tester, label string, limit int) bool {
	for i := 0; i < limit; i++ {
		tt.Key(0, ui.KeyTab)
		if tt.Focused(label) {
			return true
		}
	}
	return false
}

// exactTextCount counts labels equal to s, so an assertion can tell the state
// badge apart from an extra activity indicator carrying the same word.
func exactTextCount(texts []string, s string) int {
	count := 0
	for _, text := range texts {
		if text == s {
			count++
		}
	}
	return count
}

// assertDownloadDetails checks the expanded per-kind measurement wording.
func assertDownloadDetails(t *testing.T, tt *ui.Tester, kind string) {
	t.Helper()
	if kind == "engine" {
		if !tt.HasText("已下载 缺测 · 速率 缺测") || tt.HasText("已下载 0 bytes") {
			t.Fatal(tt.Texts())
		}
		return
	}
	if !tt.HasText("速率 2,048 bytes/s") || !tt.HasText("已下载 1572864 bytes") {
		t.Fatal(tt.Texts())
	}
}
