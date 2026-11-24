package pages

import (
	"net"
	"net/url"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func engineLocked(state string) bool {
	return state == "starting" || state == "running" || state == "stopping"
}

func (s *viewState) eligibility(name string) core.EngineEligibility {
	e := s.checks.Engines[name]
	if e.State == "" {
		e.State = core.EngineCheckUnknown
	}
	return e
}

func (s *viewState) preflightReady() bool {
	p := s.progress
	if s.localEngine() != nil {
		return p != nil && s.ready.Workspace.Initialized
	}
	return p != nil && p.DevicePreflight.Status == "ok" && p.DevicePreflight.Path != nil && p.NextAction != "device_check"
}

func (s *viewState) stopEnabled(active, runActive bool) bool {
	if !active || runActive || s.busy || s.newTaskPending {
		return false
	}
	return s.engineState.State == "starting" || s.engineState.State == "running"
}

func isLoopbackURL(raw string) bool {
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
		if host := u.Hostname(); host == "localhost" || net.ParseIP(host).IsLoopback() {
			return true
		}
	}
	return false
}

func loopbackEndpoint(raw string) string {
	if isLoopbackURL(raw) {
		return raw
	}
	return l10n.TR("engine.endpoint.non_local")
}
