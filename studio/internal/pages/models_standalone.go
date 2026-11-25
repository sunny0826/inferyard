package pages

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
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
	preparing        bool
	preparingModel   string
	lastModel        string
	lastParams       map[string]standaloneAttempt
	lastError        *core.ApiError
	switchConfirm    bool
	switchModel      string
	switchPrefill    bool
	benchmarkConfirm bool
	benchmarkAction  func()
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

func compatibleStandaloneEngine(model core.ModelAsset, eng core.EngineAsset) bool {
	switch eng.Family {
	case "lmstudio", "mlx-lm":
		return false
	case "", "llama-cpp":
		return model.Format == "gguf"
	case "polystrata":
		if model.Format == "strata_pack" {
			return true
		}
		if model.Format != "gguf" {
			return false
		}
		return model.Architecture == "" || slices.Contains(polystrataSupportedGGUFArchs, model.Architecture)
	case "vllm", "sglang":
		return model.Format == "directory"
	default:
		return false
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
	if model.SizeBytes == nil || *model.SizeBytes <= 0 {
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
	if *model.SizeBytes > avail {
		return l10n.TRf(
			"models.serve.drawer.insufficient_memory",
			"avail", fmt.Sprintf("%.1f GiB", float64(avail)/(1<<30)),
			"size", fmt.Sprintf("%.1f GiB", float64(*model.SizeBytes)/(1<<30)),
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

func (s *viewState) standaloneRowState(m core.ModelAsset) (state string, endpoint string, logPath string, canWebUI bool) {
	if s.standalone.preparing && s.standalone.preparingModel == m.Name {
		return "preparing", "", "", false
	}
	st := s.engineState
	matchedModel := st.Origin == core.EngineOriginStandalone && (m.Path != "" && cleanMatchPath(st.Model) == cleanMatchPath(m.Path) || m.Path == "" && st.Model == m.Name)
	if matchedModel {
		switch st.State {
		case "starting", "running", "stopping":
			_, ok, _ := engineWebUIInfo(st)
			return st.State, loopbackEndpoint(st.Endpoint), st.LogPath, ok
		case "failed":
			return "failed", "", st.LogPath, false
		}
	}
	if s.standalone.lastError != nil && s.standalone.lastModel == m.Name {
		return "failed", "", "", false
	}
	return "", "", "", false
}

func (s *viewState) standaloneStartBlockReason() string {
	switch {
	case s.roundWorkActive():
		return l10n.TR("models.serve.blocked.run_active")
	case s.engineState.State == "orphaned":
		return l10n.TR("models.serve.blocked.orphaned")
	case !s.serveSupported:
		return l10n.TR("models.serve.blocked.cli_outdated")
	default:
		return ""
	}
}

func (s *viewState) openStandaloneDrawer(model core.ModelAsset, prefillLast bool) {
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
	if reason := s.standaloneStartBlockReason(); reason != "" || s.busy || s.standalone.preparing {
		return
	}
	// If the same model is already active, do not reopen drawer or stop it.
	if rowState, _, _, _ := s.standaloneRowState(model); rowState == "preparing" || rowState == "starting" || rowState == "running" || rowState == "stopping" {
		return
	}
	if s.managedServiceActive() {
		s.standalone.switchConfirm = true
		s.standalone.switchModel = model.Name
		s.standalone.switchPrefill = prefillLast
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
	s.standalone.drawerOpen = false
	s.standalone.preparing = true
	s.standalone.preparingModel = modelName
	s.standalone.lastModel = modelName
	if s.standalone.lastParams == nil {
		s.standalone.lastParams = map[string]standaloneAttempt{}
	}
	s.standalone.lastParams[modelName] = standaloneAttempt{engine: engineName, context: contextSize}
	s.standalone.lastError = nil
	if s.simulate(previewStandaloneStart{model: modelName, engine: engineName, contextSize: contextSize}) {
		return
	}
	if s.service == nil {
		s.standalone.preparing = false
		return
	}
	s.enqueueStatusToast(l10n.TR("models.serve.toast.started"))
	go func() {
		state, apiErr := s.service.StartStandalone(s.ctx, core.StandaloneStartRequest{
			Model:       modelName,
			Engine:      engineName,
			ContextSize: contextSize,
		})
		if s.ctx.Err() != nil {
			return
		}
		s.win.Update(func() {
			s.standalone.preparing = false
			s.standalone.preparingModel = ""
			if apiErr != nil {
				s.standalone.lastError = apiErr
				s.failure = apiErr
				s.failurePage = s.selected
				return
			}
			s.engineState = state
		})
	}()
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
		if _, apiErr := s.service.StopEngine(); apiErr != nil {
			s.win.Update(func() {
				s.failure = apiErr
				s.failurePage = s.selected
			})
			return
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if s.ctx.Err() != nil {
				return
			}
			st := s.service.Engine()
			if st.State == "" || st.State == "stopped" || st.State == "failed" {
				s.win.Update(func() {
					s.engineState = st
					if after != nil {
						after()
					}
				})
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		s.win.Update(func() {
			s.failure = core.Err("engine_stop_failed")
			s.failurePage = s.selected
		})
	}()
}

func (s *viewState) standaloneRunningForBenchmarkConflict() bool {
	return s.engineState.Origin == core.EngineOriginStandalone &&
		(s.engineState.State == "starting" || s.engineState.State == "running")
}

func (s *viewState) guardStandaloneForBenchmark(action func()) bool {
	if !s.standaloneRunningForBenchmarkConflict() {
		return true
	}
	s.standalone.benchmarkConfirm = true
	s.standalone.benchmarkAction = action
	return false
}

func (s *viewState) modelStandaloneActions(c *ui.Context, m core.ModelAsset) {
	if !m.Available {
		return
	}
	rowState, endpoint, logPath, canWebUI := s.standaloneRowState(m)
	switch rowState {
	case "preparing", "starting", "running", "stopping":
		kind := stateActive
		badgeKey := "models.serve.badge." + rowState
		if rowState == "running" {
			kind = stateSuccess
		}
		badge(c, tr(c, badgeKey), kind, badgeOptions{Style: badgeDot, Size: sizeSM})
		if endpoint != "" {
			ui.Text(c, endpoint).FontSize(tokens(c).Type.Caption).TextColor(tokens(c).Color.FgSecondary).SingleLine()
			outlineButton(c, tr(c, "engine.action.copy_endpoint")).OnClick(func() {
				s.copyEngineEndpoint(c, s.engineState.Endpoint)
			})
		}
		if canWebUI {
			outlineButton(c, tr(c, "engine.action.open_webui")).OnClick(func() {
				s.openEngineWebUI(c, s.engineState.Endpoint)
			})
		}
		if logPath != "" {
			outlineButton(c, tr(c, "engine.action.open_log")).OnClick(func() {
				s.openEngineCardLog(logPath)
			})
		}
		if rowState != "preparing" {
			dangerButton(c, tr(c, "models.serve.action.stop")).Disabled(rowState == "stopping" || s.busy).OnClick(s.stopStandaloneService)
		}
	case "failed":
		badge(c, tr(c, "models.serve.badge.failed"), stateDanger, badgeOptions{Style: badgeDot, Size: sizeSM})
		if logPath != "" {
			outlineButton(c, tr(c, "engine.action.open_log")).OnClick(func() {
				s.openEngineCardLog(logPath)
			})
		}
		retryDisabled := s.standaloneStartBlockReason() != "" || s.busy || s.standalone.preparing
		button(c, tr(c, "models.serve.action.retry")).Disabled(retryDisabled).OnClick(func() {
			s.requestStandaloneStart(m, true)
		})
	default:
		label := tr(c, "models.serve.action.start")
		reason := s.standaloneStartBlockReason()
		disabled := reason != "" || s.busy || s.standalone.preparing
		if disabled && reason != "" {
			wrap := ui.Box(c)
			wrap.Children(func() {
				primaryButton(c, label).Label(label + " · " + m.Name).Disabled(true).Description(reason)
			})
			components.Tooltip(c, wrap, reason)
			return
		}
		if primaryButton(c, label).Label(label + " · " + m.Name).Disabled(disabled).Clicked() {
			s.requestStandaloneStart(m, false)
		}
	}
}


