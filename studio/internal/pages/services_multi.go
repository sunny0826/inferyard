package pages

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) effectiveManagedServices() []core.EngineState {
	if len(s.managedServices) > 0 {
		return s.managedServices
	}
	switch s.engineState.State {
	case "starting", "running", "stopping", "orphaned":
		return []core.EngineState{s.engineState}
	default:
		return nil
	}
}

func (s *viewState) activeManagedServices() []core.EngineState {
	all := s.effectiveManagedServices()
	if len(all) == 0 {
		return nil
	}
	out := make([]core.EngineState, 0, len(all))
	for _, st := range all {
		if engineLocked(st.State) {
			out = append(out, st)
		}
	}
	return out
}

func (s *viewState) activeStandaloneServices() []core.EngineState {
	all := s.effectiveManagedServices()
	if len(all) == 0 {
		return nil
	}
	out := make([]core.EngineState, 0, len(all))
	for _, st := range all {
		if st.Origin == core.EngineOriginStandalone && (st.State == "running" || st.State == "starting") {
			out = append(out, st)
		}
	}
	return out
}

func (s *viewState) effectiveExitedNotices() []core.ServiceExitNotice {
	if len(s.exitedNotices) > 0 {
		out := make([]core.ServiceExitNotice, 0, len(s.exitedNotices))
		for _, n := range s.exitedNotices {
			if !n.Dismissed {
				out = append(out, n)
			}
		}
		return out
	}
	if s.exitedNotice != nil && !s.exitedNotice.Dismissed {
		return []core.ServiceExitNotice{*s.exitedNotice}
	}
	return nil
}

func (s *viewState) syncExitedNotices(notices []core.ServiceExitNotice) {
	cleaned := make([]core.ServiceExitNotice, 0, len(notices))
	for _, n := range notices {
		if !n.Dismissed {
			cleaned = append(cleaned, n)
		}
	}
	s.exitedNotices = cleaned
	if len(cleaned) > 0 {
		cp := cleaned[0]
		s.exitedNotice = &cp
	} else {
		s.exitedNotice = nil
	}
}

func (s *viewState) appendExitedNotice(notice core.ServiceExitNotice) {
	if notice.Dismissed {
		return
	}
	updated := make([]core.ServiceExitNotice, 0, len(s.exitedNotices)+1)
	updated = append(updated, notice)
	for _, existing := range s.exitedNotices {
		if existing.ID != "" && existing.ID == notice.ID {
			continue
		}
		if !existing.Dismissed {
			updated = append(updated, existing)
		}
	}
	s.syncExitedNotices(updated)
}

func (s *viewState) removeExitedNotice(id string) {
	if id == "" {
		s.syncExitedNotices(nil)
		return
	}
	remaining := make([]core.ServiceExitNotice, 0, len(s.exitedNotices))
	for _, n := range s.exitedNotices {
		if n.ID == id {
			continue
		}
		remaining = append(remaining, n)
	}
	s.syncExitedNotices(remaining)
}

func (s *viewState) applyServicesStateEvent(services []core.EngineState) {
	s.managedServices = services
	if len(services) > 0 {
		s.engineState = services[0]
	} else if s.service != nil {
		s.engineState = s.service.Engine()
	}
	if s.service != nil && s.preview == nil {
		s.memorySummary = s.service.CurrentServiceMemorySummary()
	}
}

func serviceKey(st core.EngineState) string {
	if strings.TrimSpace(st.ServiceID) != "" {
		return strings.TrimSpace(st.ServiceID)
	}
	model := strings.TrimSpace(st.ModelName)
	if model == "" {
		model = displayName(st.Model)
	}
	engine := strings.TrimSpace(st.EngineName)
	if engine == "" {
		engine = st.Family
	}
	if model != "" || engine != "" {
		return core.ServiceIDFor(model, engine)
	}
	return "default-service"
}

func (s *viewState) serviceModelTitle(c *ui.Context, st core.EngineState) string {
	modelName := strings.TrimSpace(st.ModelName)
	if modelName == "" {
		modelName = displayName(st.Model)
	}
	if modelName == "" && s.startup.ModelPath != "" {
		modelName = displayName(s.startup.ModelPath)
	}
	if modelName == "" {
		modelName = displayName(st.Candidate)
	}
	if modelName == "" {
		return tr(c, "engine.card.title")
	}
	return modelName
}

