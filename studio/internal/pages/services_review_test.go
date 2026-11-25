package pages

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestBuildUsageSnippetEscapingAndServedModelPriority(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	model := `qwen's "test" $(id)`
	baseURL := "http://127.0.0.1:8080/v1"

	curlSnippet := buildUsageSnippet("curl", baseURL, model)
	if !strings.Contains(curlSnippet, `'http://127.0.0.1:8080/v1/chat/completions'`) {
		t.Fatalf("expected single-quoted URL in curl snippet:\n%s", curlSnippet)
	}
	if !strings.Contains(curlSnippet, `'\''`) {
		t.Fatalf("expected shell-escaped single quote in curl snippet:\n%s", curlSnippet)
	}
	if !strings.Contains(curlSnippet, `\"test\"`) {
		t.Fatalf("expected JSON-escaped double quotes in curl snippet:\n%s", curlSnippet)
	}

	envSnippet := buildUsageSnippet("env", baseURL, model)
	if !strings.Contains(envSnippet, `export OPENAI_MODEL='qwen'\''s "test" $(id)'`) {
		t.Fatalf("expected single-quoted env export, got:\n%s", envSnippet)
	}

	pySnippet := buildUsageSnippet("python", baseURL, model)
	if !strings.Contains(pySnippet, `model="qwen's \"test\" $(id)"`) {
		t.Fatalf("expected JSON-encoded python string literal, got:\n%s", pySnippet)
	}

	// Opening usage drawer from managed card prefers ServedModel over ModelName.
	s, _ := servicesP2State(t)
	s.engineState = core.EngineState{
		State:       "running",
		Origin:      core.EngineOriginStandalone,
		EngineName:  "llama-cpp-default",
		ModelName:   "Qwen3-8B-Q4.gguf",
		ServedModel: "qwen3-served-alias",
		Endpoint:    "http://127.0.0.1:8080",
		Pid:         1001,
	}
	tt := newServicesTester(s)
	tt.Frame()
	click(t, tt, "调用示例")
	tt.Frame()
	if !s.serviceUsage.open || s.serviceUsage.model != "qwen3-served-alias" {
		t.Fatalf("expected usage drawer to use ServedModel, got open=%v model=%q", s.serviceUsage.open, s.serviceUsage.model)
	}
}

func TestServicesParamsDrawerCredentialRejectionAndMemoryAndEngineCompat(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, svc := servicesP2State(t)
	sizeBytes := int64(32 * (1 << 30))
	s.registry.Models[0].SizeBytes = &sizeBytes
	s.device.Snapshot = json.RawMessage(`{"hardware":{"memory_available_bytes":8589934592}}`)

	// Save Params Only rejects --api-key and keeps drawer open with error.
	s.openServiceParamsDrawerForNew()
	s.serviceParams.extraArgsText = "--api-key secret123"
	tt := newServicesTester(s)
	tt.Frame()
	click(t, tt, "仅保存参数")
	tt.Frame()
	if !s.serviceParams.open || s.serviceParams.saveError == "" {
		t.Fatalf("expected drawer to remain open with saveError on credentials, got open=%v err=%q", s.serviceParams.open, s.serviceParams.saveError)
	}
	if presets := svc.RecentServices(); len(presets) != 0 {
		t.Fatalf("expected no preset saved when credentials rejected, got %+v", presets)
	}

	// Memory shortfall disables start button even when params are otherwise valid.
	s.serviceParams.extraArgsText = ""
	s.serviceParams.saveError = ""
	tt.Frame()
	if info, ok := tt.AccessByRole(ui.RoleButton, "启动服务"); !ok || !info.Disabled {
		t.Fatalf("expected Start Service disabled on memory shortfall, got ok=%v info=%+v", ok, info)
	}

	// Incompatible engine from an old preset is NOT re-added to eligible options.
	s.registry.Engines = append(s.registry.Engines, core.EngineAsset{Name: "vllm-dir-only", Family: "vllm"})
	s.checks.Engines["vllm-dir-only"] = core.EngineEligibility{State: core.EngineCheckEligible}
	opts := s.serviceParamsEngineOptions(s.registry.Models[0], true)
	for _, opt := range opts {
		if opt.Value == "vllm-dir-only" {
			t.Fatalf("expected incompatible engine not re-added to options, got %+v", opts)
		}
	}
}

