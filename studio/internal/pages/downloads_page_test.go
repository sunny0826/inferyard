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

// TestDownloadGuideClearsFailureAfterRetrySucceeds keeps the historical failed
// card in the list but stops asking for a retry once a newer download for that
// source is running or done.
func TestDownloadGuideClearsFailureAfterRetrySucceeds(t *testing.T) {
	failed := core.DownloadTask{TaskId: "model-1", Kind: "model", OutName: "model", SourceUrl: "https://example.test/model.gguf", State: "failed", StartedAt: "2026-10-17T00:00:00Z"}
	s := preparingDownloadsState()
	s.downloads = []core.DownloadTask{failed}
	if got := s.downloadGuideStatus(); got != "有 1 项下载任务失败，可在下方重新创建。" {
		t.Fatalf("unresolved failure is hidden: %q", got)
	}

	retry := failed
	retry.TaskId, retry.OutName, retry.State = "model-2", "model-2", "downloading"
	retry.StartedAt = "2026-10-17T00:01:00Z"
	// Newest first, as Service.Downloads and the preview window return them.
	s.downloads = append([]core.DownloadTask{retry}, s.downloads...)
	if got := s.downloadGuideStatus(); got != "正在下载 1 项资源…" {
		t.Fatalf("running retry still counted as a failure: %q", got)
	}

	s.downloads[0].State = "done"
	if got := s.downloadGuideStatus(); got != "下载所需引擎与模型后返回任务页。" {
		t.Fatalf("retried-away failure still asks for a retry: %q", got)
	}
	if unresolved := s.unresolvedDownloadFailures(); unresolved != 0 {
		t.Fatalf("resolved failure still counts: %d", unresolved)
	}
	// The historical card stays in the list; only the retry prompt is gone.
	if len(s.downloads) != 2 {
		t.Fatal(s.downloads)
	}
}

// TestUnresolvedFailureSurvivesUnrelatedAssets keeps a never-retried failure in
// the banner even when another asset is available or another download runs.
func TestUnresolvedFailureSurvivesUnrelatedAssets(t *testing.T) {
	failed := core.DownloadTask{TaskId: "model-1", Kind: "model", OutName: "model", State: "failed", SourceUrl: "https://example.test/other.gguf", StartedAt: "2026-10-17T00:00:00Z"}
	active := core.DownloadTask{TaskId: "model-2", Kind: "model", State: "downloading", SourceUrl: "https://example.test/another.gguf", StartedAt: "2026-10-17T00:01:00Z"}
	s := preparingDownloadsState()
	s.downloads = append(s.downloads, active, failed)
	// Another model already exists and an unrelated model source is running.
	s.registry.Models = []core.ModelAsset{{Name: "existing", Available: true}}

	if got := s.unresolvedDownloadFailures(); got != 1 {
		t.Fatalf("unrelated asset/download hid the failure: %d", got)
	}
	if got := s.downloadGuideStatus(); got != "正在下载 2 项资源，另有 1 项下载失败。" {
		t.Fatalf("banner dropped the unresolved failure: %q", got)
	}

	// A newer same-source success resolves only that failure.
	resolved := core.DownloadTask{TaskId: "model-3", Kind: "model", State: "done", SourceUrl: failed.SourceUrl, StartedAt: "2026-10-17T00:02:00Z"}
	s.downloads = append([]core.DownloadTask{resolved}, s.downloads...)
	if got := s.unresolvedDownloadFailures(); got != 0 {
		t.Fatalf("same-source success did not resolve: %d", got)
	}
}

