package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// preparingDownloadsState is the downloads page while the workspace still has
// to prepare its assets, with one active engine download.
func preparingDownloadsState() *viewState {
	s := testState()
	s.progress.NextAction = "prepare_candidate"
	s.downloads = []core.DownloadTask{{TaskId: "engine", Kind: "engine", OutName: "engine-fixture", State: "downloading"}}
	return &s
}

func downloadsOrder(t *testing.T, tt *ui.Tester) (tasks, create float32) {
	t.Helper()
	taskBox, ok := tt.Find("下载任务")
	if !ok {
		t.Fatalf("download task section missing: %v", tt.Texts())
	}
	createBox, ok := tt.Find("新建下载任务")
	if !ok {
		t.Fatalf("new download section missing: %v", tt.Texts())
	}
	return taskBox.Y, createBox.Y
}

// TestDownloadsTasksStayAboveTheForm pins the section order: as soon as any
// download task exists, its card stays above the new-download form whether or
// not the workspace is still preparing assets.
func TestDownloadsTasksStayAboveTheForm(t *testing.T) {
	s := preparingDownloadsState()
	tt := ui.NewTester(s.downloadsPageView, 1120, 2000)
	if tasks, create := downloadsOrder(t, tt); tasks >= create {
		t.Fatalf("task section is not pinned above the form: %v >= %v", tasks, create)
	}
	if !s.preparingTask() {
		t.Fatal("fixture no longer prepares assets")
	}
}

// TestDownloadGuideFollowsTaskCards keeps the top guide from announcing ready
// assets while a download is still active or has failed.
func TestDownloadGuideFollowsTaskCards(t *testing.T) {
	s := preparingDownloadsState()
	if got := s.downloadGuideStatus(); got != "正在下载 1 项资源…" {
		t.Fatal(got)
	}
	s.downloads = append(s.downloads, core.DownloadTask{TaskId: "model", Kind: "model", OutName: "model-fixture", State: "failed"})
	if got := s.downloadGuideStatus(); got != "正在下载 1 项资源，另有 1 项下载失败。" {
		t.Fatal(got)
	}
	s.downloads[0].State = "done"
	if got := s.downloadGuideStatus(); got != "有 1 项下载任务失败，可在下方重新创建。" {
		t.Fatal(got)
	}
	s.downloads = nil
	s.registry.Engines = []core.EngineAsset{{Name: "llama"}}
	s.registry.Models = []core.ModelAsset{{Name: "model", Available: true}}
	if got := s.downloadGuideStatus(); got != "模型与引擎已就绪，可返回任务页开始测评。" {
		t.Fatal(got)
	}
}

// TestDownloadGuideBannerKeepsTheWayBack checks the banner still returns to the
// task page and no longer presents a second filled primary action.
func TestDownloadGuideBannerKeepsTheWayBack(t *testing.T) {
	s := preparingDownloadsState()
	tt := ui.NewTester(s.downloadsPageView, 1120, 2000)
	if !tt.HasText("正在下载 1 项资源…") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "返回任务")
	if s.selected != "dashboard" {
		t.Fatal(s.selected)
	}
}