func serviceEngineTitle(st core.EngineState) string {
	engineTitle := strings.TrimSpace(st.EngineName)
	if engineTitle == "" {
		engineTitle = engineFamilyTitle(st.Family, st.Adapter)
	}
	if engineTitle == "" {
		engineTitle = displayName(st.Executable)
	}
	return engineTitle
}

func isLocalOrWildcardURL(raw string) bool {
	if isLoopbackURL(raw) {
		return true
	}
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
		ip := net.ParseIP(u.Hostname())
		if ip != nil && ip.IsUnspecified() {
			return true
		}
	}
	return false
}

func serviceLocalURL(st core.EngineState) (string, bool) {
	raw := strings.TrimSpace(st.Endpoint)
	if raw == "" {
		if st.Port > 0 {
			return "http://127.0.0.1:" + strconv.Itoa(st.Port), true
		}
		return "", false
	}
	if isLoopbackURL(raw) {
		return raw, true
	}
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil {
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsUnspecified() {
			port := u.Port()
			if port == "" && st.Port > 0 {
				port = strconv.Itoa(st.Port)
			}
			if port != "" {
				return u.Scheme + "://" + net.JoinHostPort("127.0.0.1", port), true
			}
		}
	}
	return "", false
}

func serviceEndpointDisplay(st core.EngineState) (string, bool) {
	raw := strings.TrimSpace(st.Endpoint)
	if isLocalOrWildcardURL(raw) {
		return raw, true
	}
	if st.Port > 0 {
		host := "127.0.0.1"
		if st.LANAccess {
			host = "0.0.0.0"
		}
		return "http://" + net.JoinHostPort(host, strconv.Itoa(st.Port)), true
	}
	return "", false
}

func serviceWebUIInfo(st core.EngineState) (string, bool, string) {
	localURL, ok := serviceLocalURL(st)
	if !ok {
		return "", false, ""
	}
	if st.WebUIDisabled {
		return localURL, false, l10n.TR("engine.webui.disabled")
	}
	if st.Family == "mlx-lm" {
		return localURL, false, l10n.TR("engine.webui.api_only")
	}
	if st.State != "running" {
		return localURL, false, ""
	}
	return localURL, true, ""
}

func (s *viewState) copyServiceEndpoint(c *ui.Context, st core.EngineState) {
	target, ok := serviceEndpointDisplay(st)
	if !ok {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast(tr(c, "engine.toast.endpoint_copied_preview"))
		return
	}
	c.WriteClipboardThen(target, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("engine.toast.endpoint_copy_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TR("engine.toast.endpoint_copied"))
	})
}

func (s *viewState) isServiceExpanded(key string, idx int) bool {
	if s.expandedServices != nil {
		if expanded, ok := s.expandedServices[key]; ok {
			return expanded
		}
	}
	return idx == 0
}

func (s *viewState) toggleServiceExpanded(key string, idx int) {
	if s.expandedServices == nil {
		s.expandedServices = map[string]bool{}
	}
	s.expandedServices[key] = !s.isServiceExpanded(key, idx)
}

func (s *viewState) markServiceExpanded(key string) {
	if key == "" {
		return
	}
	if s.expandedServices == nil {
		s.expandedServices = map[string]bool{}
	}
	s.expandedServices[key] = true
}

func (s *viewState) findManagedServiceByID(id string) (core.EngineState, bool) {
	for _, st := range s.effectiveManagedServices() {
		if st.ServiceID == id || serviceKey(st) == id {
			return st, true
		}
	}
	return core.EngineState{}, false
}

func (s *viewState) findRunningServiceByModelEngine(model, engine string) (core.EngineState, bool) {
	for _, st := range s.activeManagedServices() {
		modelMatch := (st.ModelName != "" && st.ModelName == model) || displayName(st.Model) == model
		engineMatch := engine == "" || st.EngineName == "" || st.EngineName == engine
		if modelMatch && engineMatch {
			return st, true
		}
	}
	return core.EngineState{}, false
}

