package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

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
	if !tt.HasText("ggml-org · v1.0.0 · Metal / CPU") {
		t.Fatalf("llama-cpp default release row missing: %v", tt.Texts())
	}
	if got := st.selectedRelease["llama-cpp"]; got != "" {
		t.Fatalf("rendering host engine list unexpectedly mutated selectedRelease: %q", got)
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

func TestEngineDrawerKeepsCrossPlatformReleaseSelection(t *testing.T) {
	s := testState()
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"

	winURL := "https://example.com/custom-eng/win-x64.zip"
	reg := core.DefaultEngineCatalog()
	reg.Register(core.StaticEngineProvider{
		Family: core.EngineFamilyCatalog{
			ID: "custom-split", Name: "CustomSplit", ModelFormat: "gguf",
			Releases: []core.EngineRelease{
				{
					ID: "custom-mac-1", Version: "v1.0-mac", Distribution: "Upstream", Channel: core.ReleaseStable, Recommended: true,
					Builds: []core.EnginePlatformBuild{{
						ID: "custom-mac-arm64", Platform: "darwin/arm64", Variant: "Metal", AcquireMode: core.EngineAcquireExternalPage,
						Artifacts: []core.EngineArtifact{{Role: "release_page", URL: "https://example.com/custom-eng/mac"}},
					}},
				},
				{
					ID: "custom-win-2", Version: "v2.0-win", Distribution: "Upstream", Channel: core.ReleasePreRelease,
					Builds: []core.EnginePlatformBuild{{
						ID: "custom-win-x64", Platform: "windows/amd64", Variant: "CUDA", AcquireMode: core.EngineAcquireManualArchive,
						Artifacts: []core.EngineArtifact{{Role: "engine", Label: "Windows Zip", URL: winURL}},
					}},
				},
			},
		},
	})
	s.engineCatalog = reg

	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	click(t, tt, "版本与地址 · custom-split")
	click(t, tt, "切换版本")
	click(t, tt, "v2.0-win · Upstream · 预发布")

	for range 3 {
		tt.Frame()
		if got := st.selectedRelease["custom-split"]; got != "custom-win-2" {
			t.Fatalf("host list reset cross-platform drawer selection to %q", got)
		}
		if !tt.HasText(winURL) || !tt.HasText("Windows x64") {
			t.Fatalf("cross-platform drawer lost Windows build details: %v", tt.Texts())
		}
	}
}

func TestDownloadsViewPinsActiveAndUnresolvedFailedAboveCompleted(t *testing.T) {
	source := "https://example.test/retry.gguf"

	t.Run("active then unresolved failed then completed", func(t *testing.T) {
		s := testState()
		s.downloads = []core.DownloadTask{
			{TaskId: "done-eng", Kind: "engine", DisplayName: "已完成引擎", OutName: "done-eng", State: "done", StartedAt: "2026-10-17T00:02:00Z"},
			{TaskId: "active-mod", Kind: "model", DisplayName: "下载中模型", OutName: "active-mod", State: "downloading", StartedAt: "2026-10-17T00:01:00Z"},
			{TaskId: "failed-mod", Kind: "model", DisplayName: "失败模型", OutName: "failed-mod", SourceUrl: source, State: "failed", StartedAt: "2026-10-17T00:00:00Z"},
		}
		ordered := s.orderedDownloads()
		if len(ordered) != 3 || ordered[0].TaskId != "active-mod" || ordered[1].TaskId != "failed-mod" || ordered[2].TaskId != "done-eng" {
			t.Fatalf("unexpected orderedDownloads: %+v", ordered)
		}

		tt := ui.NewTester(s.downloadsView, 1120, 2000)
		rActive, ok1 := tt.Find("下载中模型")
		rFailed, ok2 := tt.Find("失败模型")
		rDone, ok3 := tt.Find("已完成引擎")
		if !ok1 || !ok2 || !ok3 {
			t.Fatalf("missing task cards: %v", tt.Texts())
		}
		if !(rActive.Y < rFailed.Y && rFailed.Y < rDone.Y) {
			t.Fatalf("unexpected vertical order: active=%v failed=%v done=%v", rActive.Y, rFailed.Y, rDone.Y)
		}
	})

	t.Run("resolved failed drops below active and engine done", func(t *testing.T) {
		s := testState()
		s.downloads = []core.DownloadTask{
			{TaskId: "done-mod", Kind: "model", DisplayName: "重下成功模型", OutName: "done-mod", SourceUrl: source, State: "done", StartedAt: "2026-10-17T00:03:00Z"},
			{TaskId: "done-eng", Kind: "engine", DisplayName: "已完成引擎", OutName: "done-eng", State: "done", StartedAt: "2026-10-17T00:02:00Z"},
			{TaskId: "failed-mod", Kind: "model", DisplayName: "已解决失败模型", OutName: "failed-mod", SourceUrl: source, State: "failed", StartedAt: "2026-10-17T00:01:00Z"},
			{TaskId: "active-mod", Kind: "model", DisplayName: "下载中模型", OutName: "active-mod", State: "downloading", StartedAt: "2026-10-17T00:00:00Z"},
		}
		ordered := s.orderedDownloads()
		if len(ordered) != 4 || ordered[0].TaskId != "active-mod" || ordered[1].TaskId != "done-eng" || ordered[2].TaskId != "done-mod" || ordered[3].TaskId != "failed-mod" {
			t.Fatalf("unexpected order with resolved failure: %+v", ordered)
		}
	})
}
