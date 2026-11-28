//go:build ui_debug && !release

package pages

import (
	"path/filepath"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (p *previewSession) previewEngineMetadata(state string, pid int64, local bool, candidate string) core.EngineState {
	s := p.state
	family := "llama-cpp"
	executable := "llama-server"
	modelKind := "gguf"
	contextSize := int64(32768)
	if e := s.localEngine(); e != nil {
		family = e.Family
		switch e.Family {
		case "polystrata":
			executable = "polystrata"
		case "mlx-lm":
			executable = "python"
			modelKind = "directory"
		case "vllm":
			executable = "vllm"
			modelKind = "directory"
		case "lmstudio":
			executable = "lms"
		}
	}
	if s.speed != nil && s.speed.current != nil && s.speed.current.context > 0 {
		contextSize = int64(s.speed.current.context)
	}
	return core.EngineState{
		State:       state,
		Local:       local,
		Family:      family,
		Executable:  executable,
		Model:       s.model,
		ModelKind:   modelKind,
		ContextSize: contextSize,
		Pid:         pid,
		Endpoint:    "http://127.0.0.1:48857",
		Candidate:   candidate,
	}
}

func (p *previewSession) startService(bind bool, candidate string) {
	s := p.state
	if s.busy || s.running() != nil {
		return
	}
	fail := p.takeFailure()
	s.busy = bind
	s.engineState = p.previewEngineMetadata("starting", 42, false, candidate)
	p.after("service", 800*time.Millisecond, func() {
		if bind {
			s.busy = false
		}
		if fail {
			s.engineState.State, s.engineState.Error = "failed", core.Err("engine_start_failed")
			s.failure = s.engineState.Error
			return
		}
		s.engineState.State = "running"
		if bind && s.progress != nil {
			s.progress.NextAction = "run"
			for i := range s.progress.Bundles {
				b := &s.progress.Bundles[i]
				if b.Phase == "run_complete" || (b.NextAction != "bind" && b.BoundPath != nil) {
					continue
				}
				b.Phase, b.NextAction = "run_missing", "run"
				b.BoundPath = previewValue(filepath.Join(s.workspace, "bound", b.Bundle))
			}
			s.clearRecoveredBinding()
			s.failedBundle = ""
		}
	})
}

func (p *previewSession) startPreviewStandalone(op previewStandaloneStart) {
	s := p.state
	fail := p.takeFailure()
	modelPath := op.model
	modelKind := "gguf"
	for _, m := range s.registry.Models {
		if m.Name == op.model {
			if m.Path != "" {
				modelPath = m.Path
			}
			if m.Format != "" {
				modelKind = m.Format
			}
			break
		}
	}
	family := "llama-cpp"
	localEngine := false
	for _, e := range s.registry.Engines {
		if e.Name == op.engine {
			if e.Family != "" {
				family = e.Family
			}
			localEngine = e.Kind == "local_installation"
			break
		}
	}
	adapter := "prism_llama_server_v1"
	if localEngine {
		adapter = "local_openai_v1"
	}
	executable := "llama-server"
	if family == "polystrata" || family == "vllm" {
		executable = "python"
	}
	serviceID := core.ServiceIDFor(op.model, op.engine)
	p.after("service-"+serviceID, 300*time.Millisecond, func() {
		s.standalone.setPreparing(op.model, false)
		if fail {
			err := core.Err("engine_start_failed")
			s.standalone.setLastError(op.model, err)
			s.failure = err
			s.failurePage = s.selected
			s.engineState = core.EngineState{
				ServiceID:   serviceID,
				State:       "failed",
				Local:       localEngine,
				Origin:      core.EngineOriginStandalone,
				Family:      family,
				Adapter:     adapter,
				Executable:  executable,
				Model:       modelPath,
				ModelKind:   modelKind,
				ContextSize: op.contextSize,
				LogPath:     "/preview/logs/standalone.log",
				Candidate:   "preview-standalone",
				Error:       err,
			}
			return
		}
		port, portErr := p.previewNextAvailablePort(op.port, serviceID)
		if portErr != nil {
			s.standalone.setLastError(op.model, portErr)
			s.failure = portErr
			s.failurePage = s.selected
			return
		}
		s.standalone.setLastError(op.model, nil)
		endpoint := p.previewEndpointURL(op.lanAccess, port)
		budget := s.estimateLocalMemoryBudget(op.model, op.engine, op.contextSize, op.gpuLayers)
		p.clearPreviewExitNoticeFor(serviceID, op.model, op.engine)
		st := core.EngineState{
			ServiceID:         serviceID,
			State:             "starting",
			Local:             localEngine,
			Origin:            core.EngineOriginStandalone,
			Family:            family,
			Adapter:           adapter,
			Executable:        executable,
			EngineName:        op.engine,
			ModelName:         op.model,
			Model:             modelPath,
			ModelKind:         modelKind,
			Port:              port,
			RequestedPort:     op.port,
			LANAccess:         op.lanAccess,
			EstimatedMemoryGB: budget.EstimatedGB,
			ContextSize:       op.contextSize,
			GPULayers:         cloneIntPtrUI(op.gpuLayers),
			Threads:           cloneIntPtrUI(op.threads),
			ExtraArgs:         append([]string(nil), op.extraArgs...),
			StartedAt:         time.Now().UTC().Format(time.RFC3339),
			Pid:               42001 + int64(len(s.managedServices)),
			Endpoint:          endpoint,
			LogPath:           "/preview/logs/standalone.log",
			Candidate:         "preview-standalone",
		}
		p.upsertPreviewManagedService(st)
		s.markServiceExpanded(serviceID)
		s.enqueueStatusToast(l10n.TR("models.serve.toast.started"))
		p.savePreviewServicePreset(core.ServicePreset{
			Model:       op.model,
			Engine:      op.engine,
			Port:        op.port,
			LANAccess:   op.lanAccess,
			ContextSize: op.contextSize,
			GPULayers:   cloneIntPtrUI(op.gpuLayers),
			Threads:     cloneIntPtrUI(op.threads),
			ExtraArgs:   append([]string(nil), op.extraArgs...),
		})
		p.after("service-ready-"+serviceID, 500*time.Millisecond, func() {
			for i := range s.managedServices {
				if s.managedServices[i].ServiceID == serviceID {
					s.managedServices[i].State = "running"
				}
			}
			if s.engineState.ServiceID == serviceID {
				s.engineState.State = "running"
			}
		})
	})
}
