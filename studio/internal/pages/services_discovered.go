package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/inferyard"
)

func (s *viewState) discoveredServicesSection(c *ui.Context) {
	t := tokens(c)
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
				s.renderDiscoveredServiceCard(c, service, endpoint)
			}
		}
	}
	if compatible == 0 && s.discoveryError == nil && !s.discovering {
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
		ui.Text(c, message).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
	}
}

func (s *viewState) renderDiscoveredServiceCard(c *ui.Context, service inferyard.DiscoveredService, endpoint inferyard.DiscoveredEndpoint) {
	t := tokens(c)
	name := displayName(service.ProcessName)
	cardEl, innerR := compactCard(c, ui.Column(c.Key(fmt.Sprintf("disc-svc-%d-%s", service.PID, endpoint.URL))).FillWidth().Gap(t.Density.Row), t.Density.Stack)
	cardEl.Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			engineIconBox(c, name, 30, innerR)
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					ui.Text(c, name).FontWeight(600).FontSize(t.Type.Primary).SingleLine()
					inlineBadge(c, tr(c, "services.discovered.external_badge"), stateNeutral, badgeOptions{Size: sizeSM})
					inlineBadge(c, trf(c, "services.discovered.models_count", "count", len(endpoint.Models)), stateInfo, badgeOptions{Size: sizeSM})
				})
				ui.Text(c, fmt.Sprintf("PID %d · %s", service.PID, endpoint.URL)).
					FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			})
			studioTooltipGroup(c, ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row), func() {
				if validURL, ok := validLoopbackURL(endpoint.URL); ok {
					if outlineIconButton(c, reicon.Copy, tr(c, "services.discovered.copy_url")).Clicked() {
						s.copyEngineEndpoint(c, validURL)
					}
					if outlineIconButton(c, reicon.Code, tr(c, "services.discovered.usage")).Clicked() {
						targetModel := name
						if len(endpoint.Models) > 0 && strings.TrimSpace(endpoint.Models[0]) != "" {
							targetModel = endpoint.Models[0]
						}
						s.openServiceUsageDrawer(validURL, targetModel, name)
					}
				}
				if outlineIconButton(c, reicon.Gauge, tr(c, "services.discovered.benchmark")).Clicked() {
					s.startBenchmarkForDiscoveredService(service, endpoint)
				}
			})
		})
		if endpoint.AmbiguousListener {
			warningBanner(c, tr(c, "engine.discovery.ambiguous_listener"))
		}
		if len(endpoint.Models) > 0 {
			componentFold(c, trf(c, "engine.discovery.model_catalog_count", "count", fmt.Sprintf("%d", len(endpoint.Models))), func() {
				for _, model := range endpoint.Models {
					ui.Text(c, displayName(model)).FontSize(t.Type.Caption)
				}
			})
		}
	})
}
