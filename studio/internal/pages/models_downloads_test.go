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
			for _, want := range []string{task.OutName, "已观测", "速率", "总量", "总量未知", "取消下载"} {
				if !tt.HasText(want) {
					t.Fatal(want, tt.Texts())
				}
			}
			if task.Kind == "engine" {
				if !tt.HasText("归档获取接口未提供字节读数。") || tt.HasText("已下载 0 bytes") {
					t.Fatal(tt.Texts())
				}
			} else if !tt.HasText("1.50 MiB") || !tt.HasText("2.0 KiB/s") {
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
