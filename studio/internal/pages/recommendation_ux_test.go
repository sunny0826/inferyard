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

	click(t, tt, "一键安装推荐方案")
	if s.selected != "downloads" {
		t.Fatalf("expected device direct install to switch to downloads, got %q", s.selected)
	}
	if len(stub.intents) != 1 {
		t.Fatalf("expected 1 setup intent from device page, got %#v", stub.intents)
	}
}
