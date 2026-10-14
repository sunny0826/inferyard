package pages

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func TestTaskTableActions(t *testing.T) {
	for _, action := range []string{"button", "double", "enter"} {
		t.Run(action, func(t *testing.T) {
			s := testState()
			s.tasks = []core.RunTask{{TaskId: "task-a", Bundle: ptr("bundle-a")}, {TaskId: "task-b", Bundle: ptr("bundle-b")}}
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Children(func() {
					if s.detailID != "" {
						ui.Text(c, "详情："+s.detailID)
					} else {
						s.tasksView(c)
					}
				})
			}, 1000, 600)
			click(t, tt, "查看详情")
			if s.detailID != "" {
				t.Fatal("unselected action activated")
			}
			click(t, tt, "bundle-b")
			switch action {
			case "button":
				click(t, tt, "查看详情")
			case "double":
				click(t, tt, "bundle-b")
			case "enter":
				tt.Key(0, ui.KeyEnter)
			}
			if !tt.HasText("详情：task-b") {
				t.Fatal(tt.Texts())
			}
			s.detailID = ""
			tt.Frame()
			tt.Frame()
			if !tt.HasText("task-b") {
				t.Fatal("selection lost after returning", tt.Texts())
			}
		})
	}
}

func TestHistoryTableResumeAndDetails(t *testing.T) {
	s := testState()
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "standalone", Path: "/solo"}}
	s.history.Batches = []core.BatchSummary{{BatchId: "batch", Path: "/batch", Runs: []core.RunSummary{{RunId: "eligible", Path: "/eligible", BatchId: ptr("batch"), Resumable: true, Relation: ptr("resume"), ParentRunId: ptr("parent"), StartedAt: ptr("start"), FinishedAt: ptr("finish"), StopReason: ptr("stop"), Completeness: ptr("partial")}}}}
	s.tasks = []core.RunTask{{TaskId: "live", RunPath: ptr("/eligible")}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.historyView(c) }) }, 1500, 900)
	click(t, tt, "standalone")
	click(t, tt, "续测")
	if s.resumeSource != nil || !tt.HasText("独立运行不支持续测") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "eligible")
	for _, text := range []string{"/eligible", "/batch", "partial", "stop", "parent", "start", "finish", "实时详情"} {
		if !tt.HasText(text) {
			t.Fatal("missing", text, tt.Texts())
		}
	}
	click(t, tt, "续测")
	if s.resumeSource == nil || s.resumeSource.Path != "/eligible" {
		t.Fatal("wrong resume source")
	}
	click(t, tt, "实时详情")
	if s.detailID != "live" {
		t.Fatal("wrong live task")
	}
	s.resumeSource = nil
	s.denied["/eligible"] = true
	tt.Frame()
	click(t, tt, "续测")
	if s.resumeSource != nil || !tt.HasText("not_resumable") {
		t.Fatal("denied source accepted")
	}
}

func TestReportMultiSelectionAndLimit(t *testing.T) {
	s := testState()
	s.selected = "reports"
	s.progress = nil
	for i := 0; i < 101; i++ {
		s.history.StandaloneRuns = append(s.history.StandaloneRuns, core.RunSummary{RunId: fmt.Sprintf("run-%03d", i), Path: fmt.Sprintf("/runs/%03d", i)})
	}
	tt := ui.NewTester(s.view, 1120, 1200)
	click(t, tt, "生成报告")
	if s.busy {
		t.Fatal("empty selection admitted")
	}
	click(t, tt, "run-000")
	if !tt.HasText("已选 1 / 100") {
		t.Fatal(tt.Texts())
	}
	if err := tt.ClickWith(ui.Cmd, "run-001"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("已选 2 / 100") {
		t.Fatal(tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	if !tt.HasText("已选 101 / 100") || !tt.HasText("请选择 1–100") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "生成报告")
	if s.busy {
		t.Fatal("over limit admitted")
	}
	// Runs gone from history drop out of the persistent selection.
	s.history.StandaloneRuns = s.history.StandaloneRuns[:100]
	tt.Frame()
	if !tt.HasText("已选 100 / 100") {
		t.Fatal(tt.Texts())
	}
	s.selected = "models"
	tt.Frame()
	s.selected = "reports"
	tt.Frame()
	if !tt.HasText("已选 100 / 100") {
		t.Fatal("page switch lost choices")
	}
	click(t, tt, "run-000")
	if err := tt.ClickWith(ui.Cmd, "run-000"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("已选 0 / 100") {
		t.Fatal(tt.Texts())
	}
}

func TestReportTableSelection(t *testing.T) {
	s := testState()
	s.reportWindows = NewReports(&core.Service{})
	s.reports = []core.ReportSummary{{ReportId: "report-a", Path: "/reports/a", RunIds: []string{"source-a"}, RunPaths: []*string{nil}}, {ReportId: "report-b", Path: "/reports/b", RunIds: []string{"source-b"}, RunPaths: []*string{ptr("/runs/b")}}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsTableView(c) }) }, 1000, 700)
	click(t, tt, "打开报告")
	if s.failure != nil {
		t.Fatal("unselected open activated")
	}
	click(t, tt, "report-a")
	if !tt.HasText("来源不可达") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "report-b")
	if !tt.HasText("source-b · /runs/b") || tt.HasText("来源不可达") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "打开报告")
	if s.failure == nil || s.failure.Code != "not_found" {
		t.Fatal("selected open not invoked")
	}
}

