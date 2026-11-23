package pages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestDownloadsDynamicStarterCardSwitchAndPolystrataSetup(t *testing.T) {
	s := testState()
	s.device.Snapshot = json.RawMessage(`{
		"hardware": {
			"platform": "Darwin",
			"architecture": "arm64",
			"apple_silicon": true,
			"memory_total_bytes": 51539607552,
			"memory_available_bytes": 32212254720,
			"disk_free_bytes": 161061273600,
			"gpu": {"devices": [{"name": "Apple M4 Pro", "vendor": "apple", "arch_family": "Apple Silicon", "cores": 20, "metal_supported": true}]}
		},
		"starter_recommendation": {
			"definition": "starter-recommendation.v1",
			"status": "recommended",
			"engine": "polystrata",
			"backend": "metal",
			"model_id": "polystrata-qwen36-ud-q4-k-m",
			"model_title": "Qwen3.6 35B-A3B (UD-Q4_K_M)",
			"family": "qwen36",
			"size": "UD-Q4_K_M",
			"engine_core": "llama",
			"model_format": "gguf",
			"fit_level": "fits",
			"recommended_context": 32768,
			"estimated_download_bytes": 23085449216,
			"estimated_ram_bytes": 25769803776,
			"gpu": {"name": "Apple M4 Pro", "vendor": "apple", "arch_family": "Apple Silicon", "unified_memory_bytes": 51539607552},
			"alternatives": [
				{
					"engine": "llama-cpp",
					"backend": "metal",
					"model_id": "model-qwen3-0.6b-q8",
					"model_title": "Qwen3-0.6B-Q8_0 (GGUF)",
					"model_format": "gguf",
					"fit_level": "fits",
					"recommended_context": 8192,
					"estimated_download_bytes": 639446688,
					"estimated_ram_bytes": 2147483648
				}
			]
		}
	}`)
	stub := &resolvePreviewStub{}
	s.preview = stub
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	for _, want := range []string{"推荐组合", "PolyStrata", "Qwen3.6 35B-A3B (UD-Q4_K_M)", "Apple M4 Pro", "一键安装 PolyStrata", "切换备选方案（1）"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in dynamic starter card: %v", want, tt.Texts())
		}
	}

	click(t, tt, "ModelScope")
	click(t, tt, "一键安装 PolyStrata")
	if len(stub.intents) != 1 {
		t.Fatalf("expected 1 polystrata setup intent, got %#v", stub.intents)
	}
	setupIntent, ok := stub.intents[0].(previewPolystrataSetup)
	if !ok {
		t.Fatalf("expected previewPolystrataSetup, got %T", stub.intents[0])
	}
	if setupIntent.req.Family != "qwen36" || setupIntent.req.Model != "UD-Q4_K_M" || setupIntent.req.Context != 32768 || setupIntent.req.Backend != "metal" || setupIntent.req.ModelSource != "modelscope" {
		t.Fatalf("unexpected polystrata setup request: %+v", setupIntent.req)
	}

	// Switch to llama.cpp alternative
	stub.intents = nil
	click(t, tt, "切换备选方案（1）")
	click(t, tt, "使用此方案 · model-qwen3-0.6b-q8")
	if !tt.HasText("下载推荐组合") {
		t.Fatalf("expected llama.cpp starter CTA after switching alternative: %v", tt.Texts())
	}
	click(t, tt, "下载推荐组合")
	if len(stub.intents) != 2 {
		t.Fatalf("expected engine+model intents for llama.cpp fallback, got %#v", stub.intents)
	}

	// PolyStrata drawer exposes copyable CLI install command
	click(t, tt, "版本与地址 · polystrata")
	if !tt.HasText("复制命令") || !tt.HasText("inferyard engine prepare-polystrata") {
		t.Fatalf("expected copyable PolyStrata CLI command in drawer: %v", tt.Texts())
	}
}