func (s *viewState) serviceStopEnabled(st core.EngineState, runActive bool) bool {
	if runActive || s.busy || s.newTaskPending {
		return false
	}
	return st.State == "starting" || st.State == "running"
}

func (s *viewState) stopManagedServiceByID(serviceID string) {
	if s.simulate(previewStopManagedService{serviceID: serviceID}) {
		return
	}
	if s.service == nil {
		return
	}
	s.work(func() (string, *core.ApiError) {
		var apiErr *core.ApiError
		if strings.TrimSpace(serviceID) != "" {
			_, apiErr = s.service.StopManagedService(serviceID)
		} else {
			_, apiErr = s.stopEngineCall()
		}
		if apiErr != nil {
			return "", apiErr
		}
		return l10n.TR("models.serve.toast.stopping"), nil
	})
}

func (s *viewState) stopManagedServiceThen(serviceID string, after func()) {
	if s.preview != nil {
		s.simulate(previewStopManagedService{serviceID: serviceID, after: after})
		return
	}
	if s.service == nil {
		if after != nil {
			after()
		}
		return
	}
	go func() {
		var apiErr *core.ApiError
		if strings.TrimSpace(serviceID) != "" {
			_, apiErr = s.service.StopManagedService(serviceID)
		} else {
			_, apiErr = s.service.StopEngine()
		}
		if apiErr != nil {
			s.runOnUI(func() {
				s.failure = apiErr
				s.failurePage = s.selected
			})
			return
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if s.ctx != nil && s.ctx.Err() != nil {
				return
			}
			stopped := false
			if strings.TrimSpace(serviceID) != "" {
				st, ok := s.service.ManagedService(serviceID)
				stopped = !ok || st.State == "" || st.State == "stopped" || st.State == "failed"
			} else {
				st := s.service.Engine()
				stopped = st.State == "" || st.State == "stopped" || st.State == "failed"
			}
			if stopped {
				managed := s.service.ManagedServices()
				summary := s.service.CurrentServiceMemorySummary()
				eng := s.service.Engine()
				s.runOnUI(func() {
					s.managedServices = managed
					s.memorySummary = summary
					s.engineState = eng
					if after != nil {
						after()
					}
				})
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		s.runOnUI(func() {
			s.failure = core.Err("engine_stop_failed")
			s.failurePage = s.selected
		})
	}()
}

func (s *viewState) stopOrphanServiceByID(serviceID string) {
	if s.simulate(previewStopManagedService{serviceID: serviceID}) {
		return
	}
	if s.service == nil {
		return
	}
	s.work(func() (string, *core.ApiError) {
		var state core.EngineState
		var e *core.ApiError
		if strings.TrimSpace(serviceID) != "" {
			state, e = s.service.StopOrphanService(serviceID)
		} else {
			state, e = s.service.StopOrphanEngine()
		}
		if state.State == "stopped" {
			return l10n.TR("engine.orphan.stopped_cleared"), e
		}
		return l10n.TR("engine.orphan.stop_requested"), e
	})
}

func (s *viewState) discardOrphanServiceByID(serviceID string) {
	if s.simulate(previewStopManagedService{serviceID: serviceID}) {
		return
	}
	if s.service == nil {
		return
	}
	s.work(func() (string, *core.ApiError) {
		var e *core.ApiError
		if strings.TrimSpace(serviceID) != "" {
			_, e = s.service.DiscardOrphanService(serviceID)
		} else {
			_, e = s.service.DiscardOrphanRecord()
		}
		return l10n.TR("engine.orphan.discarded"), e
	})
}

func (s *viewState) openServiceLog(svc core.EngineState) {
	s.serviceLogTargetID = svc.ServiceID
	logPath := strings.TrimSpace(svc.LogPath)
	if logPath == "" && s.service != nil && svc.ServiceID != "" {
		if latest, ok := s.service.ManagedService(svc.ServiceID); ok {
			logPath = strings.TrimSpace(latest.LogPath)
		}
	}
	if logPath == "" {
		return
	}
	s.openEngineCardLog(logPath)
}
