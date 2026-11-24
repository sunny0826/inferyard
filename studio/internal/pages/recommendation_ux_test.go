package pages

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

const testRichRecommendationSnapshot = `{
	"hardware": {
		"platform": "Darwin",
		"os_version": "15.4",
		"architecture": "arm64",
		"apple_silicon": true,
		"memory_total_bytes": 51539607552,
		"memory_available_bytes": 36721975296,
		"disk_free_bytes": 171798691840,
		"gpu": {"devices": [{"name": "Apple M4 Pro", "vendor": "apple", "cores": 20, "metal_supported": true}]}
	},
	"starter_recommendation": {
		"definition": "starter-recommendation.v1",
		"status": "recommended",
		"engine": "polystrata",
		"backend": "metal",
		"model_id": "polystrata-qwen36-ud-q4-k-m",
		"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
		"family": "qwen36",
		"size": "UD-Q4_K_M",
		"engine_core": "strata-poly",
		"model_format": "gguf",
		"fit_level": "tight",
		"tier": "best",
		"recommended_context": 32768,
		"estimated_download_bytes": 22663387424,
		"estimated_ram_bytes": 28700000000,
		"disk_free_bytes": 171798691840,
		"reason": "polystrata_hardware_fit",
		"gpu": {"name": "Apple M4 Pro", "vendor": "apple", "unified_memory_bytes": 51539607552},
		"alternatives": [
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-lfm25-ud-q3-k-xl",
				"model_title": "LFM2.5-24B-A2B (UD-Q3_K_XL)",
				"family": "lfm25",
				"size": "UD-Q3_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "lightweight",
				"recommended_context": 32768,
				"estimated_download_bytes": 10947100928,
				"estimated_ram_bytes": 17000000000
			},
			{
				"engine": "llama-cpp",
				"backend": "metal",
				"model_id": "model-qwen3-0.6b-q8",
				"model_title": "Qwen3-0.6B-Q8_0（GGUF）",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "fallback",
				"recommended_context": 8192,
				"estimated_download_bytes": 639446688,
				"estimated_ram_bytes": 2147483648
			}
		],
		"polystrata_models": [
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-qwen36-ud-q4-k-m",
				"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
				"family": "qwen36",
				"size": "UD-Q4_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "tight",
				"tier": "best",
				"recommended_context": 32768,
				"estimated_download_bytes": 22663387424,
				"estimated_ram_bytes": 28700000000
			},
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-qwen36-ud-q3-k-m",
				"model_title": "Qwen3.6-35B-A3B (UD-Q3_K_M)",
				"family": "qwen36",
				"size": "UD-Q3_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 32768,
				"estimated_download_bytes": 17104402720,
				"estimated_ram_bytes": 23100000000
			},
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-lfm25-ud-q3-k-xl",
				"model_title": "LFM2.5-24B-A2B (UD-Q3_K_XL)",
				"family": "lfm25",
				"size": "UD-Q3_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "lightweight",
				"recommended_context": 32768,
				"estimated_download_bytes": 10947100928,
				"estimated_ram_bytes": 17000000000
			}
		]
	}
}`

func TestDashboardFirstUseEmbedsFlatRecommendationsAndHandsOffToDownloads(t *testing.T) {
	s := testState()
	s.selected = "dashboard"
	s.ready = core.Readiness{
		CLI:       core.InferyardProbe{Status: "ok"},
		Workspace: core.DirectoryReadiness{Exists: true, Initialized: true},
		Results:   core.DirectoryReadiness{Exists: true},
		Models:    core.DirectoryReadiness{Exists: true},
	}
	s.progress = &inferyard.WorkspaceProgress{NextAction: "prepare_candidate"}
	s.device.Snapshot = json.RawMessage(testRichRecommendationSnapshot)
	stub := &resolvePreviewStub{}
	s.preview = stub
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"

	tt := ui.NewTester(s.dashboardView, 1120, 2600)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	for _, want := range []string{
		"准备模型与引擎",
		"首选推荐",
		"轻量快速",
		"通用轻量",
		"紧凑适配",
		"推荐适配",
		"Qwen3.6-35B-A3B (UD-Q4_K_M)",
		"LFM2.5-24B-A2B (UD-Q3_K_XL)",
		"预计内存",
		"一键安装 PolyStrata",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q on dashboard first-use view: %v", want, tt.Texts())
		}
	}

	click(t, tt, "选择推荐方案 · polystrata-lfm25-ud-q3-k-xl")
	click(t, tt, "一键安装 PolyStrata")
	if s.selected != "downloads" {
		t.Fatalf("expected dashboard install CTA to navigate to downloads, got %q", s.selected)
	}
	if s.starterPanelExpanded() {
		t.Fatal("expected downloads recommendation panel to auto-collapse after launching install")
	}
	if len(stub.intents) != 1 {
		t.Fatalf("expected 1 setup intent, got %#v", stub.intents)
	}
	req := stub.intents[0].(previewPolystrataSetup).req
	if req.Family != "lfm25" || req.Model != "UD-Q3_K_XL" {
		t.Fatalf("unexpected setup request: %+v", req)
	}
}

