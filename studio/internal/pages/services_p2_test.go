package pages

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func servicesP2State(t *testing.T) (*viewState, *core.Service) {
	t.Helper()
	svc := settingsTestService(t, t.TempDir())
	s := &viewState{
		ctx:                context.Background(),
		service:            svc,
		selected:           "services",
		loaded:             true,
		settingsLoaded:     true,
		serveSupported:     true,
		keepServicesOnQuit: true,
		denied:             map[string]bool{},
		settings:           svc.Settings(),
		ready: core.Readiness{
			CLI:       core.InferyardProbe{Status: "ok"},
			Workspace: core.DirectoryReadiness{Exists: true, Initialized: true},
			Results:   core.DirectoryReadiness{Exists: true},
			Models:    core.DirectoryReadiness{Exists: true},
		},
		registry: core.Registry{
			Models: []core.ModelAsset{
				{Name: "Qwen3-8B-Q4.gguf", Format: "gguf", Available: true, Path: "/models/Qwen3-8B-Q4.gguf"},
				{Name: "Llama-3-8B.gguf", Format: "gguf", Available: true, Path: "/models/Llama-3-8B.gguf"},
			},
			Engines: []core.EngineAsset{
				{Name: "llama-cpp-default", Family: "llama-cpp", Path: "/engines/llama-server"},
			},
			DefaultEngine: "llama-cpp-default",
		},
		checks: core.EngineCheckOverview{
			Supported: true,
			Engines: map[string]core.EngineEligibility{
				"llama-cpp-default": {State: core.EngineCheckEligible},
			},
		},
	}
	s.loadSettings(s.settings)
	return s, svc
}

func newServicesTester(s *viewState) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		s.view(c)
	}, 1280, 1400)
}

func TestServicesExitedCardAndSidebarAlert(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	exitCode := 137
	gpu := -1
	s.selected = "benchmarks"
	s.exitedNotice = &core.ServiceExitNotice{
		ID:          "exit-1",
		Model:       "Qwen3-8B-Q4.gguf",
		Engine:      "llama-cpp-default",
		Port:        8081,
		ContextSize: 16384,
		GPULayers:   &gpu,
		LogPath:     "/tmp/service.log",
		ExitedAt:    "2026-11-25T05:10:00Z",
		ExitCode:    &exitCode,
		WhileAway:   true,
		LogTail:     []string{"ggml_metal: out of memory"},
		Hint:        l10n.TR("core.service.exit_hint.oom"),
	}

	tt := newServicesTester(s)
	tt.Frame()

	// Sidebar footer should show exited alert and clicking navigates to services.
	click(t, tt, "服务意外退出")
	tt.Frame()
	if s.selected != "services" {
		t.Fatalf("expected clicking sidebar exited row to navigate to services, got %q", s.selected)
	}

	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"服务意外退出", "离开期间退出", "退出码 137", "Qwen3-8B-Q4.gguf", "ggml_metal: out of memory"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected exited card to contain %q, got:\n%s", want, texts)
		}
	}

	// Clicking Adjust Params opens params drawer prefilled from exit notice.
	click(t, tt, "调整参数后启动")
	tt.Frame()
	if !s.serviceParams.open || s.serviceParams.model != "Qwen3-8B-Q4.gguf" || s.serviceParams.portText != "8081" || s.serviceParams.gpuLayersText != "-1" {
		t.Fatalf("unexpected params drawer state from exit notice: %+v", s.serviceParams)
	}
	s.serviceParams.open = false
	tt.Frame()

	// Clicking Got It dismisses notice.
	click(t, tt, "知道了")
	tt.Frame()
	if s.hasActiveExitNotice() {
		t.Fatal("expected exit notice to be dismissed")
	}
}

func TestServicesRecentAndAdoptedCard(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.engineState = core.EngineState{
		State:       "running",
		Origin:      core.EngineOriginStandalone,
		Adopted:     true,
		EngineName:  "llama-cpp-default",
		ModelName:   "Qwen3-8B-Q4.gguf",
		Model:       "/models/Qwen3-8B-Q4.gguf",
		Port:        8080,
		ContextSize: 32768,
		Endpoint:    "http://127.0.0.1:8080",
		LogPath:     "/tmp/svc.log",
		Pid:         12345,
	}
	s.recentServices = []core.ServicePreset{
		{Model: "Qwen3-8B-Q4.gguf", Engine: "llama-cpp-default", Port: 8080, ContextSize: 32768},
		{Model: "Llama-3-8B.gguf", Engine: "llama-cpp-default", Port: 0, ContextSize: 8192},
	}

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"已接管", "最近启动过", "Llama-3-8B.gguf", "端口 8080", "自动端口", "调用示例", "启动参数"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected services view to contain %q, got:\n%s", want, texts)
		}
	}
}

