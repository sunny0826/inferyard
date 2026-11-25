package pages

import (
	"context"
	"encoding/json"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func TestP4ExtCheckDialogStartBenchmarkPaths(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	newReadyState := func() (*viewState, chan struct{}) {
		s, _ := servicesP2State(t)
		preflight := "/tmp/device-preflight.json"
		s.ready.CLI.Status, s.ready.Workspace.Initialized = "ok", true
		s.ready.Results.Exists, s.ready.Models.Exists = true, true
		s.progress = &inferyard.WorkspaceProgress{
			DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: &preflight},
			Bundles:         []inferyard.BundleProgress{{Bundle: "zh-core"}},
		}
		s.selectedBundles = map[string]bool{"zh-core": true}
		s.device.Snapshot = json.RawMessage(`{"hardware":{"apple_silicon":true,"memory_total_bytes":51539607552}}`)
		done := make(chan struct{}, 4)
		s.updateWindow = func(fn func()) {
			fn()
			if !s.busy {
				select {
				case done <- struct{}{}:
				default:
				}
			}
		}
		return s, done
	}
	svc := core.EngineState{
		ServiceID: core.ServiceIDFor("Qwen3-8B-Q4.gguf", "llama-cpp-default"), State: "running",
		Origin: core.EngineOriginStandalone, EngineName: "llama-cpp-default", ModelName: "Qwen3-8B-Q4.gguf",
		Port: 8080, EstimatedMemoryGB: 4.0, ContextSize: 8192, Endpoint: "http://127.0.0.1:8080",
	}

	// 1) "另起测评专用实例" -> isolated mode skips standalone stop guard and keeps running service.
	s1, done1 := newReadyState()
	s1.managedServices = []core.EngineState{svc}
	s1.probeServiceConnFunc = func(context.Context, string) core.ExternalConnProbe {
		return core.ExternalConnProbe{Status: core.ExclusivityExternal, Port: 8080, Clients: []string{"Client (PID 9)"}}
	}
	s1.startBenchmarkForManagedService(svc)
	tt1 := newServicesTester(s1)
	tt1.Frame()
	click(t, tt1, "另起测评专用实例")
	tt1.Frame()
	if s1.pendingExclusivityMode != "isolated" || s1.pendingExclusivityModel != "Qwen3-8B-Q4.gguf" || s1.pendingExclusivityEngine != "llama-cpp-default" {
		t.Fatalf("unexpected isolated pending state: mode=%q model=%q engine=%q", s1.pendingExclusivityMode, s1.pendingExclusivityModel, s1.pendingExclusivityEngine)
	}
	s1.startBenchmark()
	<-done1
	if s1.standalone.benchmarkConfirm {
		t.Fatal("expected isolated mode to skip standalone stop confirmation dialog")
	}
	if s1.task.bindings == nil || s1.task.bindings.initialExclusivity != "" {
		t.Fatalf("expected isolated mode initialExclusivity='', got %+v", s1.task.bindings)
	}
	if s1.pendingExclusivityMode != "" || s1.pendingExclusivity != "" {
		t.Fatalf("expected pending exclusivity cleared after startBenchmark, got mode=%q excl=%q", s1.pendingExclusivityMode, s1.pendingExclusivity)
	}

	// 2) "仍然测评（报告标注）" -> reuse mode passes InitialExclusivity=external when target still matches.
	s2, done2 := newReadyState()
	s2.probeEndpointConnFunc = func(context.Context, string) core.ExternalConnProbe {
		return core.ExternalConnProbe{Status: core.ExclusivityExternal, Port: 11434, Clients: []string{"Client (PID 9)"}}
	}
	s2.engine = "llama-cpp-default"
	s2.startBenchmarkForDiscoveredService(inferyard.DiscoveredService{ProcessName: "ollama"}, inferyard.DiscoveredEndpoint{URL: "http://127.0.0.1:11434", Models: []string{"Qwen3-8B-Q4.gguf"}})
	tt2 := newServicesTester(s2)
	tt2.Frame()
	click(t, tt2, "仍然测评（报告标注）")
	tt2.Frame()
	s2.startBenchmark()
	<-done2
	if s2.task.bindings == nil || s2.task.bindings.initialExclusivity != core.ExclusivityExternal {
		t.Fatalf("expected reuse mode with matching target to pass external exclusivity, got %+v", s2.task.bindings)
	}

	// 3) "仍然测评（报告标注）" followed by switching model in wizard -> clears pending without leaking external.
	s3, done3 := newReadyState()
	s3.probeEndpointConnFunc = func(context.Context, string) core.ExternalConnProbe {
		return core.ExternalConnProbe{Status: core.ExclusivityExternal, Port: 11434}
	}
	s3.engine = "llama-cpp-default"
	s3.startBenchmarkForDiscoveredService(inferyard.DiscoveredService{ProcessName: "ollama"}, inferyard.DiscoveredEndpoint{URL: "http://127.0.0.1:11434", Models: []string{"Qwen3-8B-Q4.gguf"}})
	tt3 := newServicesTester(s3)
	tt3.Frame()
	click(t, tt3, "仍然测评（报告标注）")
	tt3.Frame()
	s3.model = "Llama-3-8B.gguf"
	s3.startBenchmark()
	<-done3
	if s3.task.bindings == nil || s3.task.bindings.initialExclusivity != "" {
		t.Fatalf("expected target mismatch to drop pending external exclusivity, got %+v", s3.task.bindings)
	}
}