func TestDownloadsStarterRemainsAccessibleAsCompactBarWhenAssetsExist(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	s.device.Snapshot = json.RawMessage(testRichRecommendationSnapshot)
	s.registry.Engines = []core.EngineAsset{{Name: "llama-server", Family: "llama-cpp"}}
	s.registry.Models = []core.ModelAsset{{Name: "Qwen3-0.6B-Q8_0.gguf", Format: "gguf", Available: true}}
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"

	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	if !tt.HasText("本机硬件推荐") || !tt.HasText("展开推荐方案") {
		t.Fatalf("expected compact recommendation bar when assets already exist: %v", tt.Texts())
	}
	if tt.HasText("一键安装 PolyStrata") {
		t.Fatalf("expected recommendation panel collapsed by default when assets exist: %v", tt.Texts())
	}

	click(t, tt, "展开推荐方案")
	if !s.starterPanelExpanded() || !tt.HasText("一键安装 PolyStrata") || !tt.HasText("已安装") {
		t.Fatalf("expected expanded flat cards with installed badge: %v", tt.Texts())
	}

	click(t, tt, "收起")
	if s.starterPanelExpanded() || !tt.HasText("展开推荐方案") {
		t.Fatalf("expected compact bar restored after collapse: %v", tt.Texts())
	}
}

