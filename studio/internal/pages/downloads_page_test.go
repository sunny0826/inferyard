package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

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
	createBox, ok := tt.Find("关联已有引擎")
	if !ok {
		t.Fatalf("engine catalog section missing: %v", tt.Texts())
	}
	return taskBox.Y, createBox.Y
}

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
	if len(s.downloads) != 2 {
		t.Fatal(s.downloads)
	}
}

func TestUnresolvedFailureSurvivesUnrelatedAssets(t *testing.T) {
	failed := core.DownloadTask{TaskId: "model-1", Kind: "model", OutName: "model", State: "failed", SourceUrl: "https://example.test/other.gguf", StartedAt: "2026-10-17T00:00:00Z"}
	active := core.DownloadTask{TaskId: "model-2", Kind: "model", State: "downloading", SourceUrl: "https://example.test/another.gguf", StartedAt: "2026-10-17T00:01:00Z"}
	s := preparingDownloadsState()
	s.downloads = append(s.downloads, active, failed)
	s.registry.Models = []core.ModelAsset{{Name: "existing", Available: true}}

	if got := s.unresolvedDownloadFailures(); got != 1 {
		t.Fatalf("unrelated asset/download hid the failure: %d", got)
	}
	if got := s.downloadGuideStatus(); got != "正在下载 2 项资源，另有 1 项下载失败。" {
		t.Fatalf("banner dropped the unresolved failure: %q", got)
	}

	resolved := core.DownloadTask{TaskId: "model-3", Kind: "model", State: "done", SourceUrl: failed.SourceUrl, StartedAt: "2026-10-17T00:02:00Z"}
	s.downloads = append([]core.DownloadTask{resolved}, s.downloads...)
	if got := s.unresolvedDownloadFailures(); got != 0 {
		t.Fatalf("same-source success did not resolve: %d", got)
	}
}

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

func TestDownloadOrderBeatsClockRollback(t *testing.T) {
	source := "https://example.test/same.gguf"
	newerDone := core.DownloadTask{TaskId: "model-2", Kind: "model", OutName: "model-2", SourceUrl: source, State: "done", StartedAt: "2026-10-16T23:59:00Z"}
	olderFailed := core.DownloadTask{TaskId: "model-1", Kind: "model", OutName: "model", SourceUrl: source, State: "failed", StartedAt: "2026-10-17T00:01:00Z"}

	s := testState()
	s.progress.NextAction = "prepare_candidate"
	s.downloads = []core.DownloadTask{newerDone, olderFailed}
	if got := s.unresolvedDownloadFailures(); got != 0 {
		t.Fatalf("rolled-back success did not resolve the older failure: %d", got)
	}

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

func TestDownloadsEngineTabStructureExtensionAndWizard(t *testing.T) {
	s := testState()
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"

	reg := core.DefaultEngineCatalog()
	reg.Register(core.StaticEngineProvider{
		Family: core.EngineFamilyCatalog{
			ID: "koboldcpp", Name: "KoboldCpp", ModelFormat: "gguf",
			Releases: []core.EngineRelease{{
				ID: "kobold-v1.91", Version: "v1.91", Distribution: "GitHub", Channel: core.ReleaseStable, Recommended: true,
				Builds: []core.EnginePlatformBuild{{
					ID: "k-v191", Platform: "any", Variant: "发布页", AcquireMode: core.EngineAcquireExternalPage,
					Artifacts: []core.EngineArtifact{{Role: "release_page", Label: "KoboldCpp v1.91", URL: "https://github.com/LostRuins/koboldcpp/releases/tag/v1.91"}},
				}},
			}},
		},
	})
	s.engineCatalog = reg

	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	for _, want := range []string{"引擎", "外部应用", "关联已有引擎", "自定义引擎地址", "下载设置", "llama.cpp", "SGLang", "LM Studio", "KoboldCpp"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in engine downloads tab: %v", want, tt.Texts())
		}
	}
	if got := st.selectedRelease["llama-cpp"]; got != "llama-cpp-upstream-v1.0.0" {
		t.Fatalf("llama-cpp default release = %q, want llama-cpp-upstream-v1.0.0", got)
	}

	// Cross-platform engines collapse cleanly on Windows hosts.
	st.hostPlatform = "windows/amd64"
	tt.Frame()
	if !tt.HasText("其他平台引擎（2）") {
		t.Fatalf("expected collapsed 其他平台引擎（2） on windows/amd64: %v", tt.Texts())
	}
	click(t, tt, "其他平台引擎（2）")
	if _, ok := tt.Find("版本与地址 · mlx-lm"); !ok {
		t.Fatal("expected 版本与地址 · mlx-lm inside 其他平台引擎")
	}
	if _, ok := tt.Find("安装 · mlx-lm"); ok {
		t.Fatal("other-platform engine row must not expose local 安装 button")
	}

	// LM Studio wizard handoff.
	click(t, tt, "关联 · lmstudio")
	if s.selected != "models" || !s.wizardOpenRequested || s.wizardPresetFamily != "lmstudio" {
		t.Fatalf("expected LM Studio wizard handoff, got selected=%q open=%v family=%q",
			s.selected, s.wizardOpenRequested, s.wizardPresetFamily)
	}
}