func TestPolystrataRound1ReviewRegressions(t *testing.T) {
	// 1. Engine row honors selected PolyStrata alternative instead of falling back to primary
	s := testState()
	s.device.Snapshot = json.RawMessage(`{
		"starter_recommendation": {
			"definition": "starter-recommendation.v1",
			"status": "recommended",
			"engine": "polystrata",
			"backend": "metal",
			"model_id": "polystrata-qwen38-iq3-s",
			"model_title": "Qwen3.8-Flash-Next (IQ3_S)",
			"family": "qwen",
			"size": "IQ3_S",
			"recommended_context": 32768,
			"estimated_download_bytes": 83600000000,
			"alternatives": [{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-qwen38-coder-iq1-m",
				"model_title": "Qwen3.8-Flash-Next Coder (IQ1_M)",
				"family": "coder",
				"size": "IQ1_M",
				"recommended_context": 32768,
				"estimated_download_bytes": 58400000000
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
	click(t, tt, "切换备选方案（1）")
	click(t, tt, "使用此方案 · polystrata-qwen38-coder-iq1-m")
	click(t, tt, "安装 · polystrata")
	if len(stub.intents) != 1 {
		t.Fatalf("expected 1 setup intent, got %#v", stub.intents)
	}
	gotReq := stub.intents[0].(previewPolystrataSetup).req
	if gotReq.Family != "coder" || gotReq.Model != "IQ1_M" {
		t.Fatalf("engine row ignored selected PolyStrata alternative: %+v", gotReq)
	}

	// 2. llama.cpp fallback selects Windows CUDA build on windows/amd64 + cuda, and exposes Linux guidance
	winCUDA, _ := core.CatalogEntryByID("engine-windows-cuda-x64")
	if got := starterLlamaCppEngineURL("windows/amd64", "cuda"); got != winCUDA.URL {
		t.Fatalf("starterLlamaCppEngineURL(windows/amd64, cuda) = %q, want %q", got, winCUDA.URL)
	}
	winCPU, _ := core.CatalogEntryByID("engine-windows-x64")
	if got := starterLlamaCppEngineURL("windows/amd64", "cpu"); got != winCPU.URL {
		t.Fatalf("starterLlamaCppEngineURL(windows/amd64, cpu) = %q, want %q", got, winCPU.URL)
	}
	if got := starterLlamaCppEngineURL("linux/amd64", "cuda"); got != "" {
		t.Fatalf("expected empty direct URL for linux/amd64, got %q", got)
	}
	sLinux := testState()
	sLinux.device.Snapshot = json.RawMessage(`{
		"starter_recommendation": {
			"definition": "starter-recommendation.v1",
			"status": "recommended",
			"engine": "llama-cpp",
			"backend": "cuda",
			"model_id": "model-qwen3-0.6b-q8",
			"model_title": "Qwen3-0.6B-Q8_0 (GGUF)",
			"recommended_context": 8192
		}
	}`)
	stLinux := sLinux.ensureDownloadsUI()
	stLinux.hostPlatform = "linux/amd64"
	stLinux.selectedRelease["llama-cpp"] = "llama-cpp-prism-b10743"
	stLinux.selectedBuild["llama-cpp"] = "engine-linux-x64"
	ttLinux := ui.NewTester(sLinux.downloadsPageView, 1120, 2400)
	if !ttLinux.HasText("查看引擎获取方式") || !ttLinux.HasText("下载推荐模型") {
		t.Fatalf("expected Linux llama.cpp fallback actions in starter card: %v", ttLinux.Texts())
	}
	click(t, ttLinux, "查看引擎获取方式")
	if stLinux.selectedRelease["llama-cpp"] != "llama-cpp-upstream-v1.0.0" || stLinux.selectedBuild["llama-cpp"] != "llama-upstream-v1.0.0-linux-x64" {
		t.Fatalf("Linux CUDA starter link did not override prior CPU drawer selection: rel=%q build=%q",
			stLinux.selectedRelease["llama-cpp"], stLinux.selectedBuild["llama-cpp"])
	}

	// 3. PolyStrata launch summary omits hardcoded 8192, never guesses backend from GPU vendor, and shows resolved check Mode
	sAMD := testState()
	sAMD.registry.Engines = []core.EngineAsset{{Name: "polystrata · local", Family: "polystrata", Kind: "local_installation"}}
	sAMD.registry.Models = []core.ModelAsset{{Name: "strata-q2.json", Format: "strata_pack", Available: true}}
	sAMD.engine = "polystrata · local"
	sAMD.model = "strata-q2.json"
	sAMD.device.Snapshot = json.RawMessage(`{"hardware":{"gpu":{"devices":[{"name":"RX 9070 XT","vendor":"amd"}]}}}`)
	if summary := launchSummaryLine(&sAMD); containsAll(summary, "HIP") || containsAll(summary, "上下文 8192") {
		t.Fatalf("unresolved PolyStrata backend must not guess HIP or hardcode 8192: %q", summary)
	}
	sAMD.checks.Engines = map[string]core.EngineEligibility{
		"polystrata · local": {State: core.EngineCheckEligible, Family: "polystrata", Mode: "hip"},
	}
	if summary := launchSummaryLine(&sAMD); !containsAll(summary, "PolyStrata（HIP (ROCm) 加速）") || containsAll(summary, "上下文 8192") {
		t.Fatalf("resolved HIP PolyStrata launch summary mismatch: %q", summary)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && !stringsContains(s, sub) {
			return false
		}
	}
	return true
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPolystrataTaskCardProgressBlockersAndLogTail(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), ".inferyard-setup.log")
	if err := os.WriteFile(logFile, []byte("line-1\nbuilding strata metal kernel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := testState()
	blockerErr := core.Err("polystrata_preflight_blocked")
	blockerErr.Details = map[string]any{
		"blockers": []map[string]any{{
			"code":        "cmake_missing",
			"message":     "CMake 3.24+ is required.",
			"fix_command": "brew install cmake",
		}},
	}
	s.downloads = []core.DownloadTask{
		{
			TaskId:      "ps-active",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · qwen36 · UD-Q4_K_M",
			State:       "downloading",
			Stage:       "setup_engine",
			Step:        ptr(3),
			StepTotal:   ptr(4),
			Percent:     ptr(65.0),
			Detail:      ptr("Metal 编译"),
			OutName:     "polystrata-1",
			LogPath:     ptr(logFile),
		},
		{
			TaskId:      "ps-failed",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · qwen · Q2_0",
			State:       "failed",
			Stage:       "preflight",
			OutName:     "polystrata-2",
			Error:       blockerErr,
		},
	}

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1120, 2200)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	for _, want := range []string{"PolyStrata 引导安装", "3 / 4", "安装引擎环境", "65%", "Metal 编译", "环境预检未通过", "CMake 3.24+ is required.", "brew install cmake"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in polystrata task cards: %v", want, tt.Texts())
		}
	}
	click(t, tt, "复制修复命令 · 0")
	if got := tt.Clipboard(); got != "brew install cmake" {
		t.Fatalf("clipboard = %q, want brew install cmake", got)
	}
	click(t, tt, "任务详情")
	click(t, tt, "查看日志尾部")
	if !tt.HasText("building strata metal kernel") {
		t.Fatalf("expected log tail content in expanded fold: %v", tt.Texts())
	}
}

func TestDevicePageShowsAMDFeaturesAndRecommendationCard(t *testing.T) {
	s := testState()
	s.device.Snapshot = json.RawMessage(`{
		"hardware": {
			"platform": "Linux",
			"cpu_model": "AMD Ryzen 9 9950X",
			"logical_cpus": 32,
			"architecture": "x86_64",
			"cpu_features": {"avx2": true, "avx512f": true},
			"memory_total_bytes": 137438953472,
			"memory_available_bytes": 118111600640,
			"disk_free_bytes": 536870912000,
			"gpu": {
				"devices": [{
					"name": "AMD Radeon RX 9070 XT",
					"vendor": "amd",
					"arch_family": "RDNA 4",
					"driver_version": "6.4.0",
					"memory_total_bytes": 17179869184
				}]
			}
		},
		"starter_recommendation": {
			"definition": "starter-recommendation.v1",
			"status": "recommended",
			"engine": "polystrata",
			"backend": "hip",
			"model_id": "polystrata-qwen38-q2-0",
			"model_title": "Qwen3.8-Flash-Next (Q2_0)",
			"family": "qwen",
			"size": "Q2_0",
			"recommended_context": 32768,
			"estimated_download_bytes": 66400000000
		}
	}`)
	tt := ui.NewTester(s.deviceView, 1120, 1800)
	for _, want := range []string{"AMD Radeon RX 9070 XT", "RDNA 4", "AVX2 · AVX-512", "推荐配置", "PolyStrata · HIP (ROCm) 加速", "Qwen3.8-Flash-Next (Q2_0)"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q on device page: %v", want, tt.Texts())
		}
	}
	click(t, tt, "前往下载")
	if s.selected != "downloads" {
		t.Fatalf("selected = %q, want downloads", s.selected)
	}
}

func TestPolystrataModelCompatibilityWizardAndHIPBackendLabel(t *testing.T) {
	s := testState()
	s.registry.Engines = []core.EngineAsset{
		{Name: "polystrata · local", Family: "polystrata", Kind: "local_installation"},
		{Name: "llama-cpp · local", Family: "llama-cpp", Kind: "local_installation"},
	}
	gguf := core.ModelAsset{Name: "qwen35.gguf", Format: "gguf", Architecture: "qwen35moe", Available: true}
	ggufUnknown := core.ModelAsset{Name: "unknown.gguf", Format: "gguf", Available: true}
	ggufLlama := core.ModelAsset{Name: "llama3.gguf", Format: "gguf", Architecture: "llama", Available: true}
	ggufUpper := core.ModelAsset{Name: "gemma4-upper.gguf", Format: "gguf", Architecture: "GEMMA4", Available: true}
	pack := core.ModelAsset{Name: "strata-qwen.json", Format: "strata_pack", Available: true}
	dir := core.ModelAsset{Name: "mlx-dir", Format: "directory", Available: true}

	s.engine = "polystrata · local"
	if !s.compatibleModel(gguf) || !s.compatibleModel(ggufUnknown) || s.compatibleModel(ggufLlama) || s.compatibleModel(ggufUpper) || !s.compatibleModel(pack) || s.compatibleModel(dir) {
		t.Fatal("polystrata compatibility mismatch")
	}
	if got := s.incompatibleModelSuffix(ggufLlama); got != "（PolyStrata 暂不支持此架构）" {
		t.Fatalf("incompatibleModelSuffix(llama3.gguf) = %q", got)
	}
	if modelLabel(pack) != "strata-qwen.json · PolyStrata" {
		t.Fatalf("unexpected strata_pack label: %q", modelLabel(pack))
	}
	if localBackendLabel("polystrata") != "PolyStrata" {
		t.Fatalf("unexpected polystrata backend label: %q", localBackendLabel("polystrata"))
	}

	s.engine = "llama-cpp · local"
	if !s.compatibleModel(gguf) || s.compatibleModel(pack) || s.compatibleModel(dir) {
		t.Fatal("llama-cpp must reject strata_pack and directory models")
	}

	s.engine = "hip-engine"
	s.checks.Engines = map[string]core.EngineEligibility{
		"hip-engine": {State: core.EngineCheckEligible, Mode: "hip"},
	}
	if got := prismBackendLabel(&s); got != "HIP (ROCm) 加速" {
		t.Fatalf("prismBackendLabel(hip) = %q, want HIP (ROCm) 加速", got)
	}

	// Detected polystrata candidate populates wizard dataDir
	w := &localEngineWizardState{}
	dataDir := "/opt/PolyStrata-data"
	applyDetectedEngine(w, inferyard.DetectedEngine{
		Family:           "polystrata",
		Executable:       "/opt/PolyStrata/.venv/bin/python",
		WorkingDirectory: "/opt/PolyStrata",
		RuntimeRoots:     []string{"/opt/PolyStrata/.venv"},
		DataDir:          &dataDir,
	})
	if w.family != "polystrata" || w.dataDir != dataDir || !wizardCanSubmit(w) {
		t.Fatalf("unexpected wizard state after applying detected polystrata: %+v", w)
	}
}
