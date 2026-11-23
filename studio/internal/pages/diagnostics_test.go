package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type stubPreviewMode struct {
	noticed bool
}

func (p *stubPreviewMode) showNotice()              { p.noticed = true }
func (p *stubPreviewMode) dispatch(previewIntent)   {}
func (p *stubPreviewMode) resetTask(string, string) {}
func (p *stubPreviewMode) detail(string) *core.RunTaskDetail {
	return nil
}
func (p *stubPreviewMode) report(string) (*core.ReportIndex, *core.ApiError) {
	return nil, nil
}
func (p *stubPreviewMode) resolveModelURL(string) inferyard.ModelResolve {
	return inferyard.ModelResolve{}
}
func (p *stubPreviewMode) detectedEngines() []inferyard.DetectedEngine { return nil }
func (p *stubPreviewMode) downloadLogTail(string) string               { return "" }

func TestHumanTranslatesDiagnosticAndLocalEngineCodes(t *testing.T) {
	codes := []string{
		"ac_power_required",
		"model_acquire_incomplete",
		"engine_acquire_incomplete",
		"engine_checksum_mismatch",
		"chat_template_unavailable",
		"token_env_missing",
		"auth_required",
		"not_found",
		"rate_limited",
		"upstream_http_error",
		"unexpected_content_encoding",
		"network_timeout",
		"connection_error",
		"transfer_error",
		"disk_full",
		"permission_denied",
		"local_engine_requires_darwin_arm64",
		"local_engine_json_unreadable",
		"local_engine_absolute_path_required",
		"local_engine_locator_missing",
		"local_engine_runtime_root_invalid",
		"local_engine_cwd_invalid",
		"local_engine_executable_invalid",
		"local_engine_models_root_required_for_lmstudio_only",
		"local_engine_installation_unreadable",
		"local_engine_non_regular_asset",
		"local_engine_directory_symlink",
		"local_engine_python_venv_required",
		"local_engine_python_probe_failed",
		"local_engine_runtime_changed_during_hash",
		"local_engine_runtime_unreadable",
		"local_engine_runtime_invalid",
		"local_engine_runtime_changed",
		"local_engine_python_import_path_invalid",
		"local_engine_import_root_undeclared",
		"local_engine_python_entrypoint_invalid",
		"local_engine_python_entrypoint_missing",
		"local_engine_mapping_unverified",
		"local_engine_macho_unreadable",
		"local_engine_mapped_library_closure_unverified",
		"polystrata_preflight_blocked",
		"polystrata_setup_failed",
		"local_engine_data_dir_invalid",
	}
	for _, code := range codes {
		got := human(code)
		if got == "" || strings.HasPrefix(got, "操作失败（") {
			t.Errorf("human(%q) returned fallback or empty: %q", code, got)
		}
	}
}

func TestServiceViewEngineLogTailAndOpenAction(t *testing.T) {
	ws := t.TempDir()
	logsDir := filepath.Join(ws, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logsDir, "engine-failed.log")
	var b strings.Builder
	for i := 1; i <= 25; i++ {
		fmt.Fprintf(&b, "engine-log-line-%02d\r\n", i)
	}
	if err := os.WriteFile(logPath, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(core.UserSettings{WorkspaceRoot: &ws})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := core.New(context.Background(), core.Options{SettingsPath: settingsPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	origOpen := openLogPathFunc
	t.Cleanup(func() { openLogPathFunc = origOpen })
	opened := ""
	openLogPathFunc = func(path string) error {
		opened = path
		return nil
	}

	s := failedBindState()
	s.service = svc
	s.engineState.LogPath = logPath

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.serviceView(c) })
	}, 1120, 1600)

	if !tt.HasText("查看引擎日志尾部") || !tt.HasText("打开日志文件") {
		t.Fatalf("missing log diagnostics controls: %v", tt.Texts())
	}
	if tt.HasText("engine-log-line-25") {
		t.Fatalf("log tail should be collapsed initially: %v", tt.Texts())
	}

	click(t, tt, "查看引擎日志尾部")
	tt.Frame()
	if !tt.HasText("engine-log-line-06") || !tt.HasText("engine-log-line-25") {
		t.Fatalf("expected trailing 20 lines after expanding fold: %v", tt.Texts())
	}
	if tt.HasText("engine-log-line-05") {
		t.Fatalf("expected earlier lines beyond 20 to be omitted: %v", tt.Texts())
	}

	click(t, tt, "打开日志文件")
	if opened != logPath {
		t.Fatalf("openLogPathFunc called with %q, want %q", opened, logPath)
	}
}