// TestBannerClearsRetryPromptAfterRetry renders the page so the banner text is
// what the operator reads: the retired failure card stays, its retry prompt
// does not.
func TestBannerClearsRetryPromptAfterRetry(t *testing.T) {
	failed := core.DownloadTask{TaskId: "model-1", Kind: "model", OutName: "model", SourceUrl: "https://example.test/model.gguf", State: "failed", StartedAt: "2026-10-17T00:00:00Z"}
	retry := failed
	retry.TaskId, retry.OutName, retry.StartedAt = "model-2", "model-2", "2026-10-17T00:01:00Z"
	s := preparingDownloadsState()
	s.downloads = []core.DownloadTask{retry, failed}

	retry.State = "downloading"
	s.downloads[0] = retry
	tt := ui.NewTester(s.downloadsPageView, 1120, 2400)
	if !tt.HasText("正在下载 1 项资源…") || tt.HasText("可在下方重新创建") {
		t.Fatal(tt.Texts())
	}

	retry.State = "done"
	s.downloads[0] = retry
	s.registry.Engines = []core.EngineAsset{{Name: "llama"}}
	s.registry.Models = []core.ModelAsset{{Name: "model", Available: true}}
	tt = ui.NewTester(s.downloadsPageView, 1120, 2400)
	if tt.HasText("可在下方重新创建") || !tt.HasText("可返回任务页开始测评") {
		t.Fatal(tt.Texts())
	}
	if !tt.HasText("重新创建下载任务") {
		t.Fatal("historical failed card left the list")
	}
}

