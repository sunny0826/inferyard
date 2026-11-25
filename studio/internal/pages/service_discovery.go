package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func (s *viewState) refreshDiscovery() {
	if s.simulate(previewRefreshDiscovery) {
		return
	}
	if s.discovering {
		return
	}
	s.engineState = s.service.Engine()
	s.discovering = true
	cliKey := s.discoveryCLI
	go func() {
		result, e := s.service.Discover(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		s.win.Update(func() {
			if cliKey != s.discoveryCLI {
				s.discovering = false
				s.refreshDiscovery()
				return
			}
			s.discovery, s.discoveryError = result, e
			s.discovering, s.discoveryLoaded = false, true
		})
	}()
}

func (s *viewState) managedServiceActive() bool {
	switch s.engineState.State {
	case "starting", "running", "stopping":
		return true
	default:
		return false
	}
}

func (s *viewState) managedEndpoint(service inferyard.DiscoveredService, endpoint inferyard.DiscoveredEndpoint) bool {
	return s.managedServiceActive() && s.engineState.Pid > 0 && service.PID == s.engineState.Pid &&
		s.engineState.Endpoint != "" && endpoint.URL == s.engineState.Endpoint && !endpoint.AmbiguousListener
}

func (s *viewState) managedServiceView(c *ui.Context) {
	state := s.engineState
	switch {
	case s.managedServiceActive():
		heading(c, tr(c, "engine.service.managed_heading"), 3)
		ui.Text(c, trf(c, "engine.service.state_line", "state", stateLabel(state.State)))
	case state.State == "orphaned":
		heading(c, tr(c, "engine.service.orphan_heading"), 3)
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateWarning,
			Size:    bannerCompact,
			Message: tr(c, "engine.service.orphan_notice"),
		})
	default:
		ui.Text(c, tr(c, "engine.service.none_managed")).TextColor(tokens(c).Color.FgSecondary)
		return
	}
	keyValueRow(c, tr(c, "engine.origin.label"), engineOriginLabel(state.Origin), false)
	if state.Origin == core.EngineOriginStandalone {
		if state.Model != "" {
			keyValueRow(c, tr(c, "engine.info.model"), displayName(state.Model), false)
		}
		if ctxStr := formatContextTokens(state.ContextSize); ctxStr != "" {
			keyValueRow(c, tr(c, "engine.info.context"), ctxStr, true)
		}
	} else if state.Candidate != "" {
		keyValueRow(c, tr(c, "engine.service.candidate_label"), displayName(state.Candidate), false)
	}
	if state.Pid > 0 {
		keyValueRow(c, "PID", fmt.Sprintf("%d", state.Pid), true)
	}
	if state.Endpoint != "" {
		keyValueRow(c, tr(c, "engine.service.endpoint_label"), loopbackEndpoint(state.Endpoint), false)
	}
	if s.managedServiceActive() {
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			if _, ok := validLoopbackURL(state.Endpoint); ok {
				outlineButton(c, tr(c, "engine.action.copy_endpoint")).OnClick(func() {
					s.copyEngineEndpoint(c, state.Endpoint)
				})
			}
			if _, canOpen, _ := engineWebUIInfo(state); canOpen {
				outlineButton(c, tr(c, "engine.action.open_webui")).OnClick(func() {
					s.openEngineWebUI(c, state.Endpoint)
				})
			}
			if state.LogPath != "" {
				outlineButton(c, tr(c, "engine.action.open_log")).OnClick(func() {
					s.openEngineCardLog(state.LogPath)
				})
			}
			runActive := s.roundWorkActive()
			dangerButton(c, tr(c, "models.serve.action.stop")).Disabled(state.State == "stopping" || runActive || s.busy).OnClick(s.stopStandaloneService)
		})
	}
}

func (s *viewState) discoveryView(c *ui.Context, bindable bool) {
	if bindable {
		heading(c, tr(c, "engine.discovery.heading"), 3)
	}
	s.discoveryBody(c, bindable)
}

func (s *viewState) discoveryBody(c *ui.Context, bindable bool) {
	s.managedServiceView(c)
	refreshButton(c, tr(c, "engine.discovery.refresh"), s.discovering).Disabled(s.discovering || s.busy).OnClick(s.refreshDiscovery)
	errorView(c, s.discoveryError)
	if s.discoveryError == nil {
		for _, code := range s.discovery.Limitations {
			warningBanner(c, human(code))
		}
	}
	compatible := 0
	for _, service := range s.discovery.Services {
		for _, endpoint := range service.Endpoints {
			if endpoint.Models != nil && !s.managedEndpoint(service, endpoint) {
				compatible++
			}
		}
	}
	if compatible > 0 {
		componentFold(c, tr(c, "engine.discovery.compatible_fold"), func() {
			for _, service := range s.discovery.Services {
				for _, endpoint := range service.Endpoints {
					if endpoint.Models != nil && !s.managedEndpoint(service, endpoint) {
						s.compatibleEndpointView(c, service, endpoint, bindable)
					}
				}
			}
		})
	} else if s.discoveryError == nil && !s.discovering {
		message := tr(c, "engine.discovery.not_loaded")
		if s.discoveryLoaded {
			message = tr(c, "engine.discovery.none_found")
			if s.managedServiceActive() {
				message = tr(c, "engine.discovery.none_other_found")
			}
			if s.discovery.Status == "partial" || len(s.discovery.Limitations) > 0 {
				message = tr(c, "engine.discovery.partial")
			}
		}
		ui.Text(c, message)
	}
}

func (s *viewState) compatibleEndpointView(c *ui.Context, service inferyard.DiscoveredService, endpoint inferyard.DiscoveredEndpoint, bindable bool) {
	ui.Column(c.Key(fmt.Sprintf("service-%d-%s", service.PID, endpoint.URL))).Gap(tokens(c).Density.Row).Children(func() {
		heading(c, displayName(service.ProcessName), 3)
		ui.Text(c, fmt.Sprintf("PID %d · %s", service.PID, endpoint.URL))
		if endpoint.Health == nil {
			ui.Text(c, tr(c, "engine.discovery.health_missing"))
		} else {
			ui.Text(c, trf(c, "engine.discovery.health_value", "status", *endpoint.Health))
		}
		if endpoint.AmbiguousListener {
			warningBanner(c, tr(c, "engine.discovery.ambiguous_listener"))
		}
		if endpoint.Reason != nil {
			ui.Text(c, human(*endpoint.Reason))
		}
		componentFold(c, trf(c, "engine.discovery.model_catalog_count", "count", fmt.Sprintf("%d", len(endpoint.Models))), func() {
			for _, model := range endpoint.Models {
				ui.Text(c, displayName(model))
			}
		})
		if bindable {
			button(c, tr(c, "engine.discovery.bind")).Disabled(s.busy || s.discovering || !s.bindingPhase()).OnClick(func() {
				s.bindDiscovered(service.PID, endpoint.URL)
			})
		}
	})
}