func TestServiceViewEngineLogPreviewModeDoesNoIO(t *testing.T) {
	origOpen := openLogPathFunc
	t.Cleanup(func() { openLogPathFunc = origOpen })
	called := false
	openLogPathFunc = func(string) error {
		called = true
		return nil
	}

	s := failedBindState()
	preview := &stubPreviewMode{}
	s.preview = preview
	s.engineState.LogPath = "/nonexistent/preview/engine.log"

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.serviceView(c) })
	}, 1120, 1600)

	click(t, tt, "查看引擎日志尾部")
	tt.Frame()
	if !tt.HasText("预览模式无本地日志。") {
		t.Fatalf("preview placeholder missing: %v", tt.Texts())
	}

	click(t, tt, "打开日志文件")
	if called || preview.noticed {
		t.Fatalf("preview mode should be a silent no-op (called=%v noticed=%v)", called, preview.noticed)
	}
}

func TestCandidateFieldsMacOSBatteryWarning(t *testing.T) {
	cases := []struct {
		name     string
		snapshot string
		want     bool
	}{
		{
			name:     "darwin on battery",
			snapshot: `{"hardware":{"platform":"Darwin","ac_online":false}}`,
			want:     true,
		},
		{
			name:     "darwin on ac power",
			snapshot: `{"hardware":{"platform":"Darwin","ac_online":true}}`,
			want:     false,
		},
		{
			name:     "darwin ac unknown",
			snapshot: `{"hardware":{"platform":"Darwin","ac_online":null}}`,
			want:     false,
		},
		{
			name:     "linux on battery",
			snapshot: `{"hardware":{"platform":"Linux","ac_online":false}}`,
			want:     false,
		},
		{
			name:     "invalid json",
			snapshot: `{broken`,
			want:     false,
		},
	}

	const warningMsg = "当前使用电池供电，macOS 测评需接通电源后开始。"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testState()
			s.preview = &stubPreviewMode{}
			s.device.Snapshot = json.RawMessage(tc.snapshot)
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Children(func() { s.candidateFields(c) })
			}, 1120, 1600)

			if got := tt.HasText(warningMsg); got != tc.want {
				t.Fatalf("warning visible = %v, want %v; texts=%v", got, tc.want, tt.Texts())
			}
			if tc.want {
				bannerRect, ok1 := tt.Find(warningMsg)
				startRect, ok2 := tt.Find(termStartBenchmark)
				if !ok1 || !ok2 || bannerRect.Y >= startRect.Y {
					t.Fatalf("warning banner should appear above start button: banner=%+v start=%+v", bannerRect, startRect)
				}
			}
		})
	}
}

func TestDownloadErrorViewRendersReadableCauseAndMappedStage(t *testing.T) {
	s := testState()
	s.downloads = []core.DownloadTask{{
		TaskId:  "dl-1",
		Kind:    "model",
		OutName: "model-1",
		State:   "failed",
		Error: &core.ApiError{
			Code:        "cli_blocked",
			Message:     "inferyard 拒绝执行",
			Limitations: []string{"model_acquire_incomplete"},
			Details: map[string]any{
				"failure_stage": "download_open",
				"failure_cause": "auth_required",
			},
		},
	}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.downloadsView(c) })
	}, 1000, 1200)

	texts := tt.Texts()
	wantCause := human("auth_required")
	wantStage := "失败阶段：建立下载连接"
	if !tt.HasText(human("model_acquire_incomplete")) {
		t.Fatalf("missing limitation label: %v", texts)
	}
	if !tt.HasText(wantCause) || !tt.HasText(wantStage) {
		t.Fatalf("missing readable cause/stage: %v", texts)
	}
	for _, raw := range []string{"failure_stage", "failure_cause", "auth_required", "download_open"} {
		if tt.HasText(raw) {
			t.Fatalf("raw diagnostic key/code %q leaked into UI: %v", raw, texts)
		}
	}

	causeIdx, stageIdx := -1, -1
	for i, txt := range texts {
		if txt == wantCause && causeIdx == -1 {
			causeIdx = i
		}
		if txt == wantStage && stageIdx == -1 {
			stageIdx = i
		}
	}
	if causeIdx < 0 || stageIdx < 0 || causeIdx >= stageIdx {
		t.Fatalf("expected readable cause (%d) before stage metadata (%d): %v", causeIdx, stageIdx, texts)
	}
}