func TestServicesUsageDrawerSnippetsCopyAndProbe(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer srv.Close()

	s, _ := servicesP2State(t)
	uiQueue := make(chan func(), 4)
	s.updateWindow = func(fn func()) { uiQueue <- fn }
	s.openServiceUsageDrawer(srv.URL, "Qwen3-8B-Q4.gguf", "llama-cpp-default")

	tt := newServicesTester(s)
	tt.Frame()

	wantBase := srv.URL + "/v1"
	if s.serviceUsage.baseURL != wantBase {
		t.Fatalf("baseURL = %q, want %q", s.serviceUsage.baseURL, wantBase)
	}

	// Switch snippet tabs: curl -> Python -> Environment.
	click(t, tt, "Python")
	tt.Frame()
	if s.serviceUsage.tab != "python" {
		t.Fatalf("expected python tab, got %q", s.serviceUsage.tab)
	}
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "from openai import OpenAI") {
		t.Fatal("expected Python OpenAI snippet")
	}

	click(t, tt, "环境变量")
	tt.Frame()
	if s.serviceUsage.tab != "env" {
		t.Fatalf("expected env tab, got %q", s.serviceUsage.tab)
	}

	// Copy API Key.
	click(t, tt, "复制 API Key")
	tt.Frame()
	if got := tt.Clipboard(); got != serviceUsageDefaultAPIKey {
		t.Fatalf("clipboard = %q, want %q", got, serviceUsageDefaultAPIKey)
	}

	// Run connectivity test.
	click(t, tt, "发一条测试请求")
	tt.Frame()
	select {
	case fn := <-uiQueue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for connectivity test result")
	}
	tt.Frame()
	if s.serviceUsage.testResult == nil || !s.serviceUsage.testResult.OK || s.serviceUsage.testResult.StatusCode != 200 || s.serviceUsage.testResult.Reply != "OK" {
		t.Fatalf("unexpected probe result: %+v", s.serviceUsage.testResult)
	}
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, "HTTP 200") || !strings.Contains(texts, "OK") {
		t.Fatalf("expected probe success text in UI, got:\n%s", texts)
	}
}

func TestServicesParamsDrawerPresetAutoFillAndSave(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, svc := servicesP2State(t)
	gpu := 24
	threads := 8
	if err := svc.SaveServicePreset(core.ServicePreset{
		Model:       "Qwen3-8B-Q4.gguf",
		Engine:      "llama-cpp-default",
		Port:        9090,
		ContextSize: 16384,
		GPULayers:   &gpu,
		Threads:     &threads,
		ExtraArgs:   []string{"--flash-attn", "--alias", "my model"},
	}); err != nil {
		t.Fatalf("SaveServicePreset: %v", err)
	}

	s.openServiceParamsDrawerForNew()
	tt := newServicesTester(s)
	tt.Frame()

	if s.serviceParams.portText != "9090" || s.serviceParams.contextPreset != "16384" || s.serviceParams.gpuLayersText != "24" || s.serviceParams.threadsText != "8" {
		t.Fatalf("expected auto-filled preset params, got %+v", s.serviceParams)
	}

	// Advanced section is collapsed by default; expand it and verify fields appear.
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "GPU 层数") {
		t.Fatal("expected advanced parameters to be collapsed by default")
	}
	click(t, tt, "高级参数")
	tt.Frame()
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "GPU 层数") {
		t.Fatal("expected GPU layers field after expanding advanced parameters")
	}

	// Modify port and click Save Params Only.
	s.serviceParams.portText = "9191"
	tt.Frame()
	click(t, tt, "仅保存参数")
	tt.Frame()

	if s.serviceParams.open {
		t.Fatal("expected params drawer to close after Save Params Only")
	}
	saved, ok := svc.ServicePresetFor("Qwen3-8B-Q4.gguf", "llama-cpp-default")
	if !ok || saved.Port != 9191 {
		t.Fatalf("expected updated preset port 9191, got ok=%v preset=%+v", ok, saved)
	}
}

func TestServicesQuitDialogAndPolicy(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.engineState = core.EngineState{
		State:     "running",
		Origin:    core.EngineOriginStandalone,
		ModelName: "Qwen3-8B-Q4.gguf",
	}

	// When minimize-to-tray is enabled, quit dialog must not intercept close.
	s.minimizeToTray = true
	if s.shouldShowQuitServiceDialog() {
		t.Fatal("expected quit dialog suppressed when minimizeToTray is enabled")
	}

	// When minimize-to-tray is disabled and standalone service is running, intercept close.
	s.minimizeToTray = false
	if !s.shouldShowQuitServiceDialog() {
		t.Fatal("expected quit dialog to intercept close when standalone service is running")
	}

	closed := false
	s.quitDialog.closeWindow = func() { closed = true }
	s.handleWindowClose(nil)
	if !s.quitDialog.open {
		t.Fatal("expected quitDialog.open=true after handleWindowClose")
	}

	tt := newServicesTester(s)
	tt.Frame()
	click(t, tt, "保持服务运行并退出")
	tt.Frame()
	if !closed || !s.quitDialog.bypassClose || s.quitDialog.open {
		t.Fatalf("expected window closed with bypassClose=true, closed=%v dialog=%+v", closed, s.quitDialog)
	}
}

func TestSettingsKeepServicesOnQuitSwitchPersists(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, svc := servicesP2State(t)
	s.selected = "settings"
	uiQueue := make(chan func(), 4)
	s.updateWindow = func(fn func()) { uiQueue <- fn }

	tt := settingsTester(s)
	tt.Frame()
	if !s.keepServicesOnQuit || !svc.EffectiveKeepServicesOnQuit() {
		t.Fatal("expected keepServicesOnQuit default true")
	}

	click(t, tt, "退出时保持本地服务运行")
	if s.keepServicesOnQuit {
		t.Fatal("expected keepServicesOnQuit toggled off optimistically")
	}
	select {
	case fn := <-uiQueue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for keep_services_on_quit persist")
	}
	tt.Frame()
	if svc.EffectiveKeepServicesOnQuit() {
		t.Fatal("expected keep_services_on_quit=false persisted to service")
	}
	// Verify JSON serialization roundtrip on splitExtraArgs helper too.
	args, ok := splitExtraArgs(`--flash-attn --alias "hello world"`)
	if !ok || len(args) != 3 || args[2] != "hello world" {
		t.Fatalf("unexpected splitExtraArgs result: ok=%v args=%#v", ok, args)
	}
	raw, _ := json.Marshal(args)
	if !strings.Contains(string(raw), "hello world") {
		t.Fatalf("unexpected json: %s", raw)
	}
}
