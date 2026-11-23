package pages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestStarterRefreshingPlaceholderAndPrimaryCTAHierarchy(t *testing.T) {
	s := testState()
	s.device.Refreshing = true
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	var accent ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		accent = c.Theme().Accent
		s.downloadsPageView(c)
	}, 1120, 2600)

	if !tt.HasText("检测中") || !tt.HasText("正在检测本机硬件配置与可用磁盘空间…") {
		t.Fatalf("expected refreshing placeholder while device check runs: %v", tt.Texts())
	}
	if tt.HasText("下载推荐组合") || tt.HasText("Qwen3-0.6B-Q8_0") {
		t.Fatalf("must not flash static llama.cpp fallback while device check runs: %v", tt.Texts())
	}

	s.device.Refreshing = false
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
			"notes": ["xcode_clt_missing"]
		}
	}`)
	tt.Frame()

	filled, found := buttonFill(tt, "一键安装 PolyStrata", accent)
	if !found || !filled {
		t.Fatalf("expected primaryButton on starter PolyStrata CTA: found=%v filled=%v", found, filled)
	}
	if !tt.HasText("xcode-select --install") || !tt.HasText("前置工具") {
		t.Fatalf("expected inline xcode_clt_missing hint on PolyStrata starter card: %v", tt.Texts())
	}
	click(t, tt, "复制 Xcode CLT 安装命令")
	if got := tt.Clipboard(); got != "xcode-select --install" {
		t.Fatalf("clipboard = %q, want xcode-select --install", got)
	}
	rowFilled, rowFound := buttonFill(tt, "安装 · polystrata", accent)
	if !rowFound || rowFilled {
		t.Fatalf("expected secondary button for PolyStrata row while starter card visible: found=%v filled=%v", rowFound, rowFilled)
	}
}

func TestStarterAlternativeAutoCollapsesAndSyncsDownloadSource(t *testing.T) {
	s := testState()
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
				"engine": "llama-cpp",
				"backend": "metal",
				"model_id": "model-qwen3-0.6b-q8",
				"model_title": "Qwen3-0.6B-Q8_0 (GGUF)",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 8192,
				"estimated_download_bytes": 639446688
			}]
		}
	}`)
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	click(t, tt, "ModelScope")
	if st.polystrataModelSource != "modelscope" || st.modelSource != "modelscope" {
		t.Fatalf("expected ModelScope selection to sync across sources: poly=%q direct=%q", st.polystrataModelSource, st.modelSource)
	}

	click(t, tt, "切换备选方案（1）")
	if !st.starterAltOpen {
		t.Fatal("expected alternatives fold to open")
	}
	click(t, tt, "使用此方案 · model-qwen3-0.6b-q8")
	if st.starterAltOpen || st.starterChoice != 1 {
		t.Fatalf("expected fold auto-collapse after choosing alternative: open=%v choice=%d", st.starterAltOpen, st.starterChoice)
	}
	if !tt.HasText("模型下载源") || !tt.HasText("下载推荐组合") {
		t.Fatalf("expected download source switcher to remain visible on llama.cpp option: %v", tt.Texts())
	}

	click(t, tt, "HuggingFace")
	if st.modelSource != "huggingface" || st.polystrataModelSource != "huggingface" {
		t.Fatalf("expected HuggingFace selection on llama.cpp option to sync back to PolyStrata: direct=%q poly=%q", st.modelSource, st.polystrataModelSource)
	}
}

