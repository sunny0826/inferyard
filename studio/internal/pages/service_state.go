package pages

import (
	"net"
	"net/url"

	"inferyard/studio/internal/core"
)

// engineLocked reports whether a managed service is active; the task page
// keeps its selection controls disabled while it runs.
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

// failureBadge shows only the real fixed reason produced by the engine
// supervisor; unknown codes fall back to the generic human mapping, never an
// invented cause.
func failureBadge(state core.EngineState) string {
	if state.Error == nil {
		return "失败"
	}
	return "失败：" + state.Error.Code + " · " + human(state.Error.Code)
}

// preflightReady reports whether progress reached the configuration stage: the
// latest device preflight exists and the workspace is not still waiting for a
// device check.
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

// loopbackEndpoint displays only loopback service endpoints.
func loopbackEndpoint(raw string) string {
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
		if host := u.Hostname(); host == "localhost" || net.ParseIP(host).IsLoopback() {
			return raw
		}
	}
	return "非本机地址（不显示）"
}
