package pages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestPolystrataEnginesTabOpensDrawerWithModelPickerWhenInstalled(t *testing.T) {
	s := testState()
	s.registry.Engines = []core.EngineAsset{{
		Name:   "polystrata · local",
		Family: "polystrata",
		Kind:   "local_installation",
		Path:   "/ws/engines/polystrata/installation.json",
	}}
	s.registry.Models = []core.ModelAsset{{
		Name:      "strata-qwen36.json",
		Format:    "strata_pack",
		Available: true,
	}}
	s.device.Snapshot = json.RawMessage(`{
		"starter_recommendation": {
			"definition": "starter-recommendation.v1",
			"status": "recommended",
			"engine": "polystrata",
			"backend": "metal",
			"model_id": "polystrata-qwen36-ud-q4-k-m",
			"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
			"family": "qwen36",
			"size": "UD-Q4_K_M",
			"fit_level": "fits",
			"recommended_context": 32768,
			"estimated_download_bytes": 22663387424,
			"alternatives": [{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-lfm25-ud-q3-k-xl",
				"model_title": "LFM2.5-24B-A2B (UD-Q3_K_XL)",
				"family": "lfm25",
				"size": "UD-Q3_K_XL",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 32768,
				"estimated_download_bytes": 10947100928
			}]
		}
	}`)
	stub := &resolvePreviewStub{}
	s.preview = stub
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	if !tt.HasText("安装新模型") {
		t.Fatalf("expected 安装新模型 CTA on installed PolyStrata row: %v", tt.Texts())
	}
	click(t, tt, "安装 · polystrata")
	if len(stub.intents) != 0 {
		t.Fatalf("must not launch install before confirming in drawer: %#v", stub.intents)
	}
	if !st.engineDrawerOpen || !tt.HasText("新增模型配置") || !tt.HasText("预计下载") || !tt.HasText("安装所选模型") {
		t.Fatalf("expected PolyStrata model configuration drawer: open=%v texts=%v", st.engineDrawerOpen, tt.Texts())
	}
	if tt.HasText("将复用当前受管 PolyStrata 环境") {
		t.Fatalf("drawer must not unconditionally promise managed reuse for external engines: %v", tt.Texts())
	}

	click(t, tt, "选择模型")
	if !tt.HasText("LFM2.5-24B-A2B (UD-Q3_K_XL)") {
		t.Fatalf("expected recommended PolyStrata alternative in picker popup: %v", tt.Texts())
	}
	click(t, tt, polystrataDrawerOptionSummary(starterOption{
		ModelTitle:             "LFM2.5-24B-A2B (UD-Q3_K_XL)",
		EstimatedDownloadBytes: 10947100928,
		RecommendedContext:     32768,
	}))
	click(t, tt, "ModelScope")
	s.preview = nil
	click(t, tt, "复制命令")
	s.preview = stub
	copiedCmd := tt.Clipboard()
	for _, flag := range []string{"--family lfm25", "--model UD-Q3_K_XL", "--context 32768", "--model-source modelscope"} {
		if !strings.Contains(copiedCmd, flag) {
			t.Fatalf("copied CLI command missing %q: %q", flag, copiedCmd)
		}
	}
	click(t, tt, "安装此版本 · polystrata")

	if st.engineDrawerOpen {
		t.Fatal("expected drawer to auto-close after confirming PolyStrata setup")
	}
	if len(stub.intents) != 1 {
		t.Fatalf("expected 1 setup intent after confirming in drawer, got %#v", stub.intents)
	}
	req := stub.intents[0].(previewPolystrataSetup).req
	if req.Family != "lfm25" || req.Model != "UD-Q3_K_XL" || req.Backend != "metal" || req.ModelSource != "modelscope" {
		t.Fatalf("unexpected confirmed PolyStrata setup request: %+v", req)
	}

	// Without hardware snapshot, fallback catalog presets on darwin/arm64 still exclude CUDA-only models.
	sNoSnap := testState()
	noSnapOpts := sNoSnap.polystrataDrawerOptions("darwin/arm64")
	if len(noSnapOpts) == 0 {
		t.Fatal("expected fallback catalog presets when no snapshot is available")
	}
	for _, opt := range noSnapOpts {
		if strings.Contains(opt.ModelTitle, "Flint") || strings.Contains(opt.ModelTitle, "Gemma 4") {
			t.Fatalf("darwin/arm64 fallback presets must exclude CUDA-only model %q", opt.ModelTitle)
		}
	}
}

func TestPolystrataDrawerBlocksIncompatibleHardwareSnapshot(t *testing.T) {
	s := testState()
	s.device.Snapshot = json.RawMessage(`{
		"starter_recommendation": {
			"definition": "starter-recommendation.v1",
			"status": "recommended",
			"engine": "llama-cpp",
			"backend": "metal",
			"model_id": "model-qwen3-0.6b-q8",
			"model_title": "Qwen3-0.6B-Q8_0 (GGUF)",
			"fit_level": "fits",
			"recommended_context": 8192,
			"estimated_download_bytes": 639446688,
			"reason": "fallback_llama_cpp_insufficient_ram_for_polystrata"
		}
	}`)
	stub := &resolvePreviewStub{}
	s.preview = stub
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

	click(t, tt, "安装 · polystrata")
	if !st.engineDrawerOpen {
		t.Fatal("expected PolyStrata drawer to open")
	}
	if !tt.HasText("当前内存不足以运行 PolyStrata 模型") {
		t.Fatalf("expected localized fallback reason in drawer: %v", tt.Texts())
	}
	if tt.HasText("Qwen3.6-35B-A3B") || tt.HasText("Qwen3.8-Flash-Next") {
		t.Fatalf("drawer must not re-inject oversized PolyStrata models excluded by snapshot: %v", tt.Texts())
	}
	info, ok := tt.AccessByRole(ui.RoleButton, "安装此版本 · polystrata")
	if !ok || !info.Disabled {
		t.Fatalf("confirm install button must be disabled when snapshot has no fitting PolyStrata model: ok=%v info=%+v", ok, info)
	}
	click(t, tt, "安装此版本 · polystrata")
	if len(stub.intents) != 0 || !st.engineDrawerOpen {
		t.Fatalf("disabled confirm button must not dispatch setup: intents=%#v open=%v", stub.intents, st.engineDrawerOpen)
	}
}
