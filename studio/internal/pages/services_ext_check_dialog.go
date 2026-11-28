package pages

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type extConnCheckDialogState struct {
	open         bool
	autoStart    bool
	serviceID    string
	modelName    string
	engineName   string
	endpoint     string
	contextSize  int64
	gpuLayers    *int
	probe        core.ExternalConnProbe
	memoryBudget core.ServiceMemoryBudget
	hasBudget    bool
}

func (s *viewState) startBenchmarkForManagedService(svc core.EngineState) {
	modelName := strings.TrimSpace(svc.ModelName)
	if modelName == "" {
		modelName = displayName(svc.Model)
	}
	engineName := strings.TrimSpace(svc.EngineName)
	if engineName == "" && svc.Family != "" {
		match := ""
		count := 0
		for _, eng := range s.registry.Engines {
			if eng.Family == svc.Family {
				match = eng.Name
				count++
			}
		}
		if count == 1 {
			engineName = match
		}
	}
	ctxSize := svc.ContextSize
	gpuLayers := svc.GPULayers
	serviceID := strings.TrimSpace(svc.ServiceID)
	endpoint := strings.TrimSpace(svc.Endpoint)

	finish := func(probe core.ExternalConnProbe) {
		s.handleExtConnProbeResult(serviceID, modelName, engineName, endpoint, ctxSize, gpuLayers, probe)
	}
	if s.probeServiceConnFunc != nil {
		finish(s.probeServiceConnFunc(s.ctx, serviceID))
		return
	}
	if s.preview != nil {
		if pm, ok := s.preview.(previewExclusivityMode); ok {
			finish(pm.probeServiceConnections(svc))
		} else {
			finish(core.ExternalConnProbe{Status: core.ExclusivityUnknown, Port: svc.Port})
		}
		return
	}
	if s.service == nil {
		finish(core.ExternalConnProbe{Status: core.ExclusivityUnknown, Port: svc.Port})
		return
	}
	if s.win == nil {
		finish(s.service.ProbeServiceConnections(s.ctx, serviceID))
		return
	}
	go func() {
		probe := s.service.ProbeServiceConnections(s.ctx, serviceID)
		s.runOnUI(func() { finish(probe) })
	}()
}

func (s *viewState) startBenchmarkForDiscoveredService(service inferyard.DiscoveredService, endpoint inferyard.DiscoveredEndpoint) {
	modelName := ""
	if len(endpoint.Models) > 0 {
		modelName = strings.TrimSpace(endpoint.Models[0])
	}
	if modelName == "" {
		modelName = s.model
	}
	epURL := strings.TrimSpace(endpoint.URL)

	finish := func(probe core.ExternalConnProbe) {
		s.handleExtConnProbeResult("", modelName, "", epURL, 0, nil, probe)
	}
	if s.probeEndpointConnFunc != nil {
		finish(s.probeEndpointConnFunc(s.ctx, epURL))
		return
	}
	if s.preview != nil {
		if pm, ok := s.preview.(previewExclusivityMode); ok {
			finish(pm.probeEndpointConnections(epURL))
		} else {
			finish(core.ExternalConnProbe{Status: core.ExclusivityUnknown})
		}
		return
	}
	if s.service == nil {
		finish(core.ExternalConnProbe{Status: core.ExclusivityUnknown})
		return
	}
	serverPID := int(service.PID)
	probeEndpoint := func() core.ExternalConnProbe {
		if serverPID > 0 {
			return s.service.ProbeEndpointConnections(s.ctx, epURL, serverPID)
		}
		return s.service.ProbeEndpointConnections(s.ctx, epURL)
	}
	if s.win == nil {
		finish(probeEndpoint())
		return
	}
	go func() {
		probe := probeEndpoint()
		s.runOnUI(func() { finish(probe) })
	}()
}

func (s *viewState) clearPendingExclusivity() {
	s.pendingExclusivity = ""
	s.pendingExclusivityModel = ""
	s.pendingExclusivityEngine = ""
	s.pendingExclusivityMode = ""
}

func (s *viewState) pendingExclusivityMatchesTarget() bool {
	if s.pendingExclusivityMode == "" {
		return false
	}
	if s.pendingExclusivityModel != "" && strings.TrimSpace(s.model) != strings.TrimSpace(s.pendingExclusivityModel) {
		return false
	}
	if s.pendingExclusivityEngine != "" && strings.TrimSpace(s.engine) != strings.TrimSpace(s.pendingExclusivityEngine) {
		return false
	}
	return true
}

func (s *viewState) applyServiceBenchmarkTarget(modelName, engineName string) {
	unmatchedModel := false
	resolvedModel := ""
	if modelName != "" {
		if m, ok := s.findModelAsset(modelName); ok && m.Available {
			resolvedModel = m.Name
		} else {
			unmatchedModel = true
		}
	}
	resolvedEngine := ""
	if engineName != "" {
		for _, eng := range s.registry.Engines {
			if eng.Name == engineName {
				resolvedEngine = eng.Name
				break
			}
		}
	} else if !unmatchedModel && resolvedModel != "" {
		resolvedEngine = s.resolveCompatibleEngineForModel(resolvedModel)
	}
	if s.speed != nil {
		s.speed.selected = false
	}
	if !s.roundWorkActive() && s.running() == nil {
		s.clearBenchmark()
		if s.speed != nil {
			s.speed.current, s.speed.opened = nil, nil
		}
		s.task.stage = "prepare_candidate"
		s.resumeSource = nil
		s.detailMu.Lock()
		s.requestedDetail = ""
		s.detailMu.Unlock()
		s.failure = nil
	}
	s.engine = resolvedEngine
	if resolvedEngine == "" {
		s.model = ""
	} else if resolvedModel != "" {
		if m, ok := s.findModelAsset(resolvedModel); ok && m.Available && s.compatibleModel(m) {
			s.model = resolvedModel
		} else {
			s.model = ""
		}
	} else if modelName != "" {
		s.model = ""
	} else if s.model != "" {
		if m, ok := s.findModelAsset(s.model); !ok || !m.Available || !s.compatibleModel(m) {
			s.model = ""
		}
	}
	s.skipAutoModelAdopt = unmatchedModel
}

