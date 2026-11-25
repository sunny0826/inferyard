//go:build ui_debug && !release

package pages

import (
	"fmt"
	"strconv"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func seedPreviewRecentServices(model, engine string) []core.ServicePreset {
	gpuAll := -1
	return []core.ServicePreset{
		{
			Model:         model,
			Engine:        engine,
			Port:          8080,
			ContextSize:   32768,
			GPULayers:     &gpuAll,
			LastStartedAt: "2026-11-24T10:15:00Z",
		},
		{
			Model:         "Preview-Small.gguf",
			Engine:        engine,
			Port:          0,
			ContextSize:   8192,
			LastStartedAt: "2026-11-23T08:00:00Z",
		},
	}
}

func previewServiceExitedScene(s *viewState) {
	s.selected = "services"
	code := 137
	gpuAll := -1
	s.syncExitedNotices([]core.ServiceExitNotice{
		{
			ID:          "preview-exit-1",
			ServiceID:   core.ServiceIDFor(s.model, s.engine),
			Model:       s.model,
			Engine:      s.engine,
			Port:        8080,
			ContextSize: 32768,
			GPULayers:   &gpuAll,
			LogPath:     "/preview/logs/standalone.log",
			DetectedAt:  "2026-11-25T05:42:10Z",
			ExitCode:    &code,
			WhileAway:   true,
			LogTail: []string{
				"llama_new_context_with_model: kv_size = 32768",
				"ggml_metal_buffer_init: failed to allocate buffer (out of memory)",
				"process terminated with signal SIGKILL",
			},
			Hint: core.ExitHintOOM,
		},
	})
}

func previewMultiServicesScene(s *viewState) {
	s.selected = "services"
	svc1 := core.EngineState{
		ServiceID:         core.ServiceIDFor(s.model, s.engine),
		State:             "running",
		Origin:            core.EngineOriginStandalone,
		Adopted:           true,
		Family:            "llama-cpp",
		Adapter:           "prism_llama_server_v1",
		Executable:        "llama-server",
		EngineName:        s.engine,
		ModelName:         s.model,
		Model:             s.model,
		ModelKind:         "gguf",
		Port:              8080,
		RequestedPort:     8080,
		LANAccess:         true,
		EstimatedMemoryGB: 5.9,
		ContextSize:       32768,
		StartedAt:         "2026-11-25T04:00:00Z",
		Pid:               42001,
		Endpoint:          "http://0.0.0.0:8080",
		LogPath:           "/preview/logs/standalone-1.log",
	}
	svc2 := core.EngineState{
		ServiceID:         core.ServiceIDFor("Preview-Small.gguf", s.engine),
		State:             "running",
		Origin:            core.EngineOriginStandalone,
		Family:            "llama-cpp",
		Adapter:           "prism_llama_server_v1",
		Executable:        "llama-server",
		EngineName:        s.engine,
		ModelName:         "Preview-Small.gguf",
		Model:             "Preview-Small.gguf",
		ModelKind:         "gguf",
		Port:              48858,
		EstimatedMemoryGB: 2.6,
		ContextSize:       8192,
		StartedAt:         "2026-11-25T04:10:00Z",
		Pid:               42002,
		Endpoint:          "http://127.0.0.1:48858",
		LogPath:           "/preview/logs/standalone-2.log",
	}
	s.managedServices = []core.EngineState{svc1, svc2}
	s.engineState = svc1
	s.memorySummary = s.currentMemorySummary()
}

func (p *previewSession) savePreviewServicePreset(preset core.ServicePreset) {
	if preset.LastStartedAt == "" {
		preset.LastStartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	p.state.upsertRecentServiceLocal(preset)
}

func (p *previewSession) dismissPreviewExitNotice(id string) {
	p.state.removeExitedNotice(id)
}

func (p *previewSession) previewNextAvailablePort(requested int, excludeServiceID string) (int, *core.ApiError) {
	used := map[int]bool{}
	for _, st := range p.state.managedServices {
		if engineLocked(st.State) && st.Port > 0 && st.ServiceID != excludeServiceID {
			used[st.Port] = true
		}
	}
	if requested > 0 {
		if used[requested] {
			return 0, &core.ApiError{
				Code:    "engine_port_in_use",
				Message: l10n.TRf("core.engine.err.port_in_use_numbered", "port", strconv.Itoa(requested)),
				Details: map[string]any{"port": requested},
			}
		}
		return requested, nil
	}
	for port := 48857; port < 48957; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 48857, nil
}

func (p *previewSession) upsertPreviewManagedService(st core.EngineState) {
	s := p.state
	updated := false
	for i := range s.managedServices {
		if s.managedServices[i].ServiceID == st.ServiceID {
			s.managedServices[i] = st
			updated = true
			break
		}
	}
	if !updated {
		s.managedServices = append(s.managedServices, st)
	}
	s.engineState = st
	s.memorySummary = core.ServiceMemoryBudget{}
	s.memorySummary = s.currentMemorySummary()
}

func (p *previewSession) stopPreviewManagedService(serviceID string, after func()) {
	s := p.state
	if serviceID == "" {
		p.stopPreviewEngine(after)
		return
	}
	if s.running() != nil || (s.speed != nil && s.speed.current != nil && s.speed.current.active()) {
		s.failure = core.Err("run_active")
		return
	}
	if p.takeFailure() {
		s.failure = core.Err("engine_stop_failed")
		return
	}
	found := false
	for i := range s.managedServices {
		if s.managedServices[i].ServiceID == serviceID {
			s.managedServices[i].State = "stopping"
			found = true
			break
		}
	}
	if !found {
		p.stopPreviewEngine(after)
		return
	}
	if s.engineState.ServiceID == serviceID {
		s.engineState.State = "stopping"
	}
	p.after("service-stop-"+serviceID, 400*time.Millisecond, func() {
		remaining := make([]core.EngineState, 0, len(s.managedServices))
		var stopped core.EngineState
		for _, st := range s.managedServices {
			if st.ServiceID == serviceID {
				stopped = st
				continue
			}
			remaining = append(remaining, st)
		}
		s.managedServices = remaining
		if len(remaining) > 0 {
			s.engineState = remaining[0]
		} else {
			stopped.State = "stopped"
			s.engineState = stopped
		}
		s.memorySummary = core.ServiceMemoryBudget{}
		s.memorySummary = s.currentMemorySummary()
		if after != nil {
			after()
		}
	})
}

func (p *previewSession) clearPreviewExitNoticeFor(serviceID, model, engine string) {
	s := p.state
	remaining := make([]core.ServiceExitNotice, 0, len(s.exitedNotices))
	for _, n := range s.exitedNotices {
		if (serviceID != "" && n.ServiceID == serviceID) || (model != "" && engine != "" && n.Model == model && n.Engine == engine) {
			continue
		}
		remaining = append(remaining, n)
	}
	s.syncExitedNotices(remaining)
}

func (p *previewSession) previewEndpointURL(lanAccess bool, port int) string {
	if lanAccess {
		return fmt.Sprintf("http://0.0.0.0:%d", port)
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func (p *previewSession) testPreviewServiceEndpoint(op previewTestServiceEndpoint) {
	s := p.state
	fail := p.takeFailure()
	p.after("service-test", 250*time.Millisecond, func() {
		if s.serviceUsage.testSeq != op.seq {
			return
		}
		s.serviceUsage.testing = false
		if fail {
			s.serviceUsage.testResult = &core.ServiceTestResult{
				OK:         false,
				StatusCode: 503,
				LatencyMs:  312,
				Error:      "service unavailable",
			}
			return
		}
		s.serviceUsage.testResult = &core.ServiceTestResult{
			OK:         true,
			StatusCode: 200,
			LatencyMs:  128,
			Reply:      "OK",
		}
	})
}

func (p *previewSession) stopPreviewEngine(after func()) {
	s := p.state
	if s.running() != nil || (s.speed != nil && s.speed.current != nil && s.speed.current.active()) {
		s.failure = core.Err("run_active")
		return
	}
	if p.takeFailure() {
		s.failure = core.Err("engine_stop_failed")
		return
	}
	p.cancelEvents("service")
	stoppedID := s.engineState.ServiceID
	s.engineState.State = "stopping"
	for i := range s.managedServices {
		if stoppedID != "" && s.managedServices[i].ServiceID == stoppedID {
			s.managedServices[i].State = "stopping"
		}
	}
	p.after("service", 400*time.Millisecond, func() {
		if stoppedID != "" {
			remaining := make([]core.EngineState, 0, len(s.managedServices))
			for _, st := range s.managedServices {
				if st.ServiceID != stoppedID {
					remaining = append(remaining, st)
				}
			}
			s.managedServices = remaining
		}
		if len(s.managedServices) > 0 {
			s.engineState = s.managedServices[0]
		} else if s.engineState.Origin == core.EngineOriginStandalone {
			s.engineState = core.EngineState{
				State:       "stopped",
				Origin:      core.EngineOriginStandalone,
				Model:       s.engineState.Model,
				ModelKind:   s.engineState.ModelKind,
				Family:      s.engineState.Family,
				Adapter:     s.engineState.Adapter,
				ContextSize: s.engineState.ContextSize,
				Candidate:   s.engineState.Candidate,
			}
		} else {
			s.engineState = core.EngineState{State: "stopped"}
		}
		s.memorySummary = core.ServiceMemoryBudget{}
		s.memorySummary = s.currentMemorySummary()
		if after != nil {
			after()
		}
	})
}

func (p *previewSession) stopAllPreviewServices(after func()) {
	s := p.state
	if s.running() != nil || (s.speed != nil && s.speed.current != nil && s.speed.current.active()) {
		s.failure = core.Err("run_active")
		return
	}
	if p.takeFailure() {
		s.failure = core.Err("engine_stop_failed")
		return
	}
	p.cancelEvents("service")
	s.engineState.State = "stopping"
	for i := range s.managedServices {
		if engineLocked(s.managedServices[i].State) {
			s.managedServices[i].State = "stopping"
		}
	}
	p.after("service", 400*time.Millisecond, func() {
		s.managedServices = nil
		if s.engineState.Origin == core.EngineOriginStandalone {
			s.engineState = core.EngineState{
				State:       "stopped",
				Origin:      core.EngineOriginStandalone,
				Model:       s.engineState.Model,
				ModelKind:   s.engineState.ModelKind,
				Family:      s.engineState.Family,
				Adapter:     s.engineState.Adapter,
				ContextSize: s.engineState.ContextSize,
				Candidate:   s.engineState.Candidate,
			}
		} else {
			s.engineState = core.EngineState{State: "stopped"}
		}
		s.memorySummary = core.ServiceMemoryBudget{}
		s.memorySummary = s.currentMemorySummary()
		if after != nil {
			after()
		}
	})
}