func TestDownloadTaskCardsAndDetails(t *testing.T) {
	s := testState()
	a := core.DownloadTask{TaskId: "a", OutName: "model-a", State: "downloading", OutDir: "/out/a"}
	b := core.DownloadTask{TaskId: "b", OutName: "model-b", State: "failed", OutDir: "/out/b", FileName: ptr("model.gguf"), DownloadedBytes: 1048576, Result: &core.DownloadResult{Bytes: 1048576, Sha256: "fixture-sha"}, Error: &core.ApiError{Code: "fixture", Message: "下载失败"}}
	s.downloads = []core.DownloadTask{a, b}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 900)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if !tt.HasText("取消下载") {
		t.Fatal("downloading task lacks cancel", tt.Texts())
	}
	s.downloads = []core.DownloadTask{b}
	tt.Frame()
	for _, text := range []string{"/out/b", "model.gguf", "1048576", "fixture-sha", "下载失败", "输出目录保留"} {
		if !tt.HasText(text) {
			t.Fatal("missing", text, tt.Texts())
		}
	}
	if tt.HasText("取消下载") {
		t.Fatal("failed download has cancel")
	}
	if dir := os.Getenv("STUDIO_UI_PREVIEW_DIR"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "downloads.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDownloadTaskCardsRenderOnReorder(t *testing.T) {
	s := testState()
	a := core.DownloadTask{TaskId: "a", OutName: "model-a", State: "downloading", OutDir: "/out/a"}
	b := core.DownloadTask{TaskId: "b", OutName: "model-b", State: "failed", OutDir: "/out/b"}
	s.downloads = []core.DownloadTask{a, b}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 900)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	for _, want := range []string{"输出目录：/out/a", "输出目录：/out/b"} {
		if !tt.HasText(want) {
			t.Fatal("missing", want, tt.Texts())
		}
	}
	first, _ := tt.Find("model-a")
	second, _ := tt.Find("model-b")
	if first.Y >= second.Y {
		t.Fatal("order", first, second)
	}
	s.downloads = []core.DownloadTask{b, a}
	tt.Frame()
	for _, want := range []string{"输出目录：/out/a", "输出目录：/out/b"} {
		if !tt.HasText(want) {
			t.Fatal("missing after reorder", want, tt.Texts())
		}
	}
	first, _ = tt.Find("model-b")
	second, _ = tt.Find("model-a")
	if first.Y >= second.Y {
		t.Fatal("reordered", first, second)
	}
	if dir := os.Getenv("STUDIO_UI_PREVIEW_DIR"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "downloads-reordered.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoryResumePanelKeepsTableSelection(t *testing.T) {
	s := testState()
	s.selected = "runs"
	s.history.Batches = []core.BatchSummary{{BatchId: "batch", Path: "/batch", Runs: []core.RunSummary{{RunId: "selected-run", Path: "/selected", BatchId: ptr("batch"), Resumable: true}}}}
	tt := ui.NewTester(s.view, 1600, 2600)
	click(t, tt, "selected-run")
	click(t, tt, "续测")
	tt.Frame()
	if !tt.HasText("确认续测") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "关闭续测设置")
	tt.Frame()
	if !tt.HasText("/selected") || tt.HasText("确认续测") {
		t.Fatal("table selection lost when panel disappeared", tt.Texts())
	}
	s.selected = "models"
	tt.Frame()
	s.selected = "runs"
	tt.Frame()
	if !tt.HasText("/selected") {
		t.Fatal("table selection lost after page change")
	}
}
