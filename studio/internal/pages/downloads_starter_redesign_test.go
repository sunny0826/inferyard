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

func TestDownloadSourceSyncedAcrossModelsTabDetailDrawerAndStarter(t *testing.T) {
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

	click(t, tt, "HuggingFace")
	if st.polystrataModelSource != "huggingface" || st.modelSource != "huggingface" {
		t.Fatalf("expected huggingface sync from starter card: poly=%q direct=%q", st.polystrataModelSource, st.modelSource)
	}

	clickDownloadsTab(t, tt, "模型")
	clickLastSegmented(t, tt, "ModelScope")
	if st.modelSource != "modelscope" || st.polystrataModelSource != "modelscope" {
		t.Fatalf("Models tab ModelScope click failed to sync polystrataModelSource: direct=%q poly=%q", st.modelSource, st.polystrataModelSource)
	}

	click(t, tt, "模型详情 · model-qwen3-4b-q4km")
	clickLastSegmented(t, tt, "HuggingFace")
	if st.modelSource != "huggingface" || st.polystrataModelSource != "huggingface" {
		t.Fatalf("model detail drawer HuggingFace click failed to sync polystrataModelSource: direct=%q poly=%q", st.modelSource, st.polystrataModelSource)
	}
}

func TestStarterHiddenConsistentWithDownloadResolvedAcrossDistinctModels(t *testing.T) {
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
	s.downloads = []core.DownloadTask{
		{
			TaskId:      "ps-b-done",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · lfm25 · UD-Q3_K_XL",
			State:       "done",
			OutName:     "polystrata",
		},
		{
			TaskId:      "ps-a-failed",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · qwen36 · UD-Q4_K_M",
			State:       "failed",
			Stage:       "downloading_model",
			OutName:     "polystrata",
		},
	}
	if s.unresolvedDownloadFailures() != 1 {
		t.Fatalf("expected task A to remain unresolved when newer done task has different DisplayName, got %d", s.unresolvedDownloadFailures())
	}
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	if tt.HasText("一键安装 PolyStrata") {
		t.Fatalf("starter card must stay hidden whenever downloadResolved considers task A unresolved: %v", tt.Texts())
	}
}