func TestDownloadsEngineDetailDrawerAndOSActions(t *testing.T) {
	s := testState()
	s.progress.NextAction = "prepare_candidate"
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

	click(t, tt, "版本与地址 · llama-cpp")
	if !st.engineDrawerOpen || !tt.HasText("引擎详情") {
		t.Fatalf("expected engine drawer open: %v", tt.Texts())
	}
	if !tt.HasText("下载地址") || tt.HasText("SHA-256：") {
		t.Fatalf("default v1.0.0 release_page should omit SHA-256: %v", tt.Texts())
	}

	// Toggle 仅正式版 filters out snapshot b11146.
	click(t, tt, "切换版本")
	if !tt.HasText("b11146 · ggml-org · 快照版") {
		t.Fatalf("expected snapshot option before filtering: %v", tt.Texts())
	}
	click(t, tt, "b11146 · ggml-org · 快照版")
	click(t, tt, "仅正式版")
	if !st.drawerStableOnly || st.selectedRelease["llama-cpp"] != "llama-cpp-upstream-v1.0.0" {
		t.Fatalf("expected stable filter fallback to v1.0.0, got stableOnly=%v rel=%q", st.drawerStableOnly, st.selectedRelease["llama-cpp"])
	}

	// Switch to PrismML Windows CUDA build to test multi-artifact SHA-256 + clipboard/URL actions.
	click(t, tt, "切换版本")
	click(t, tt, "prism-b10743-adfffbe · PrismML · 正式版")
	click(t, tt, "选择平台")
	click(t, tt, "Windows x64")
	if !tt.HasText("与本机") {
		t.Fatalf("expected non-host platform warning when picking Windows on macOS: %v", tt.Texts())
	}
	click(t, tt, "CUDA 12.4")
	for _, want := range []string{"llama-server (CUDA 12.4)", "CUDA 12.4 (cudart)", "SHA-256："} {
		if !tt.HasText(want) {
			t.Fatalf("missing Windows CUDA artifact detail %q: %v", want, tt.Texts())
		}
	}

	winCUDA, _ := core.CatalogEntryByID("engine-windows-cuda-x64")
	click(t, tt, "复制地址 · engine-windows-cuda-x64 · 0")
	if got := tt.Clipboard(); got != winCUDA.URL {
		t.Fatalf("clipboard = %q, want %q", got, winCUDA.URL)
	}
	click(t, tt, "打开链接 · engine-windows-cuda-x64 · 0")
	opened := tt.OpenedURLs()
	if len(opened) == 0 || opened[len(opened)-1] != winCUDA.URL {
		t.Fatalf("opened URLs = %v, want %q", opened, winCUDA.URL)
	}

	// Switch back to host platform (macOS arm64) and dispatch 安装此版本 when platform matches.
	if core.CurrentPlatform() == "darwin/arm64" {
		click(t, tt, "选择平台")
		click(t, tt, "macOS arm64")
		stub := &resolvePreviewStub{}
		s.preview = stub
		click(t, tt, "安装此版本 · llama-cpp")
		macEntry, _ := core.CatalogEntryByID("engine-macos-arm64")
		if len(stub.intents) != 1 {
			t.Fatalf("expected 1 preview install intent, got %#v", stub.intents)
		}
		dl, ok := stub.intents[0].(previewDownload)
		if !ok || dl.kind != "engine" || !strings.EqualFold(dl.source, macEntry.URL) {
			t.Fatalf("unexpected direct install intent: %#v", stub.intents)
		}
	}
}
