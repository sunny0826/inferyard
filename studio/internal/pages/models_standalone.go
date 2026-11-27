package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

const standaloneCustomContext = "custom"

var standaloneContextPresets = []int64{4096, 8192, 16384, 32768, 65536, 131072}

type standaloneAttempt struct {
	engine  string
	context int64
}

type standaloneServeState struct {
	drawerOpen       bool
	modelName        string
	engineName       string
	contextPreset    string
	customContext    string
	preparing        map[string]bool
	lastModel        string
	lastParams       map[string]standaloneAttempt
	lastErrors       map[string]*core.ApiError
	switchConfirm    bool
	switchModel      string
	switchPrefill    bool
	switchAction     func()
	benchmarkConfirm bool
	benchmarkAction  func()
}

func (st *standaloneServeState) isPreparing(model string) bool {
	return model != "" && st.preparing[model]
}

func (st *standaloneServeState) setPreparing(model string, active bool) {
	if model == "" {
		return
	}
	if active {
		if st.preparing == nil {
			st.preparing = map[string]bool{}
		}
		st.preparing[model] = true
		return
	}
	delete(st.preparing, model)
}

func (st *standaloneServeState) setLastError(model string, err *core.ApiError) {
	if model == "" {
		return
	}
	if err == nil {
		delete(st.lastErrors, model)
		return
	}
	if st.lastErrors == nil {
		st.lastErrors = map[string]*core.ApiError{}
	}
	st.lastErrors[model] = err
}

func engineOriginLabel(origin string) string {
	switch origin {
	case core.EngineOriginStandalone:
		return l10n.TR("engine.origin.standalone")
	case core.EngineOriginContextSpeed:
		return l10n.TR("engine.origin.context_speed")
	default:
		return l10n.TR("engine.origin.benchmark")
	}
}

func (s *viewState) standaloneEngineOptions(model core.ModelAsset) []components.NativeSelectOption[string] {
	options := []components.NativeSelectOption[string]{}
	for _, eng := range s.registry.Engines {
		if !compatibleStandaloneEngine(model, eng) {
			continue
		}
		if s.eligibility(eng.Name).State != core.EngineCheckEligible {
			continue
		}
		options = append(options, components.NativeSelectOption[string]{Value: eng.Name, Label: eng.Name})
	}
	return options
}

func (s *viewState) defaultStandaloneEngine(model core.ModelAsset, options []components.NativeSelectOption[string]) string {
	if len(options) == 0 {
		return ""
	}
	if s.engine != "" {
		for _, opt := range options {
			if opt.Value == s.engine {
				return opt.Value
			}
		}
	}
	return options[0].Value
}

func snapshotInt64(raw any) (int64, bool) {
	switch v := raw.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n, true
		}
		if f, err := v.Float64(); err == nil {
			return int64(f), true
		}
	case float64:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

func (s *viewState) recommendedStandaloneContext(model core.ModelAsset) int64 {
	var rec int64
	if model.Path != "" {
		if snap, err := parseDeviceSnapshot(s.device.Snapshot); err == nil && snap != nil {
			if r := snap.object("recommendation"); r != nil {
				if sel, ok := r["selected"].(map[string]any); ok {
					selPath, _ := sel["model_path"].(string)
					if selPath != "" && cleanMatchPath(selPath) == cleanMatchPath(model.Path) {
						if v, ok := snapshotInt64(sel["context_size"]); ok && v > 0 {
							rec = v
						}
					}
				}
			}
		}
	}
	if rec <= 0 {
		rec = 8192
	}
	if model.ContextLength != nil && *model.ContextLength > 0 && rec > *model.ContextLength {
		rec = *model.ContextLength
	}
	return rec
}

func standaloneContextOptions(model core.ModelAsset) []components.NativeSelectOption[string] {
	options := []components.NativeSelectOption[string]{}
	for _, v := range standaloneContextPresets {
		if model.ContextLength != nil && *model.ContextLength > 0 && v > *model.ContextLength {
			continue
		}
		options = append(options, components.NativeSelectOption[string]{
			Value: strconv.FormatInt(v, 10),
			Label: formatContextTokens(v),
		})
	}
	options = append(options, components.NativeSelectOption[string]{
		Value: standaloneCustomContext,
		Label: l10n.TR("models.serve.drawer.context_custom"),
	})
	return options
}

func (s *viewState) standaloneMemoryShortfall(model core.ModelAsset) string {
	memBytes := model.EffectiveMemoryBytes()
	if memBytes == nil || *memBytes <= 0 {
		return ""
	}
	snap, err := parseDeviceSnapshot(s.device.Snapshot)
	if err != nil || snap == nil {
		return ""
	}
	hw := snap.object("hardware")
	if hw == nil {
		return ""
	}
	avail, ok := snapshotInt64(hw["memory_available_bytes"])
	if !ok || avail <= 0 {
		return ""
	}
	if *memBytes > avail {
		return l10n.TRf(
			"models.serve.drawer.insufficient_memory",
			"avail", fmt.Sprintf("%.1f GiB", float64(avail)/(1<<30)),
			"size", fmt.Sprintf("%.1f GiB", float64(*memBytes)/(1<<30)),
		)
	}
	return ""
}