func TestStarterHiddenWhenUnresolvedPolystrataTaskExists(t *testing.T) {
	s := testState()
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
			"recommended_context": 32768
		}
	}`)
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	if !tt.HasText("一键安装 PolyStrata") {
		t.Fatalf("expected starter card initially: %v", tt.Texts())
	}

	s.downloads = []core.DownloadTask{{
		TaskId:      "ps-failed",
		Kind:        "polystrata",
		DisplayName: "PolyStrata · qwen36 · UD-Q4_K_M",
		State:       "failed",
		Stage:       "setup_engine",
		OutName:     "polystrata",
	}}
	tt.Frame()
	if tt.HasText("一键安装 PolyStrata") {
		t.Fatalf("starter card must stay hidden while unresolved failed PolyStrata task exists: %v", tt.Texts())
	}

	s.downloads = []core.DownloadTask{{
		TaskId:      "ps-cancelled",
		Kind:        "polystrata",
		DisplayName: "PolyStrata · qwen36 · UD-Q4_K_M",
		State:       "cancelled",
		Stage:       "downloading_model",
		OutName:     "polystrata",
	}}
	tt.Frame()
	if tt.HasText("一键安装 PolyStrata") {
		t.Fatalf("starter card must stay hidden while unresolved cancelled PolyStrata task exists: %v", tt.Texts())
	}
}

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
			"estimated_download_bytes": 22663387424
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

	click(t, tt, "选择模型")
	if !tt.HasText("LFM2.5-24B-A2B (UD-Q3_K_XL)") {
		t.Fatalf("expected catalog models in PolyStrata picker popup: %v", tt.Texts())
	}
	if tt.HasText("Flint 40B-A2.5B") {
		t.Fatalf("darwin/arm64 picker must filter out CUDA-only Flint models: %v", tt.Texts())
	}
	click(t, tt, polystrataDrawerOptionSummary(starterOption{
		ModelTitle:             "LFM2.5-24B-A2B (UD-Q3_K_XL)",
		EstimatedDownloadBytes: 10947100928,
		RecommendedContext:     32768,
	}))
	click(t, tt, "ModelScope")
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
}

func TestStarterAndDeviceRecommendationLabelsLocalized(t *testing.T) {
	cases := []struct {
		name     string
		snapshot string
		want     []string
		rawKeys  []string
	}{
		{
			name: "blocked_insufficient_disk",
			snapshot: `{
				"starter_recommendation": {
					"definition": "starter-recommendation.v1",
					"status": "blocked",
					"reason": "insufficient_disk_space",
					"notes": ["apple_silicon_required", "macos_version_below_15"]
				}
			}`,
			want:    []string{"可用磁盘空间不足", "需要 Apple Silicon 芯片", "需要 macOS 15 或更高版本"},
			rawKeys: []string{"insufficient_disk_space", "apple_silicon_required", "macos_version_below_15"},
		},
		{
			name: "fallback_llama_cpp_with_notes",
			snapshot: `{
				"starter_recommendation": {
					"definition": "starter-recommendation.v1",
					"status": "recommended",
					"engine": "llama-cpp",
					"backend": "cpu",
					"model_id": "model-qwen3-0.6b-q8",
					"model_title": "Qwen3-0.6B-Q8_0 (GGUF)",
					"fit_level": "fallback",
					"recommended_context": 8192,
					"estimated_download_bytes": 639446688,
					"reason": "fallback_llama_cpp_insufficient_disk_for_polystrata",
					"notes": ["nvidia_driver_too_old", "host_available_memory_tight"]
				}
			}`,
			want:    []string{"当前磁盘空间不足以安装 PolyStrata", "NVIDIA 驱动版本过低", "当前可用内存偏紧"},
			rawKeys: []string{"fallback_llama_cpp_insufficient_disk_for_polystrata", "nvidia_driver_too_old", "host_available_memory_tight"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testState()
			s.device.Snapshot = json.RawMessage(tc.snapshot)
			s.ensureDownloadsUI().hostPlatform = "darwin/arm64"
			ttDownloads := ui.NewTester(s.downloadsPageView, 1120, 2200)
			ttDevice := ui.NewTester(s.deviceView, 1120, 1800)

			for name, texts := range map[string][]string{"downloads": ttDownloads.Texts(), "device": ttDevice.Texts()} {
				joined := strings.Join(texts, "\n")
				for _, want := range tc.want {
					if !strings.Contains(joined, want) {
						t.Fatalf("%s missing localized text %q in %v", name, want, texts)
					}
				}
				for _, raw := range tc.rawKeys {
					if strings.Contains(joined, raw) {
						t.Fatalf("%s leaked raw device label key %q in %v", name, raw, texts)
					}
				}
			}
		})
	}
}
