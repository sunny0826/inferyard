package pages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestPolystrataTaskCardPhaseBarCompactDetailAndTopLevelLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), ".inferyard-setup.log")
	if err := os.WriteFile(logPath, []byte("pip install numpy\n[50%] Linking CXX executable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	longDetail := "Building CXX object external/llama.cpp/ggml/src/ggml-metal/CMakeFiles/ggml-metal.dir/ggml-metal.m.o"

	s := testState()
	s.downloads = []core.DownloadTask{{
		TaskId:          "ps-live",
		Kind:            "polystrata",
		DisplayName:     "PolyStrata · qwen36 · UD-Q4_K_M",
		State:           "downloading",
		Stage:           "downloading_model",
		Step:            ptr(4),
		StepTotal:       ptr(4),
		DownloadedBytes: 512 << 20,
		TotalBytes:      ptr(int64(1024 << 20)),
		BytesPerSecond:  ptr(float64(64 << 20)),
		Detail:          ptr(longDetail),
		OutName:         "polystrata",
		LogPath:         ptr(logPath),
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1120, 2200)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	for _, want := range []string{"4 / 4", "下载模型", "512.00 MiB / 1.00 GiB", "50.0%", "64.00 MiB/s", "剩余", "✓  预检", "✓  获取运行时", "✓  构建环境", "模型与核验", longDetail, "查看安装日志"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in active PolyStrata task card: %v", want, tt.Texts())
		}
	}
	if _, ok := tt.Find("PolyStrata 安装阶段"); !ok {
		t.Fatalf("missing phaseProgress row label: %v", tt.Texts())
	}
	if _, ok := tt.Find("安装阶段进度"); ok {
		t.Fatalf("stage progress bar should be hidden on PolyStrata card: %v", tt.Texts())
	}
	if tt.HasText("任务详情") {
		t.Fatalf("PolyStrata card must not render 任务详情 fold: %v", tt.Texts())
	}
	// Long detail must be rendered as compact single-line caption, never promoted to metric tile "当前".
	if tt.HasText("当前") {
		t.Fatalf("detail must not render in giant metric tile: %v", tt.Texts())
	}

	if !tt.HasText("安装中") {
		t.Fatalf("expected install-oriented status on merged header button: %v", tt.Texts())
	}
	if _, ok := tt.Find("取消安装"); !ok {
		t.Fatalf("expected merged header button to expose 取消安装 action: %v", tt.Texts())
	}
	if tt.HasText("下载中") || tt.HasText("取消下载") {
		t.Fatalf("active PolyStrata card must not use download-oriented labels: %v", tt.Texts())
	}

	click(t, tt, "查看安装日志")
	if !tt.HasText("[50%] Linking CXX executable") {
		t.Fatalf("expected top-level install log content: %v", tt.Texts())
	}
	click(t, tt, "复制安装日志")
	if got := tt.Clipboard(); got != "pip install numpy\n[50%] Linking CXX executable" {
		t.Fatalf("unexpected copied install log: %q", got)
	}

	// Stage transition to verifying_installation updates UI without stalling at done.
	s.downloads[0].Stage = "verifying_installation"
	s.downloads[0].TotalBytes = nil
	s.downloads[0].DownloadedBytes = 0
	s.downloads[0].BytesPerSecond = nil
	s.downloads[0].Percent = nil
	s.downloads[0].Detail = ptr("注册并核验引擎安装")
	tt.Frame()
	if !tt.HasText("核验安装") || !tt.HasText("注册并核验引擎安装") {
		t.Fatalf("expected verifying_installation stage and detail: %v", tt.Texts())
	}

	click(t, tt, "取消安装")
	if !tt.HasText("确认取消安装") {
		t.Fatalf("expected install cancel confirmation label: %v", tt.Texts())
	}
}

func TestPolystrataFailedAndCancelledRecoveryActionsOpenDrawerOrResume(t *testing.T) {
	for _, tc := range []struct {
		state      string
		wantButton string
	}{
		{state: "failed", wantButton: "重试安装"},
		{state: "cancelled", wantButton: "继续安装"},
	} {
		t.Run(tc.state, func(t *testing.T) {
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
			s.downloads = []core.DownloadTask{{
				TaskId:      "ps-" + tc.state,
				Kind:        "polystrata",
				DisplayName: "PolyStrata · qwen36 · UD-Q4_K_M",
				State:       tc.state,
				Stage:       "downloading_model",
				OutName:     "polystrata",
			}}
			stub := &resolvePreviewStub{}
			s.preview = stub
			st := s.ensureDownloadsUI()
			st.hostPlatform = "darwin/arm64"
			tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

			if !tt.HasText(tc.wantButton) || !tt.HasText("更换方案或下载源") {
				t.Fatalf("missing PolyStrata recovery actions for %s task: %v", tc.state, tt.Texts())
			}
			click(t, tt, tc.wantButton)
			if len(stub.intents) != 1 {
				t.Fatalf("expected 1 retry intent, got %#v", stub.intents)
			}
			if retry, ok := stub.intents[0].(previewRetryDownload); !ok || retry.id != "ps-"+tc.state {
				t.Fatalf("unexpected retry intent: %#v", stub.intents[0])
			}

			stub.intents = nil
			click(t, tt, "更换方案或下载源")
			if !st.engineDrawerOpen || st.engineDrawerFamilyID != "polystrata" {
				t.Fatalf("expected PolyStrata config drawer opened from failed card: open=%v fam=%q", st.engineDrawerOpen, st.engineDrawerFamilyID)
			}
			click(t, tt, "选择模型")
			click(t, tt, polystrataDrawerOptionSummary(starterOption{
				ModelTitle:             "LFM2.5-24B-A2B (UD-Q3_K_XL)",
				EstimatedDownloadBytes: 10947100928,
				RecommendedContext:     32768,
			}))
			click(t, tt, "ModelScope")
			click(t, tt, "安装此版本 · polystrata")
			if st.engineDrawerOpen || len(stub.intents) != 1 {
				t.Fatalf("expected confirmed new PolyStrata setup intent and drawer close: open=%v intents=%#v", st.engineDrawerOpen, stub.intents)
			}
			req := stub.intents[0].(previewPolystrataSetup).req
			if req.Family != "lfm25" || req.Model != "UD-Q3_K_XL" || req.ModelSource != "modelscope" {
				t.Fatalf("unexpected recovery PolyStrata setup request: %+v", req)
			}
		})
	}
}

func TestPolystrataPhaseIndexAndRecoveryDrawerRestoresModelSource(t *testing.T) {
	if got := polystrataPhaseIndex(core.DownloadTask{
		State:     "downloading",
		Stage:     "setup_engine",
		Step:      ptr(4),
		StepTotal: ptr(4),
	}); got != 3 {
		t.Fatalf("expected valid 4-stage Step=4 to win over Stage=setup_engine, got %d", got)
	}
	if got := polystrataPhaseIndex(core.DownloadTask{
		State: "downloading",
		Stage: "setup_engine",
	}); got != 2 {
		t.Fatalf("expected Stage=setup_engine fallback without Step to return 2, got %d", got)
	}

	s := testState()
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	st.setPolystrataModelSource("huggingface")
	s.downloads = []core.DownloadTask{{
		TaskId:      "ps-restore-src",
		Kind:        "polystrata",
		DisplayName: "PolyStrata · qwen36 · UD-Q4_K_M",
		State:       "failed",
		Stage:       "downloading_model",
		ModelSource: "modelscope",
		OutName:     "polystrata",
	}}
	stub := &resolvePreviewStub{}
	s.preview = stub
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

	click(t, tt, "更换方案或下载源")
	if !st.engineDrawerOpen || st.polystrataModelSource != "modelscope" {
		t.Fatalf("expected recovery drawer to restore ModelSource=modelscope, got open=%v src=%q", st.engineDrawerOpen, st.polystrataModelSource)
	}
	click(t, tt, "安装此版本 · polystrata")
	if len(stub.intents) != 1 {
		t.Fatalf("expected 1 setup intent, got %#v", stub.intents)
	}
	req := stub.intents[0].(previewPolystrataSetup).req
	if req.ModelSource != "modelscope" {
		t.Fatalf("expected restored ModelSource=modelscope in submitted setup request, got %+v", req)
	}
}

func TestPolystrataTaskCardZeroRoundedAndUnknownTotalProgress(t *testing.T) {
	s := testState()
	s.downloads = []core.DownloadTask{{
		TaskId:          "ps-small",
		Kind:            "polystrata",
		DisplayName:     "PolyStrata · qwen36 · UD-Q4_K_M",
		State:           "downloading",
		Stage:           "downloading_model",
		Step:            ptr(4),
		StepTotal:       ptr(4),
		DownloadedBytes: 0,
		Percent:         ptr(100.0),
		Detail:          ptr("chat template"),
		OutName:         "polystrata",
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1120, 2200)
	tt.Frame()

	if !tt.HasText("100%") || !tt.HasText("chat template") {
		t.Fatalf("expected 100%% and chat template for zero-rounded download, got %v", tt.Texts())
	}
	if tt.HasText("0 B / 1 B") || tt.HasText("0.0%") {
		t.Fatalf("must not render synthetic 0 B / 1 B for zero-rounded download: %v", tt.Texts())
	}

	s.downloads[0].DownloadedBytes = 8_400_000
	s.downloads[0].Percent = nil
	s.downloads[0].Detail = ptr("next-shard.gguf")
	tt.Frame()
	wantBytes := formatDownloadBytes(8_400_000)
	if !tt.HasText("下载模型") || !tt.HasText(wantBytes) || !tt.HasText("next-shard.gguf") {
		t.Fatalf("expected unknown-total download card with %q and next-shard.gguf, got %v", wantBytes, tt.Texts())
	}
}

func TestPolystrataHeaderCancelFlowAndLogToggleEmptyState(t *testing.T) {
	emptyLog := filepath.Join(t.TempDir(), ".inferyard-setup.log")
	if err := os.WriteFile(emptyLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stub := &resolvePreviewStub{}
	s := testState()
	s.preview = stub
	s.downloads = []core.DownloadTask{{
		TaskId:          "ps-cancel-header",
		Kind:            "polystrata",
		DisplayName:     "PolyStrata · qwen · Q2_0",
		State:           "downloading",
		Stage:           "downloading_model",
		Step:            ptr(4),
		StepTotal:       ptr(4),
		DownloadedBytes: 4 << 30,
		TotalBytes:      ptr(int64(35 << 30)),
		OutName:         "polystrata-qwen",
		LogPath:         ptr(emptyLog),
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1120, 2200)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	// Toggle empty log open and closed.
	click(t, tt, "查看安装日志")
	if !tt.HasText("收起安装日志") || !tt.HasText("暂无可显示的日志内容。") {
		t.Fatalf("expected expanded empty log state: %v", tt.Texts())
	}
	if _, ok := tt.Find("复制安装日志"); ok {
		t.Fatalf("empty log must not show copy button: %v", tt.Texts())
	}
	click(t, tt, "查看安装日志")
	if tt.HasText("暂无可显示的日志内容。") {
		t.Fatalf("expected log panel to collapse after second toggle: %v", tt.Texts())
	}

	// Open cancel popover from merged header button, verify button switches to cancel label, then confirm.
	click(t, tt, "取消安装")
	if !tt.HasText("取消安装") || !tt.HasText("确认取消安装") || !tt.HasText("继续安装") {
		t.Fatalf("expected merged header button and confirm popover: %v", tt.Texts())
	}
	click(t, tt, "确认取消安装")
	if len(stub.intents) != 1 {
		t.Fatalf("expected 1 cancel intent, got %#v", stub.intents)
	}
	cancelIntent, ok := stub.intents[0].(previewCancelDownload)
	if !ok || cancelIntent.id != "ps-cancel-header" {
		t.Fatalf("unexpected cancel intent: %#v", stub.intents[0])
	}
}