func (s *viewState) findModelAsset(name string) (core.ModelAsset, bool) {
	for _, m := range s.registry.Models {
		if m.Name == name {
			return m, true
		}
	}
	return core.ModelAsset{}, false
}

func standaloneServiceMatchesModel(st core.EngineState, m core.ModelAsset) bool {
	if st.Origin != core.EngineOriginStandalone {
		return false
	}
	if m.Name != "" && st.ModelName != "" && st.ModelName == m.Name {
		return true
	}
	if m.Name != "" && st.EngineName != "" && st.ServiceID != "" && st.ServiceID == core.ServiceIDFor(m.Name, st.EngineName) {
		return true
	}
	if m.Path != "" && st.Model != "" && cleanMatchPath(st.Model) == cleanMatchPath(m.Path) {
		return true
	}
	return m.Name != "" && st.Model == m.Name
}

func (s *viewState) standaloneRowState(m core.ModelAsset) (state string, svc core.EngineState, endpoint string, logPath string, canWebUI bool) {
	if s.standalone.isPreparing(m.Name) {
		return "preparing", core.EngineState{}, "", "", false
	}
	for _, st := range s.effectiveManagedServices() {
		if !standaloneServiceMatchesModel(st, m) {
			continue
		}
		switch st.State {
		case "starting", "running", "stopping":
			ep, _ := serviceEndpointDisplay(st)
			_, ok, _ := serviceWebUIInfo(st)
			return st.State, st, ep, st.LogPath, ok
		}
	}
	if standaloneServiceMatchesModel(s.engineState, m) && s.engineState.State == "failed" {
		return "failed", s.engineState, "", s.engineState.LogPath, false
	}
	if s.standalone.lastErrors[m.Name] != nil {
		return "failed", core.EngineState{}, "", "", false
	}
	return "", core.EngineState{}, "", "", false
}

func (s *viewState) standaloneStartBlockReason() string {
	if s.environmentMissing() {
		if s.ready.CLI.Status == "outdated" && s.ready.Workspace.Initialized && s.ready.Results.Exists && s.ready.Models.Exists {
			return l10n.TR("models.serve.blocked.cli_outdated")
		}
		return l10n.TR("models.serve.blocked.env_missing")
	}
	if s.busy {
		return l10n.TR("models.action.blocked.busy")
	}
	if s.roundWorkActive() {
		return l10n.TR("models.serve.blocked.run_active")
	}
	if s.engineState.State == "orphaned" || s.hasOrphanService("") {
		return l10n.TR("models.serve.blocked.orphaned")
	}
	if !s.serveSupported && !s.serveProbing {
		return l10n.TR("models.serve.blocked.cli_outdated")
	}
	return ""
}

func (s *viewState) openStandaloneDrawer(model core.ModelAsset, prefillLast bool) {
	s.verifyTaskEngines()
	s.adoptDownloadedEngine()
	options := s.standaloneEngineOptions(model)
	rec := s.recommendedStandaloneContext(model)
	attempt, hasAttempt := s.standalone.lastParams[model.Name]
	if prefillLast && hasAttempt && attempt.context > 0 {
		if model.ContextLength == nil || *model.ContextLength <= 0 || attempt.context <= *model.ContextLength {
			rec = attempt.context
		}
	}
	preset := standaloneCustomContext
	for _, v := range standaloneContextPresets {
		if v == rec && (model.ContextLength == nil || *model.ContextLength <= 0 || v <= *model.ContextLength) {
			preset = strconv.FormatInt(v, 10)
			break
		}
	}
	selectedEngine := s.defaultStandaloneEngine(model, options)
	if prefillLast && hasAttempt && attempt.engine != "" {
		for _, opt := range options {
			if opt.Value == attempt.engine {
				selectedEngine = opt.Value
				break
			}
		}
	}
	s.standalone.drawerOpen = true
	s.standalone.modelName = model.Name
	s.standalone.engineName = selectedEngine
	s.standalone.contextPreset = preset
	s.standalone.customContext = strconv.FormatInt(rec, 10)
}

func (s *viewState) headerStandaloneModel() (core.ModelAsset, bool) {
	if s.engineState.Model != "" {
		want := cleanMatchPath(s.engineState.Model)
		for _, m := range s.registry.Models {
			if m.Available && m.Path != "" && cleanMatchPath(m.Path) == want {
				return m, true
			}
		}
	}
	if m, ok := s.findModelAsset(s.standalone.lastModel); ok && m.Available {
		return m, true
	}
	return core.ModelAsset{}, false
}