func TestDeviceRecommendationDirectInstallAndDrawerUsesFullPolystrataModels(t *testing.T) {
	s := testState()
	s.selected = "device"
	s.device.Snapshot = json.RawMessage(testRichRecommendationSnapshot)
	stub := &resolvePreviewStub{}
	s.preview = stub
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"

	// Drawer receives the extra mid-tier quant from polystrata_models even though it is not in flat alternatives
	opts := s.polystrataDrawerOptions("darwin/arm64")
	foundQ3 := false
	for _, opt := range opts {
		if opt.ModelID == "polystrata-qwen36-ud-q3-k-m" {
			foundQ3 = true
			break
		}
	}
	if !foundQ3 {
		t.Fatalf("expected drawer to include full polystrata_models entry Q3_K_M, got %+v", opts)
	}

	tt := ui.NewTester(s.deviceView, 1120, 1800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	click(t, tt, "一键安装 PolyStrata")
	if s.selected != "downloads" {
		t.Fatalf("expected device direct install to switch to downloads, got %q", s.selected)
	}
	if len(stub.intents) != 1 {
		t.Fatalf("expected 1 setup intent from device page, got %#v", stub.intents)
	}
}

func TestRecommendationReviewRegressions(t *testing.T) {
	// 1. Existing llama.cpp engine + missing model must download only the model (not re-download engine)
	t.Run("existing_llama_cpp_engine_downloads_only_missing_model", func(t *testing.T) {
		s := testState()
		s.selected = "downloads"
		s.device.Snapshot = json.RawMessage(testRichRecommendationSnapshot)
		s.registry.Engines = []core.EngineAsset{{Name: "llama-server", Family: "llama-cpp"}}
		s.registry.Models = nil
		stub := &resolvePreviewStub{}
		s.preview = stub
		st := s.ensureDownloadsUI()
		st.hostPlatform = "darwin/arm64"
		s.setStarterPanelExpanded(true)

		tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()

		click(t, tt, "选择推荐方案 · model-qwen3-0.6b-q8")
		if !tt.HasText("下载推荐模型") || tt.HasText("下载推荐组合") {
			t.Fatalf("expected model-only CTA when llama.cpp engine already installed: %v", tt.Texts())
		}
		click(t, tt, "下载推荐模型")
		if len(stub.intents) != 1 {
			t.Fatalf("expected exactly 1 model download intent, got %#v", stub.intents)
		}
		dl, ok := stub.intents[0].(previewDownload)
		if !ok || dl.kind != "model" {
			t.Fatalf("expected model download intent, got %#v", stub.intents[0])
		}

		// Once the model is also present, CTA becomes disabled "已就绪"
		s.registry.Models = []core.ModelAsset{{Name: "Qwen3-0.6B-Q8_0.gguf", Format: "gguf", Available: true}}
		tt.Frame()
		if !tt.HasText("已就绪") || tt.HasText("下载推荐模型") {
			t.Fatalf("expected ready state when both engine and model installed: %v", tt.Texts())
		}
	})

	// 2. Uninstalling a PolyStrata model clears the "已安装" badge even when a done task remains in history
	t.Run("uninstalled_polystrata_model_clears_installed_badge", func(t *testing.T) {
		s := testState()
		s.selected = "downloads"
		s.device.Snapshot = json.RawMessage(testRichRecommendationSnapshot)
		cfgPath := "/tmp/engines/polystrata/strata-qwen36.json"
		s.registry.Engines = []core.EngineAsset{{Name: "PolyStrata · local", Family: "polystrata"}}
		s.registry.Models = []core.ModelAsset{{Name: "strata-qwen36.json", Format: "strata_pack", Path: cfgPath, Available: true}}
		s.downloads = []core.DownloadTask{{
			TaskId:          "ps-done",
			Kind:            "polystrata",
			DisplayName:     "PolyStrata · qwen36 · UD-Q4_K_M",
			State:           "done",
			ModelConfigPath: &cfgPath,
		}}
		st := s.ensureDownloadsUI()
		st.hostPlatform = "darwin/arm64"
		s.setStarterPanelExpanded(true)

		tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()
		opts := starterOptions(s.starterRecommendation())
		if !s.isStarterOptionInstalled(opts[0]) || !tt.HasText("已就绪") {
			t.Fatalf("expected installed/ready state while strata model is present: %v", tt.Texts())
		}

		// Uninstall the model while keeping engine + done task history
		s.registry.Models = nil
		tt.Frame()
		if s.isStarterOptionInstalled(opts[0]) || tt.HasText("已就绪") || !tt.HasText("安装所选 PolyStrata 模型") {
			t.Fatalf("installed/ready state must clear after PolyStrata model is uninstalled: %v", tt.Texts())
		}
	})

	// 3. Linux device page exposes drawer action + independent model download for llama.cpp fallback
	t.Run("linux_device_page_uses_platform_aware_llama_cpp_actions", func(t *testing.T) {
		s := testState()
		s.selected = "device"
		s.device.Snapshot = json.RawMessage(`{
			"hardware": {"platform": "Linux", "memory_total_bytes": 17179869184, "disk_free_bytes": 88046829568},
			"starter_recommendation": {
				"definition": "starter-recommendation.v1",
				"status": "recommended",
				"engine": "llama-cpp",
				"backend": "cpu",
				"model_id": "model-qwen3-4b-q4km",
				"model_title": "Qwen3-4B-Q4_K_M（GGUF）",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "best",
				"recommended_context": 8192,
				"estimated_download_bytes": 2497280256,
				"estimated_ram_bytes": 6442450944,
				"reason": "fallback_llama_cpp_no_compatible_polystrata_gpu"
			}
		}`)
		stub := &resolvePreviewStub{}
		s.preview = stub
		st := s.ensureDownloadsUI()
		st.hostPlatform = "linux/amd64"

		tt := ui.NewTester(s.deviceView, 1120, 1800)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()

		if !tt.HasText("查看引擎获取方式") || !tt.HasText("下载推荐模型") || tt.HasText("下载推荐组合") {
			t.Fatalf("expected Linux-aware actions on device page: %v", tt.Texts())
		}
		click(t, tt, "下载推荐模型")
		if len(stub.intents) != 1 || stub.intents[0].(previewDownload).kind != "model" {
			t.Fatalf("expected direct model download intent on Linux device page, got %#v", stub.intents)
		}
	})

	// 4. Failed regular engine/model download collapses recommendation panel by default
	t.Run("regular_failed_download_collapses_starter_panel_by_default", func(t *testing.T) {
		s := testState()
		s.selected = "downloads"
		s.device.Snapshot = json.RawMessage(testRichRecommendationSnapshot)
		s.downloads = []core.DownloadTask{{
			TaskId:    "model-failed",
			Kind:      "model",
			State:     "failed",
			OutName:   "failed-model",
			SourceUrl: "https://example.invalid/model.gguf",
			Error:     core.Err("download_failed"),
		}}
		st := s.ensureDownloadsUI()
		st.hostPlatform = "darwin/arm64"

		tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()

		if s.starterPanelExpanded() || !tt.HasText("展开推荐方案") {
			t.Fatalf("expected starter panel collapsed by default while unresolved model failure exists: %v", tt.Texts())
		}
	})

	// 5. Missing models_disk_free_bytes must not fall back to displaying workspace disk_free_bytes
	t.Run("missing_models_disk_omits_workspace_disk_in_summary", func(t *testing.T) {
		s := testState()
		s.device.Snapshot = json.RawMessage(`{
			"hardware": {
				"platform": "Linux",
				"memory_total_bytes": 34359738368,
				"disk_free_bytes": 128849018880,
				"models_disk_free_bytes": null,
				"missing": {"models_disk_free_bytes": "multiple_models_roots_unselected"}
			},
			"starter_recommendation": {
				"definition": "starter-recommendation.v1",
				"status": "blocked",
				"disk_free_bytes": null,
				"reason": "disk_space_unavailable",
				"notes": ["multiple_models_roots_unselected"]
			}
		}`)
		rec := s.starterRecommendation()
		summary := s.starterHardwareSummary(rec)
		if summary != "内存 32.00 GiB" {
			t.Fatalf("expected summary to omit workspace disk when models_disk_free_bytes is missing, got %q", summary)
		}
	})

	// 6. Installed CPU-only llama.cpp build must not mark a recommended CUDA build as ready
	t.Run("cpu_llama_cpp_does_not_block_recommended_cuda_engine", func(t *testing.T) {
		s := testState()
		s.selected = "device"
		s.device.Snapshot = json.RawMessage(`{
			"hardware": {"platform": "Windows", "memory_total_bytes": 17179869184, "disk_free_bytes": 88046829568},
			"starter_recommendation": {
				"definition": "starter-recommendation.v1",
				"status": "recommended",
				"engine": "llama-cpp",
				"backend": "cuda",
				"model_id": "model-qwen3-4b-q4km",
				"model_title": "Qwen3-4B-Q4_K_M（GGUF）",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "best",
				"recommended_context": 8192,
				"estimated_download_bytes": 2497280256,
				"estimated_ram_bytes": 6442450944,
				"reason": "fallback_llama_cpp_insufficient_vram_for_polystrata"
			}
		}`)
		s.registry.Engines = []core.EngineAsset{{Name: "llama-cpu", Family: "llama-cpp", Path: "C:\\engines\\cpu\\llama-server.exe"}}
		s.checks = core.EngineCheckOverview{
			Supported: true,
			Engines: map[string]core.EngineEligibility{
				"llama-cpu": {State: core.EngineCheckEligible, Mode: "cpu", Modes: map[string]bool{"cpu": true, "metal": false}},
			},
		}
		s.registry.Models = []core.ModelAsset{{Name: "Qwen3-4B-Q4_K_M.gguf", Format: "gguf", Available: true}}
		stub := &resolvePreviewStub{}
		s.preview = stub
		st := s.ensureDownloadsUI()
		st.hostPlatform = "windows/amd64"

		tt := ui.NewTester(s.deviceView, 1120, 1800)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()

		if tt.HasText("已就绪") || !tt.HasText("下载推荐引擎") {
			t.Fatalf("expected 下载推荐引擎 when only CPU llama.cpp is installed for CUDA recommendation: %v", tt.Texts())
		}
		click(t, tt, "下载推荐引擎")
		if len(stub.intents) != 1 || stub.intents[0].(previewDownload).kind != "engine" {
			t.Fatalf("expected 1 engine download intent, got %#v", stub.intents)
		}
	})

	// 7. Reusing the same PolyStrata config path for another quantization must not mark the old quant ready
	t.Run("reused_polystrata_config_path_clears_old_quant_ready_state", func(t *testing.T) {
		s := testState()
		s.selected = "downloads"
		s.device.Snapshot = json.RawMessage(testRichRecommendationSnapshot)
		cfgPath := "/tmp/engines/polystrata/strata-qwen36.json"
		s.registry.Engines = []core.EngineAsset{{Name: "PolyStrata · local", Family: "polystrata"}}
		s.registry.Models = []core.ModelAsset{{
			Name:         "strata-qwen36.json",
			Format:       "strata_pack",
			Path:         cfgPath,
			Available:    true,
			CoveredPaths: []string{"/tmp/polystrata-data/models/Qwen3.6-35B-A3B-UD-Q3_K_M.gguf"},
		}}
		s.downloads = []core.DownloadTask{{
			TaskId:          "ps-q4-old",
			Kind:            "polystrata",
			DisplayName:     "PolyStrata · qwen36 · UD-Q4_K_M",
			State:           "done",
			ModelConfigPath: &cfgPath,
		}}
		st := s.ensureDownloadsUI()
		st.hostPlatform = "darwin/arm64"
		s.setStarterPanelExpanded(true)

		tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()

		opts := starterOptions(s.starterRecommendation())
		if s.isStarterOptionInstalled(opts[0]) || tt.HasText("已就绪") || !tt.HasText("安装所选 PolyStrata 模型") {
			t.Fatalf("old Q4 quant must not be treated as ready when config path now points to Q3: %v", tt.Texts())
		}
	})
}