func (s *viewState) handleExtConnProbeResult(serviceID, modelName, engineName, endpoint string, ctxSize int64, gpuLayers *int, probe core.ExternalConnProbe) {
	if probe.Status != core.ExclusivityExternal {
		s.clearPendingExclusivity()
		s.applyServiceBenchmarkTarget(modelName, engineName)
		if serviceID != "" {
			s.pendingExclusivityMode = "reuse"
			s.pendingExclusivityModel = strings.TrimSpace(modelName)
			s.pendingExclusivityEngine = strings.TrimSpace(engineName)
		}
		s.openBenchmarksFlow()
		return
	}
	// Pass empty engine so checkMemoryBudget does NOT exclude the currently running service
	// when estimating whether remaining memory can fit a second dedicated benchmark instance.
	budget := s.checkMemoryBudget(modelName, "", ctxSize, gpuLayers)
	s.extCheckDialog = extConnCheckDialogState{
		open:         true,
		serviceID:    serviceID,
		modelName:    modelName,
		engineName:   engineName,
		endpoint:     endpoint,
		contextSize:  ctxSize,
		gpuLayers:    gpuLayers,
		probe:        probe,
		memoryBudget: budget,
		hasBudget:    budget.Known,
	}
}

func (s *viewState) extConnCheckDialog(c *ui.Context) {
	if !s.extCheckDialog.open {
		studioDialog(c, &s.extCheckDialog.open, components.DialogOptions{Label: tr(c, "services.ext_check.title")}, nil)
		return
	}
	t := tokens(c)
	d := s.extCheckDialog
	title := tr(c, "services.ext_check.title")
	canDedicated := !d.hasBudget || d.memoryBudget.Enough

	studioDialog(c.Key("ext-conn-check-dialog"), &s.extCheckDialog.open, components.DialogOptions{Label: title}, func(panel ui.Element) {
		panel.Role(ui.RoleAlertDialog)
		components.Text(c, title).Bold()
		components.Text(c, trf(c, "services.ext_check.desc", "port", strconv.Itoa(d.probe.Port))).TextColor(t.Color.FgSecondary)

		if len(d.probe.Clients) > 0 {
			ui.Column(c).FillWidth().Padding(t.Density.Row).Radius(t.Radius.Control).
				Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Gap(t.Density.Hairline).Children(func() {
				ui.Text(c, tr(c, "services.ext_check.clients_label")).Bold().FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary)
				for _, client := range d.probe.Clients {
					ui.Text(c, client).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
				}
			})
		}

		if d.hasBudget {
			if canDedicated {
				components.Text(c, trf(c, "services.ext_check.memory_enough",
					"remaining", fmt.Sprintf("%.1f", d.memoryBudget.RemainingGB),
					"est", fmt.Sprintf("%.1f", d.memoryBudget.EstimatedGB),
				)).TextColor(t.Color.FgSecondary)
			} else {
				noticeBanner(c, noticeBannerOptions{
					Kind: stateWarning,
					Size: bannerCompact,
					Message: trf(c, "services.ext_check.memory_insufficient",
						"remaining", fmt.Sprintf("%.1f", d.memoryBudget.RemainingGB),
						"est", fmt.Sprintf("%.1f", d.memoryBudget.EstimatedGB),
					),
				})
			}
		}

		applyBenchmarkTarget := func(mode, exclusivity string) {
			if !d.autoStart {
				s.applyServiceBenchmarkTarget(d.modelName, d.engineName)
			}
			s.pendingExclusivityMode = mode
			s.pendingExclusivity = exclusivity
			s.pendingExclusivityModel = strings.TrimSpace(d.modelName)
			s.pendingExclusivityEngine = strings.TrimSpace(d.engineName)
			if d.autoStart {
				s.startBenchmark()
				return
			}
			s.openBenchmarksFlow()
		}

		ui.Row(c).FillWidth().Wrap().Justify(ui.End).Gap(t.Density.Row).Children(func() {
			if canDedicated {
				if button(c, tr(c, "services.ext_check.action.cancel")).Clicked() {
					s.extCheckDialog.open = false
				}
				if outlineButton(c, tr(c, "services.ext_check.action.benchmark_anyway")).Clicked() {
					s.extCheckDialog.open = false
					applyBenchmarkTarget("reuse", core.ExclusivityExternal)
				}
				if primaryButton(c, tr(c, "services.ext_check.action.dedicated_instance")).Clicked() {
					s.extCheckDialog.open = false
					applyBenchmarkTarget("isolated", "")
				}
			} else {
				if outlineButton(c, tr(c, "services.ext_check.action.benchmark_anyway")).Clicked() {
					s.extCheckDialog.open = false
					applyBenchmarkTarget("reuse", core.ExclusivityExternal)
				}
				if primaryButton(c, tr(c, "services.ext_check.action.cancel")).Clicked() {
					s.extCheckDialog.open = false
				}
			}
		})
	})
}