func (s *viewState) openStandaloneFromHeader(prefillLast bool) {
	target, hasTarget := s.headerStandaloneModel()
	openNow := func() {
		s.libraryTab = 0
		s.selectPageNow("library")
		if hasTarget {
			s.requestStandaloneStart(target, prefillLast)
		}
	}
	if s.guardLeaveSettings("library", openNow) {
		return
	}
	openNow()
}

func (s *viewState) requestStandaloneStart(model core.ModelAsset, prefillLast bool) {
	if reason := s.standaloneStartBlockReason(); reason != "" || s.serveProbing || s.standalone.isPreparing(model.Name) {
		return
	}
	// If the same model is already active, do not reopen drawer or stop it.
	if rowState, _, _, _, _ := s.standaloneRowState(model); rowState == "preparing" || rowState == "starting" || rowState == "running" || rowState == "stopping" {
		return
	}
	s.openStandaloneDrawer(model, prefillLast)
}

func (s *viewState) resolvedStandaloneContext(model core.ModelAsset) (int64, string) {
	raw := s.standalone.contextPreset
	if raw == standaloneCustomContext {
		raw = strings.TrimSpace(s.standalone.customContext)
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || val <= 0 || (model.ContextLength != nil && *model.ContextLength > 0 && val > *model.ContextLength) {
		if model.ContextLength != nil && *model.ContextLength > 0 {
			return 0, l10n.TRf("models.serve.drawer.context_invalid", "limit", strconv.FormatInt(*model.ContextLength, 10))
		}
		return 0, l10n.TR("models.serve.drawer.context_invalid_nolimit")
	}
	return val, ""
}

func (s *viewState) launchStandalone(modelName, engineName string, contextSize int64) {
	s.launchStandaloneRequest(core.StandaloneStartRequest{
		Model:       modelName,
		Engine:      engineName,
		ContextSize: contextSize,
	})
}

func (s *viewState) stopStandaloneService() {
	if s.simulate(previewStopEngine) {
		return
	}
	if s.service == nil {
		return
	}
	s.enqueueStatusToast(l10n.TR("models.serve.toast.stopping"))
	s.work(func() (string, *core.ApiError) {
		state, apiErr := s.service.StopEngine()
		if apiErr == nil {
			s.win.Update(func() { s.engineState = state })
		}
		return "", apiErr
	})
}

func (s *viewState) stopActiveServiceThen(after func()) {
	if s.preview != nil {
		s.simulate(previewStopEngineThen{after: after})
		return
	}
	if s.service == nil {
		return
	}
	go func() {
		parentCtx := s.ctx
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		stopCtx, cancel := context.WithTimeout(parentCtx, 15*time.Second)
		defer cancel()
		apiErr := s.service.StopActiveServicesAndWait(stopCtx)
		if parentCtx.Err() != nil {
			return
		}
		if apiErr != nil {
			s.runOnUI(func() {
				s.failure = apiErr
				s.failurePage = s.selected
			})
			return
		}
		s.runOnUI(func() {
			s.managedServices = s.service.ManagedServices()
			s.engineState = s.service.Engine()
			if after != nil {
				after()
			}
		})
	}()
}

func (s *viewState) reusableStandaloneService(modelName, engineName string) (core.EngineState, bool) {
	e := s.localEngine()
	if e == nil || e.Family == "lmstudio" || e.Family == "mlx-lm" || modelName == "" || engineName == "" {
		return core.EngineState{}, false
	}
	modelAsset, hasModel := s.findModelAsset(modelName)
	for _, st := range s.activeStandaloneServices() {
		if st.State != "running" || st.Pid <= 0 {
			continue
		}
		modelMatch := (st.ModelName != "" && st.ModelName == modelName) ||
			displayName(st.Model) == modelName ||
			(hasModel && standaloneServiceMatchesModel(st, modelAsset))
		engineMatch := st.EngineName == engineName || (st.EngineName == "" && st.Family == e.Family)
		if !modelMatch || !engineMatch {
			continue
		}
		if e.Family == "polystrata" {
			if st.ContextSize > 0 && st.ContextSize < 4096 {
				continue
			}
		} else if st.ContextSize < 8192 {
			continue
		}
		port := st.Port
		if port <= 0 {
			if localURL, ok := serviceLocalURL(st); ok {
				if idx := strings.LastIndex(localURL, ":"); idx >= 0 {
					port, _ = strconv.Atoi(localURL[idx+1:])
				}
			}
		}
		if port <= 0 {
			continue
		}
		reused := st
		reused.Port = port
		reused.Endpoint = fmt.Sprintf("http://127.0.0.1:%d", port)
		return reused, true
	}
	return core.EngineState{}, false
}

func (s *viewState) standaloneRunningForBenchmarkConflict() bool {
	return len(s.activeStandaloneServices()) > 0
}

func (s *viewState) guardStandaloneForBenchmark(action func()) bool {
	if !s.standaloneRunningForBenchmarkConflict() {
		return true
	}
	s.standalone.benchmarkConfirm = true
	s.standalone.benchmarkAction = action
	return false
}