func TestServicesAutoPortPolicyAndRemovedModelDisabled(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	// Running service started with auto port (RequestedPort=0, actual Port=49123):
	// opening Launch Params drawer keeps portText empty rather than baking 49123.
	s.engineState = core.EngineState{
		State:         "running",
		Origin:        core.EngineOriginStandalone,
		EngineName:    "llama-cpp-default",
		ModelName:     "Qwen3-8B-Q4.gguf",
		Port:          49123,
		RequestedPort: 0,
		ContextSize:   16384,
		Endpoint:      "http://127.0.0.1:49123",
	}
	s.openServiceParamsDrawerForRunning()
	if s.serviceParams.portText != "" {
		t.Fatalf("expected empty portText for auto-port running service, got %q", s.serviceParams.portText)
	}
	s.serviceParams.open = false

	// Exit notice with dynamic Port=49123 and RequestedPort=0 keeps portText empty.
	s.openServiceParamsDrawerFromExitNotice(core.ServiceExitNotice{
		Model:         "Qwen3-8B-Q4.gguf",
		Engine:        "llama-cpp-default",
		Port:          49123,
		RequestedPort: 0,
		ContextSize:   16384,
	})
	if s.serviceParams.portText != "" {
		t.Fatalf("expected empty portText for auto-port exit notice, got %q", s.serviceParams.portText)
	}
	s.serviceParams.open = false

	// Removed model in Recent Services disables Start button and params drawer Start button.
	s.engineState = core.EngineState{State: "stopped"}
	removedPreset := core.ServicePreset{
		Model:       "Removed-Model.gguf",
		Engine:      "llama-cpp-default",
		Port:        0,
		ContextSize: 8192,
	}
	s.recentServices = []core.ServicePreset{removedPreset}
	tt := newServicesTester(s)
	tt.Frame()
	if info, ok := tt.AccessByRole(ui.RoleButton, "▶ 启动 · Removed-Model.gguf"); !ok || !info.Disabled {
		t.Fatalf("expected Recent Start button disabled for removed model, got ok=%v info=%+v", ok, info)
	}

	s.openServiceParamsDrawerForPreset(removedPreset)
	tt.Frame()
	if !tt.HasText("该模型已不在当前模型库中") {
		t.Fatalf("expected missing model warning in params drawer, got %v", tt.Texts())
	}
	if info, ok := tt.AccessByRole(ui.RoleButton, "启动服务"); !ok || !info.Disabled {
		t.Fatalf("expected drawer Start button disabled for removed model, got ok=%v info=%+v", ok, info)
	}
}

func TestServicesSwitchConfirmationAndExitNoticeNotDismissedEarly(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.engineState = core.EngineState{
		State:      "running",
		Origin:     core.EngineOriginStandalone,
		EngineName: "llama-cpp-default",
		ModelName:  "Qwen3-8B-Q4.gguf",
		Endpoint:   "http://127.0.0.1:8080",
		Pid:        1002,
	}
	s.recentServices = []core.ServicePreset{
		{Model: "Llama-3-8B.gguf", Engine: "llama-cpp-default", Port: 0, ContextSize: 8192},
	}

	// Starting another model from Recent while one is running opens switch confirm dialog instead of stopping immediately.
	s.startFromPreset(s.recentServices[0])
	if !s.standalone.switchConfirm || s.standalone.switchModel != "Llama-3-8B.gguf" || s.standalone.switchAction == nil {
		t.Fatalf("expected switch confirmation dialog armed, got %+v", s.standalone)
	}

	// Restarting from exit notice does not dismiss notice before backend starts.
	s.engineState = core.EngineState{State: "stopped"}
	s.service = nil
	s.exitedNotice = &core.ServiceExitNotice{
		ID:         "exit-2",
		Model:      "Qwen3-8B-Q4.gguf",
		Engine:     "llama-cpp-default",
		DetectedAt: "2026-11-25T05:10:00Z",
		WhileAway:  true,
		Hint:       core.ExitHintOOM,
	}
	s.restartFromExitNotice(*s.exitedNotice)
	if !s.hasActiveExitNotice() {
		t.Fatal("expected exit notice preserved until backend start succeeds")
	}

	// Exit card renders DetectedAt and translates OOM hint in both Chinese and English.
	tt := newServicesTester(s)
	tt.Frame()
	zhTexts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(zhTexts, "发现时间") || !strings.Contains(zhTexts, "内存不足") {
		t.Fatalf("expected Chinese detected_at and OOM hint, got:\n%s", zhTexts)
	}
	restoreEn := l10n.SetTestLocale(l10n.LanguageEn)
	defer restoreEn()
	tt.Frame()
	enTexts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(enTexts, "Detected at") || !strings.Contains(enTexts, "out-of-memory") {
		t.Fatalf("expected English detected_at and OOM hint after language switch, got:\n%s", enTexts)
	}
}

func TestKeepServicesOnQuitSerializesRapidToggles(t *testing.T) {
	s, _ := servicesP2State(t)
	s.updateWindow = func(fn func()) { fn() }

	var mu sync.Mutex
	var calls []bool
	gate := make(chan struct{})
	firstEntered := make(chan struct{})
	s.persistKeepServices = func(patch map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError) {
		var val bool
		_ = json.Unmarshal(patch["keep_services_on_quit"], &val)
		mu.Lock()
		first := len(calls) == 0
		calls = append(calls, val)
		mu.Unlock()
		if first {
			close(firstEntered)
			<-gate
		}
		v := val
		return core.SettingsResponse{
			Settings:  core.UserSettings{KeepServicesOnQuit: &v},
			Effective: core.EffectiveSettings{KeepServicesOnQuit: v},
		}, nil
	}

	s.changeKeepServicesOnQuit(false)
	<-firstEntered
	s.changeKeepServicesOnQuit(true)
	s.changeKeepServicesOnQuit(false)
	close(gate)
	s.waitKeepServicesPersist()

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0] != false || calls[1] != false {
		t.Fatalf("expected serialized coalesced writes [false, false], got %v", calls)
	}
	if s.keepServicesOnQuit != false {
		t.Fatalf("expected final keepServicesOnQuit=false, got %v", s.keepServicesOnQuit)
	}
}
