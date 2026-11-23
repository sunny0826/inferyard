package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestPolystrataEngineDrawerShowsSizeAndSHA256(t *testing.T) {
	s := testState()
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

	click(t, tt, "版本与地址 · polystrata")
	if !st.engineDrawerOpen || !tt.HasText("引擎详情") {
		t.Fatalf("expected polystrata engine drawer open: %v", tt.Texts())
	}
	if tt.HasText("SHA-256：未提供") {
		t.Fatalf("polystrata drawer must not display SHA-256：未提供: %v", tt.Texts())
	}
	entry, ok := core.CatalogEntryByID("engine-polystrata")
	if !ok || entry.SHA256 == "" || entry.Bytes <= 0 {
		t.Fatalf("unexpected catalog entry: ok=%v entry=%+v", ok, entry)
	}
	if !tt.HasText("SHA-256：" + entry.SHA256) {
		t.Fatalf("missing pinned SHA-256 in drawer: %v", tt.Texts())
	}
	if !tt.HasText(catalogHumanSize(entry.Bytes)) {
		t.Fatalf("missing archive size badge in drawer: %v", tt.Texts())
	}
	click(t, tt, "复制 SHA-256 · engine-polystrata-macos-arm64 · 0")
	if got := tt.Clipboard(); got != entry.SHA256 {
		t.Fatalf("clipboard SHA-256 = %q, want %q", got, entry.SHA256)
	}
}

func TestPolystrataStage2ProxyGuidanceOnNetworkFailure(t *testing.T) {
	proxyUnsetErr := core.Err("cli_blocked")
	proxyUnsetErr.Limitations = []string{"engine_acquire_incomplete"}
	proxyUnsetErr.Details = map[string]any{
		"failure_stage": "download_body",
		"failure_cause": "connection_error",
	}
	proxySetErr := core.Err("cli_blocked")
	proxySetErr.Limitations = []string{"engine_acquire_incomplete"}
	proxySetErr.Details = map[string]any{
		"failure_stage": "download_open",
		"failure_cause": "network_timeout",
	}
	transferErr := core.Err("cli_blocked")
	transferErr.Limitations = []string{"engine_acquire_incomplete"}
	transferErr.Details = map[string]any{
		"failure_stage": "download_open",
		"failure_cause": "transfer_error",
	}
	preflightErr := core.Err("cli_blocked")
	preflightErr.Limitations = []string{"polystrata_xcode_clt_missing"}
	preflightErr.Details = map[string]any{
		"failure_stage": "preflight",
		"failure_cause": "polystrata_xcode_clt_missing",
	}

	s := testState()
	s.downloads = []core.DownloadTask{
		{
			TaskId:      "ps-net-unset",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · qwen36",
			OutName:     "polystrata-1",
			State:       "failed",
			Stage:       "downloading_runtime",
			Error:       proxyUnsetErr,
		},
		{
			TaskId:      "ps-transfer",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · transfer",
			OutName:     "polystrata-2",
			State:       "failed",
			Stage:       "downloading_runtime",
			Error:       transferErr,
		},
		{
			TaskId:      "ps-preflight",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · preflight",
			OutName:     "polystrata-3",
			State:       "failed",
			Stage:       "preflight",
			Error:       preflightErr,
		},
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 1600)
	if !tt.HasText("设置 → 下载代理") || !tt.HasText("ModelScope") {
		t.Fatalf("expected Stage 2 unset-proxy guidance, got %v", tt.Texts())
	}
	if _, ok := tt.Find("前往设置代理 · ps-transfer"); ok {
		t.Fatalf("transfer_error must not show Stage 2 proxy guidance: %v", tt.Texts())
	}
	if _, ok := tt.Find("前往设置代理 · ps-preflight"); ok {
		t.Fatalf("preflight failure must not show Stage 2 proxy guidance: %v", tt.Texts())
	}

	// Verify configured proxy adjusts hint copy and button navigates to settings
	proxyURL := "http://127.0.0.1:7890"
	s.settings.Settings.DownloadProxy = &proxyURL
	s.downloads[0] = core.DownloadTask{
		TaskId:      "ps-net-set",
		Kind:        "polystrata",
		DisplayName: "PolyStrata · qwen36",
		OutName:     "polystrata-4",
		State:       "failed",
		Stage:       "downloading_runtime",
		Error:       proxySetErr,
	}
	tt.Frame()
	if !tt.HasText("请检查「设置 → 下载代理」配置是否可用后重试") {
		t.Fatalf("expected configured-proxy guidance copy, got %v", tt.Texts())
	}
	click(t, tt, "前往设置代理 · ps-net-set")
	if s.selected != "settings" {
		t.Fatalf("expected click to navigate to settings, got %q", s.selected)
	}
}

