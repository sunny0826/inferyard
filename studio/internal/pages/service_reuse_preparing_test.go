package pages

import (
	"context"
	"strings"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func TestServicesPageRendersImmediatePreparingCardAndSidebarStatus(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.selected = "services"
	s.markStandalonePreparing(core.StandaloneStartRequest{
		Model:       "Qwen3-8B-Q4.gguf",
		Engine:      "llama-cpp-default",
		ContextSize: 16384,
	})

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"Qwen3-8B-Q4.gguf", "llama-cpp-default", "准备中", "服务启动中"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected preparing services page to contain %q, got:\n%s", want, texts)
		}
	}
	if strings.Contains(texts, "暂无运行中的托管服务") {
		t.Fatalf("preparing services page must not show idle empty state, got:\n%s", texts)
	}

	s.standalone.setPreparing("Qwen3-8B-Q4.gguf", false)
	svc := core.EngineState{
		ServiceID:   core.ServiceIDFor("Qwen3-8B-Q4.gguf", "llama-cpp-default"),
		State:       "running",
		Origin:      core.EngineOriginStandalone,
		EngineName:  "llama-cpp-default",
		ModelName:   "Qwen3-8B-Q4.gguf",
		Port:        8080,
		ContextSize: 16384,
		Endpoint:    "http://127.0.0.1:8080",
	}
	s.managedServices = []core.EngineState{svc}
	s.engineState = svc
	tt.Frame()
	runningTexts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(runningTexts, "服务运行中") || strings.Contains(runningTexts, "服务启动中") || strings.Contains(runningTexts, "准备中") {
		t.Fatalf("expected running services page to clear preparing/starting indicators, got:\n%s", runningTexts)
	}
}

func TestBenchmarksPageRendersImmediatePreparingCardOnStart(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	preflight := "/tmp/device-preflight.json"
	s.selected = "benchmarks"
	s.benchFlowOpen = true
	s.model = "Qwen3-8B-Q4.gguf"
	s.engine = "llama-cpp-default"
	s.progress = &inferyard.WorkspaceProgress{
		DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: &preflight},
		Bundles:         []inferyard.BundleProgress{{Bundle: "zh-core"}},
	}
	s.busy = true
	s.busyAction = "prepare-benchmark"
	s.task = benchmarkTask{stage: "prepare_candidate", bundles: []string{"zh-core", "zh-svg-pelican"}}

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"Qwen3-8B-Q4.gguf", "llama-cpp-default", "zh-core", "zh-svg-pelican", "准备中"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected preparing benchmark card to show %q, got:\n%s", want, texts)
		}
	}
	if strings.Contains(texts, "请选择模型") {
		t.Fatalf("expected config form to be replaced by preparing card while preparing benchmark, got:\n%s", texts)
	}
}

func TestStartBenchmarkReusesRunningLocalStandaloneWithoutStopDialog(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.registry.Engines = []core.EngineAsset{
		{Name: "PolyStrata · v0.1.45", Family: "polystrata", Kind: "local_installation", Path: "/engines/polystrata/installation.json"},
	}
	s.checks.Engines["PolyStrata · v0.1.45"] = core.EngineEligibility{State: core.EngineCheckEligible, Family: "polystrata"}
	s.model = "Qwen3-8B-Q4.gguf"
	s.engine = "PolyStrata · v0.1.45"
	s.selectedBundles = map[string]bool{"zh-core": true}
	preflight := "/tmp/device-preflight.json"
	s.progress = &inferyard.WorkspaceProgress{
		DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: &preflight},
		Bundles:         []inferyard.BundleProgress{{Bundle: "zh-core"}},
	}

	svc := core.EngineState{
		ServiceID:   core.ServiceIDFor("Qwen3-8B-Q4.gguf", "PolyStrata · v0.1.45"),
		State:       "running",
		Origin:      core.EngineOriginStandalone,
		Family:      "polystrata",
		EngineName:  "PolyStrata · v0.1.45",
		ModelName:   "Qwen3-8B-Q4.gguf",
		Pid:         7788,
		Port:        49123,
		LANAccess:   true,
		ContextSize: 16384,
		Endpoint:    "http://0.0.0.0:49123",
	}
	s.managedServices = []core.EngineState{svc}
	s.engineState = svc
	s.probeServiceConnFunc = func(context.Context, string) core.ExternalConnProbe {
		return core.ExternalConnProbe{Status: core.ExclusivityExclusive, Port: 49123}
	}

	reused, ok := s.reusableStandaloneService(s.model, s.engine)
	if !ok || reused.Pid != 7788 || reused.Endpoint != "http://127.0.0.1:49123" {
		t.Fatalf("expected reusableStandaloneService to normalize loopback endpoint, got ok=%v reused=%+v", ok, reused)
	}
	req := s.localCandidateRequest([]string{"zh-core"}, &reused)
	if req == nil || req.ServicePID != 7788 || req.Endpoint != "http://127.0.0.1:49123" {
		t.Fatalf("unexpected localCandidateRequest for reused standalone: %+v", req)
	}

	done := make(chan struct{}, 2)
	s.updateWindow = func(fn func()) {
		fn()
		if !s.busy {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	}
	s.startBenchmark()
	<-done
	if s.standalone.benchmarkConfirm {
		t.Fatal("expected startBenchmark to reuse running local standalone without opening stop confirmation dialog")
	}
}
