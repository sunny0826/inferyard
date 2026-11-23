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
	proxyUnsetErr := core.Err("engine_acquire_incomplete")
	proxyUnsetErr.Details = map[string]any{
		"failure_stage": "downloading_runtime",
		"failure_cause": "connection_error",
	}
	proxySetErr := core.Err("engine_acquire_incomplete")
	proxySetErr.Details = map[string]any{
		"failure_stage": "download_open",
		"failure_cause": "network_timeout",
	}
	preflightErr := core.Err("polystrata_preflight_blocked")
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
			TaskId:      "ps-preflight",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · preflight",
			OutName:     "polystrata-2",
			State:       "failed",
			Stage:       "preflight",
			Error:       preflightErr,
		},
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 1400)
	if !tt.HasText("设置 → 下载代理") || !tt.HasText("ModelScope") {
		t.Fatalf("expected Stage 2 unset-proxy guidance, got %v", tt.Texts())
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
		OutName:     "polystrata-3",
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