func TestPolystrataPreflightErrorDeduplicationLocalizedStagesAndDiskBytes(t *testing.T) {
	s := testState()
	xcodeErr := core.Err("cli_blocked")
	xcodeErr.Limitations = []string{"polystrata_xcode_clt_missing"}
	xcodeErr.Details = map[string]any{
		"failure_stage": "preflight",
		"failure_cause": "polystrata_xcode_clt_missing",
		"blockers": []map[string]any{{
			"code":        "polystrata_xcode_clt_missing",
			"message":     "Xcode Command Line Tools are required to build PolyStrata on macOS.",
			"fix_command": "xcode-select --install",
		}},
	}

	diskErr := core.Err("cli_blocked")
	diskErr.Limitations = []string{"polystrata_disk_insufficient"}
	diskErr.Details = map[string]any{
		"failure_stage": "preflight",
		"failure_cause": "polystrata_disk_insufficient",
		"stage":         "preflight",
		"blockers": []any{
			map[string]any{
				"code":           "polystrata_disk_insufficient",
				"message":        "Insufficient free disk space: 12.0 GiB available, at least 28.0 GiB required.",
				"required_bytes": int64(28 << 30),
				"free_bytes":     int64(12 << 30),
			},
			map[string]any{
				"code":           "insufficient_disk_space",
				"message":        "Insufficient disk space at --data-dir.",
				"required_bytes": float64(40 << 30),
				"free_bytes":     float64(10 << 30),
			},
		},
	}

	unknownErr := core.Err("polystrata_preflight_blocked")
	unknownErr.Details = map[string]any{
		"failure_stage": "setup_engine",
		"blockers": []map[string]any{{
			"code":    "custom_vendor_blocker",
			"message": "Custom vendor prerequisite missing.",
		}},
	}

	dupErr := core.Err("cli_blocked")
	dupErr.Limitations = []string{"network_timeout"}
	dupErr.Details = map[string]any{
		"failure_stage": "downloading_runtime",
		"failure_cause": "network_timeout",
	}

	s.downloads = []core.DownloadTask{
		{TaskId: "ps-xcode", Kind: "polystrata", DisplayName: "PolyStrata · xcode", OutName: "ps-1", State: "failed", Stage: "preflight", Error: xcodeErr},
		{TaskId: "ps-disk", Kind: "polystrata", DisplayName: "PolyStrata · disk", OutName: "ps-2", State: "failed", Stage: "preflight", Error: diskErr},
		{TaskId: "ps-unknown", Kind: "polystrata", DisplayName: "PolyStrata · unknown", OutName: "ps-3", State: "failed", Stage: "setup_engine", Error: unknownErr},
		{TaskId: "ps-dup", Kind: "polystrata", DisplayName: "PolyStrata · net", OutName: "ps-4", State: "failed", Stage: "downloading_runtime", Error: dupErr},
	}

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1120, 2600)
	tt.Frame()

	texts := tt.Texts()
	if got := exactTextCount(texts, "缺少 Xcode Command Line Tools，请先安装。"); got != 1 {
		t.Fatalf("expected localized xcode blocker once, got %d in %v", got, texts)
	}
	if tt.HasText("Xcode Command Line Tools are required to build PolyStrata on macOS.") {
		t.Fatalf("raw English xcode blocker message leaked: %v", texts)
	}
	if !tt.HasText("失败阶段：环境预检") || !tt.HasText("失败阶段：安装引擎环境") || !tt.HasText("失败阶段：下载运行时") {
		t.Fatalf("missing localized failure stages: %v", texts)
	}
	if tt.HasText("失败阶段：preflight") || tt.HasText("失败阶段：setup_engine") || tt.HasText("失败阶段：downloading_runtime") {
		t.Fatalf("raw failure stage token leaked: %v", texts)
	}
	if !tt.HasText("可用磁盘空间不足（可用 12.00 GiB，至少需要 28.00 GiB）。") || !tt.HasText("可用磁盘空间不足（可用 10.00 GiB，至少需要 40.00 GiB）。") {
		t.Fatalf("expected formatted disk byte details in blockers: %v", texts)
	}
	if got := exactTextCount(texts, "可用磁盘空间不足。"); got != 0 {
		t.Fatalf("expected base disk shortage line deduplicated when structured blocker renders byte figures, got %d: %v", got, texts)
	}
	if !tt.HasText("Custom vendor prerequisite missing.") {
		t.Fatalf("expected unknown blocker code to fall back to message: %v", texts)
	}
	if got := exactTextCount(texts, human("network_timeout")); got != 1 {
		t.Fatalf("expected matching limitation and failure_cause once, got %d: %v", got, texts)
	}
	if tt.HasText("stage") {
		t.Fatalf("duplicate raw stage detail leaked into kv-error-details: %v", texts)
	}

	click(t, tt, "复制修复命令 · 0")
	if got := tt.Clipboard(); got != "xcode-select --install" {
		t.Fatalf("clipboard = %q, want xcode-select --install", got)
	}
}
