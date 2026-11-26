package pages

import (
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"net"
	"path/filepath"
	"time"
)

type dashboardSnapshot struct {
	candidate string
	stale     bool
	unknown   bool
	startup   core.Startup
	failure   *core.ApiError
}

func (s *viewState) readDashboard(p *inferyard.WorkspaceProgress) dashboardSnapshot {
	var result dashboardSnapshot
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
		if b.BoundPath != nil && result.failure == nil {
			alive, e := s.service.BindingAlive(*b.BoundPath)
			result.stale, result.unknown, result.failure = !alive && e == nil, e != nil, e
		}
		break
	}
	return result
}

func (s *viewState) ensureManualProcesses() {
	if s.preview != nil || s.service == nil || s.processesLoading {
		return
	}
	if !s.processesLoadedAt.IsZero() && time.Since(s.processesLoadedAt) < 3*time.Second {
		return
	}
	if s.win == nil && s.updateWindow == nil {
		if procs, err := s.service.Processes(s.ctx); err == nil {
			s.processes = procs
			s.processesLoadedAt = time.Now()
		}
		return
	}
	s.processesLoading = true
	svc, ctx := s.service, s.ctx
	go func() {
		procs, err := svc.Processes(ctx)
		s.updateView(func() {
			s.processesLoading = false
			s.processesLoadedAt = time.Now()
			if err == nil {
				s.processes = procs
			}
		})
	}()
}

func (s *viewState) applyDashboard(v dashboardSnapshot, e *core.ApiError) {
	s.refreshFailure = ""
	if e == nil {
		e = v.failure
	}
	if e != nil {
		s.refreshFailure = human(e.Code)
	}
	if s.initDir == "" {
		s.initDir = emptyText(s.ready.Workspace.Path)
	}
	if v.candidate != s.candidate {
		s.process = ""
		s.processesLoadedAt = time.Time{}
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
	s.startup, s.bindingStale = v.startup, v.stale
}
