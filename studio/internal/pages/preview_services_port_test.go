//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestPreviewFixedPortConflictReturnsPortInUse(t *testing.T) {
	s := &viewState{
		model:          "Model-A.gguf",
		engine:         "llama-a",
		serveSupported: true,
		managedServices: []core.EngineState{
			{
				ServiceID: core.ServiceIDFor("Model-A.gguf", "llama-a"),
				State:     "running",
				Origin:    core.EngineOriginStandalone,
				ModelName: "Model-A.gguf",
				Port:      8080,
			},
		},
	}
	p := &previewSession{state: s}

	// Different service requesting occupied fixed port 8080 must fail with engine_port_in_use.
	otherID := core.ServiceIDFor("Model-B.gguf", "llama-a")
	if _, err := p.previewNextAvailablePort(8080, otherID); err == nil || err.Code != "engine_port_in_use" {
		t.Fatalf("expected engine_port_in_use for occupied fixed port 8080, got %+v", err)
	}

	// Same service restarting on its own port 8080 is allowed.
	sameID := core.ServiceIDFor("Model-A.gguf", "llama-a")
	if port, err := p.previewNextAvailablePort(8080, sameID); err != nil || port != 8080 {
		t.Fatalf("expected same service to keep port 8080, got port=%d err=%+v", port, err)
	}

	// Free fixed port 8081 is allowed.
	if port, err := p.previewNextAvailablePort(8081, otherID); err != nil || port != 8081 {
		t.Fatalf("expected free port 8081 to be allowed, got port=%d err=%+v", port, err)
	}
}

func TestPreviewStopSingleEngineVsBenchmarkHandoffAllServices(t *testing.T) {
	p := newPreviewSession(context.Background())
	s := p.state
	svcA := core.EngineState{
		ServiceID: core.ServiceIDFor("Model-A.gguf", "llama-a"),
		State:     "running",
		Origin:    core.EngineOriginStandalone,
		ModelName: "Model-A.gguf",
		Port:      8080,
	}
	svcB := core.EngineState{
		ServiceID: core.ServiceIDFor("Model-B.gguf", "llama-a"),
		State:     "running",
		Origin:    core.EngineOriginStandalone,
		ModelName: "Model-B.gguf",
		Port:      8081,
	}
	s.engineState = svcA
	s.managedServices = []core.EngineState{svcA, svcB}

	// Normal previewStopEngine only stops the current/primary service, keeping svcB running.
	p.dispatch(previewStopEngine)
	p.advance(500 * time.Millisecond)
	if len(s.managedServices) != 1 || s.managedServices[0].ServiceID != svcB.ServiceID {
		t.Fatalf("expected previewStopEngine to stop only svcA and keep svcB, got %+v", s.managedServices)
	}

	// Benchmark handoff (previewStopEngineThen) stops all remaining active preview services.
	s.managedServices = []core.EngineState{svcA, svcB}
	s.engineState = svcA
	called := false
	p.dispatch(previewStopEngineThen{after: func() { called = true }})
	p.advance(500 * time.Millisecond)
	if !called || len(s.managedServices) != 0 || s.engineState.State != "stopped" {
		t.Fatalf("expected previewStopEngineThen to stop all services and invoke callback, called=%v managed=%+v engine=%+v", called, s.managedServices, s.engineState)
	}
}