// TestOlderSuccessDoesNotMaskNewerFailure keeps an older success from hiding a
// later failure of the same source: only a newer task resolves a failure.
func TestOlderSuccessDoesNotMaskNewerFailure(t *testing.T) {
	source := "https://example.test/same.gguf"
	done := core.DownloadTask{TaskId: "model-1", Kind: "model", OutName: "model", SourceUrl: source, State: "done", StartedAt: "2026-10-17T00:00:00Z"}
	failed := core.DownloadTask{TaskId: "model-2", Kind: "model", OutName: "model-2", SourceUrl: source, State: "failed", StartedAt: "2026-10-17T00:01:00Z"}

	for _, tc := range []struct {
		name      string
		downloads []core.DownloadTask
		want      string
	}{
		{
			name:      "standalone",
			downloads: []core.DownloadTask{failed, done},
			want:      "有 1 项下载任务失败，可在下方重新创建。",
		},
		{
			name: "alongside active engine",
			downloads: []core.DownloadTask{
				{TaskId: "engine", Kind: "engine", OutName: "engine", State: "downloading", StartedAt: "2026-10-17T00:02:00Z"},
				failed, done,
			},
			want: "正在下载 1 项资源，另有 1 项下载失败。",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testState()
			s.progress.NextAction = "prepare_candidate"
			s.downloads = tc.downloads
			if got := s.unresolvedDownloadFailures(); got != 1 {
				t.Fatalf("older success masked the newer failure: %d", got)
			}
			if got := s.downloadGuideStatus(); got != tc.want {
				t.Fatalf("banner = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNewerSuccessResolvesOlderFailure is the mirror case: a success that comes
// after the failure does retire its retry prompt.
func TestNewerSuccessResolvesOlderFailure(t *testing.T) {
	source := "https://example.test/same.gguf"
	failed := core.DownloadTask{TaskId: "model-1", Kind: "model", OutName: "model", SourceUrl: source, State: "failed", StartedAt: "2026-10-17T00:00:00Z"}
	done := core.DownloadTask{TaskId: "model-2", Kind: "model", OutName: "model-2", SourceUrl: source, State: "done", StartedAt: "2026-10-17T00:01:00Z"}
	s := testState()
	s.progress.NextAction = "prepare_candidate"
	s.downloads = []core.DownloadTask{done, failed}
	if got := s.unresolvedDownloadFailures(); got != 0 {
		t.Fatalf("newer success did not resolve the failure: %d", got)
	}
}

// TestDownloadOrderBeatsClockRollback pins the ordering source: list position,
// not StartedAt, decides which task is newer, so a clock step backward between
// downloads neither revives a resolved failure nor hides a later one.
func TestDownloadOrderBeatsClockRollback(t *testing.T) {
	source := "https://example.test/same.gguf"
	// Newest first: the later task carries an earlier wall-clock stamp.
	newerDone := core.DownloadTask{TaskId: "model-2", Kind: "model", OutName: "model-2", SourceUrl: source, State: "done", StartedAt: "2026-10-16T23:59:00Z"}
	olderFailed := core.DownloadTask{TaskId: "model-1", Kind: "model", OutName: "model", SourceUrl: source, State: "failed", StartedAt: "2026-10-17T00:01:00Z"}

	s := testState()
	s.progress.NextAction = "prepare_candidate"
	s.downloads = []core.DownloadTask{newerDone, olderFailed}
	if got := s.unresolvedDownloadFailures(); got != 0 {
		t.Fatalf("rolled-back success did not resolve the older failure: %d", got)
	}

	// Newest first again: the later task failed after the older success.
	newerFailed := core.DownloadTask{TaskId: "model-4", Kind: "model", OutName: "model-4", SourceUrl: source, State: "failed", StartedAt: "2026-10-16T22:00:00Z"}
	olderDone := core.DownloadTask{TaskId: "model-3", Kind: "model", OutName: "model-3", SourceUrl: source, State: "done", StartedAt: "2026-10-17T00:05:00Z"}
	s.downloads = []core.DownloadTask{newerFailed, olderDone}
	if got := s.unresolvedDownloadFailures(); got != 1 {
		t.Fatalf("rolled-back older success masked the newer failure: %d", got)
	}
	if got := s.downloadGuideStatus(); got != "有 1 项下载任务失败，可在下方重新创建。" {
		t.Fatalf("banner = %q", got)
	}
}

// TestDownloadGuideBannerKeepsTheWayBack checks the banner still returns to the
// task page.
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

// buttonFill reports whether the button showing label paints the theme accent,
// which is what separates primaryButton from button in the rendered frame.
func buttonFill(tt *ui.Tester, label string, accent ui.Color) (bool, bool) {
	r, ok := tt.Find(label)
	if !ok {
		return false, false
	}
	img := tt.Image()
	y := int(r.Y + r.H/2)
	for _, x := range []int{int(r.X) + 4, int(r.X+r.W) - 4} {
		o := img.PixOffset(x, y)
		if img.Pix[o] == accent.R && img.Pix[o+1] == accent.G && img.Pix[o+2] == accent.B {
			return true, true
		}
	}
	return false, true
}

// TestNewDownloadCardKeepsOnePrimaryAction proves the form highlights at most
// one download action and that the guide stays a secondary control.
func TestNewDownloadCardKeepsOnePrimaryAction(t *testing.T) {
	cases := []struct {
		name          string
		engines       []core.EngineAsset
		models        []core.ModelAsset
		downloads     []core.DownloadTask
		primaryEngine bool
		primaryModel  bool
	}{
		{name: "missing both", primaryEngine: true},
		{name: "missing model", engines: []core.EngineAsset{{Name: "llama"}}, primaryModel: true},
		{name: "missing engine", models: []core.ModelAsset{{Name: "model", Available: true}}, primaryEngine: true},
		{
			name: "both ready", engines: []core.EngineAsset{{Name: "llama"}},
			models: []core.ModelAsset{{Name: "model", Available: true}},
		},
		{
			name: "engine downloading", downloads: []core.DownloadTask{
				{TaskId: "engine", Kind: "engine", State: "downloading"},
				{TaskId: "model", Kind: "model", State: "downloading"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testState()
			s.progress.NextAction = "prepare_candidate"
			s.registry.Engines, s.registry.Models, s.downloads = tc.engines, tc.models, tc.downloads
			var accent ui.Color
			tt := ui.NewTester(func(c *ui.Context) {
				accent = c.Theme().Accent
				s.downloadsPageView(c)
			}, 1120, 2400)

			for _, probe := range []struct {
				label string
				want  bool
			}{
				{"返回任务", false},
				{"下载引擎", tc.primaryEngine},
				{"下载模型", tc.primaryModel},
			} {
				filled, found := buttonFill(tt, probe.label, accent)
				if !found {
					t.Fatalf("%s button missing: %v", probe.label, tt.Texts())
				}
				if filled != probe.want {
					t.Fatalf("%s filled = %v, want %v", probe.label, filled, probe.want)
				}
			}
			if tc.primaryEngine && tc.primaryModel {
				t.Fatal("fixture asks for two primary download actions")
			}
		})
	}
}
