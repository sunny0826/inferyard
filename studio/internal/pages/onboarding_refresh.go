package pages

import (
	"fmt"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"net"
	"path/filepath"
)

type onboardingSnapshot struct {
	candidate string
	stale     bool
	unknown   bool
	startup   core.Startup
	processes []core.ProcessCandidate
	failure   *core.ApiError
}

func (s *viewState) readOnboarding(p *inferyard.WorkspaceProgress) onboardingSnapshot {
	var result onboardingSnapshot
	if p == nil {
		return result
	}
	for _, b := range p.Bundles {
		if b.Phase == "run_complete" {
			continue
		}
		result.candidate = emptyText(b.CandidatePath)
		if result.candidate == "" && b.BoundPath != nil {
			result.candidate = filepath.Join(*b.BoundPath, "config.toml")
		}
		if result.candidate == "" {
			break
		}
		result.startup, result.failure = s.service.Startup(result.candidate)
		if result.failure == nil {
			result.processes, result.failure = s.service.Processes(s.ctx)
		}
		if b.BoundPath != nil && result.failure == nil {
			alive, e := s.service.BindingAlive(*b.BoundPath)
			result.stale, result.unknown, result.failure = !alive && e == nil, e != nil, e
		}
		break
	}
	return result
}

func (s *viewState) applyOnboarding(v onboardingSnapshot, e *core.ApiError) {
	s.refreshFailure = ""
	if e == nil {
		e = v.failure
	}
	if e != nil {
		s.refreshFailure = human(e.Code)
	}
	if s.initDir == "" {
		s.initDir = suggestInitDir(emptyText(s.ready.Workspace.Path), s.ready.Workspace.Exists, s.ready.Workspace.Initialized)
	}
	if !s.assetsPreset {
		s.assetsPreset = true
		if s.modelName == "" {
			s.modelName = fmt.Sprintf("model-%d", uniqueID())
		}
		if s.engineName == "" {
			s.engineName = fmt.Sprintf("engine-%d", uniqueID())
		}
		if s.modelURL == "" {
			s.modelURL = core.DefaultModelURL
		}
		if s.engineURL == "" {
			s.engineURL = core.DefaultEngineURL()
		}
	}
	if !s.enginePreset {
		s.enginePreset = true
		workspace := emptyText(s.ready.Workspace.Path)
		if s.progress != nil {
			workspace = s.progress.Workspace
		}
		if s.engine == "" {
			s.engine = core.FindEngineBinary(core.EngineRoots(workspace)...)
		}
	}
	if v.candidate != s.candidate {
		s.process = ""
		s.candidate = v.candidate
		s.bindEndpoint = ""
		host, port := "", ""
		for i, a := range v.startup.Args {
			if i+1 >= len(v.startup.Args) {
				continue
			}
			if a == "--host" {
				host = v.startup.Args[i+1]
			}
			if a == "--port" {
				port = v.startup.Args[i+1]
			}
		}
		if host != "" && port != "" {
			s.bindEndpoint = "http://" + net.JoinHostPort(host, port)
		}
	}
	s.startup, s.processes, s.bindingStale, s.bindingUnknown = v.startup, v.processes, v.stale, v.unknown
}
