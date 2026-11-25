package pages

import (
	"errors"
	"strconv"
	"strings"

	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

const (
	serviceParamsModeNew     = "new"
	serviceParamsModePreset  = "preset"
	serviceParamsModeRunning = "running"
)

type serviceParamsDrawerState struct {
	open          bool
	mode          string
	serviceID     string
	model         string
	engine        string
	lastLookupKey string
	portText      string
	lanAccess     bool
	contextPreset string
	customContext string
	gpuLayersText string
	threadsText   string
	extraArgsText string
	saveError     string
}

func (s *viewState) openServiceParamsDrawerForNew() {
	s.adoptDownloadedEngine()
	modelName := s.defaultServiceModelName()
	engineName := s.defaultServiceEngineForModel(modelName)
	s.serviceParams = serviceParamsDrawerState{
		open:   true,
		mode:   serviceParamsModeNew,
		model:  modelName,
		engine: engineName,
	}
	s.applyPresetForSelection(modelName, engineName)
}

func (s *viewState) openServiceParamsDrawerForPreset(preset core.ServicePreset) {
	s.serviceParams = serviceParamsDrawerState{
		open:   true,
		mode:   serviceParamsModePreset,
		model:  preset.Model,
		engine: preset.Engine,
	}
	s.populateServiceParamsFromPreset(preset)
	s.serviceParams.lastLookupKey = preset.Model + "\x00" + preset.Engine
}

func (s *viewState) openServiceParamsDrawerForRunning() {
	s.openServiceParamsDrawerForService(s.engineState)
}

func (s *viewState) openServiceParamsDrawerForService(st core.EngineState) {
	modelName := strings.TrimSpace(st.ModelName)
	if modelName == "" {
		if m, ok := s.headerStandaloneModel(); ok {
			modelName = m.Name
		} else {
			modelName = displayName(st.Model)
		}
	}
	engineName := strings.TrimSpace(st.EngineName)
	if engineName == "" {
		engineName = s.defaultServiceEngineForModel(modelName)
	}
	requestedPort := st.RequestedPort
	if requestedPort == 0 {
		if saved, ok := s.lookupServicePreset(modelName, engineName); ok {
			requestedPort = saved.Port
		}
	}
	preset := core.ServicePreset{
		Model:       modelName,
		Engine:      engineName,
		Port:        requestedPort,
		LANAccess:   st.LANAccess,
		ContextSize: st.ContextSize,
		GPULayers:   cloneIntPtrUI(st.GPULayers),
		Threads:     cloneIntPtrUI(st.Threads),
		ExtraArgs:   append([]string(nil), st.ExtraArgs...),
	}
	if preset.ContextSize <= 0 {
		preset.ContextSize = 8192
	}
	s.serviceParams = serviceParamsDrawerState{
		open:      true,
		mode:      serviceParamsModeRunning,
		serviceID: st.ServiceID,
		model:     modelName,
		engine:    engineName,
	}
	s.populateServiceParamsFromPreset(preset)
	s.serviceParams.lastLookupKey = modelName + "\x00" + engineName
}

func (s *viewState) openServiceParamsDrawerFromExitNotice(notice core.ServiceExitNotice) {
	preset := core.ServicePreset{
		Model:       notice.Model,
		Engine:      notice.Engine,
		Port:        notice.RequestedPort,
		LANAccess:   notice.LANAccess,
		ContextSize: notice.ContextSize,
		GPULayers:   cloneIntPtrUI(notice.GPULayers),
		Threads:     cloneIntPtrUI(notice.Threads),
		ExtraArgs:   append([]string(nil), notice.ExtraArgs...),
	}
	s.openServiceParamsDrawerForPreset(preset)
}

func (s *viewState) defaultServiceModelName() string {
	if s.model != "" {
		if m, ok := s.findModelAsset(s.model); ok && m.Available {
			return m.Name
		}
	}
	for _, m := range s.registry.Models {
		if m.Available {
			return m.Name
		}
	}
	return ""
}

func (s *viewState) defaultServiceEngineForModel(modelName string) string {
	if m, ok := s.findModelAsset(modelName); ok {
		opts := s.standaloneEngineOptions(m)
		if len(opts) > 0 {
			return s.defaultStandaloneEngine(m, opts)
		}
	}
	if s.engine != "" {
		return s.engine
	}
	if len(s.registry.Engines) > 0 {
		return s.registry.Engines[0].Name
	}
	return ""
}

func (s *viewState) lookupServicePreset(model, engine string) (core.ServicePreset, bool) {
	if s.service != nil && s.preview == nil {
		if preset, ok := s.service.ServicePresetFor(model, engine); ok {
			return preset, true
		}
	}
	for _, p := range s.recentServices {
		if p.Model == model && p.Engine == engine {
			return p, true
		}
	}
	return core.ServicePreset{}, false
}

func (s *viewState) applyPresetForSelection(model, engine string) {
	s.serviceParams.lastLookupKey = model + "\x00" + engine
	if preset, ok := s.lookupServicePreset(model, engine); ok {
		s.populateServiceParamsFromPreset(preset)
		return
	}
	rec := int64(8192)
	if m, ok := s.findModelAsset(model); ok {
		rec = s.recommendedStandaloneContext(m)
	}
	s.populateServiceParamsFromPreset(core.ServicePreset{
		Model:       model,
		Engine:      engine,
		ContextSize: rec,
	})
}

func (s *viewState) populateServiceParamsFromPreset(preset core.ServicePreset) {
	st := &s.serviceParams
	if preset.Port > 0 {
		st.portText = strconv.Itoa(preset.Port)
	} else {
		st.portText = ""
	}
	st.lanAccess = preset.LANAccess
	ctxVal := preset.ContextSize
	if ctxVal <= 0 {
		ctxVal = 8192
	}
	model, hasModel := s.findModelAsset(preset.Model)
	st.contextPreset = standaloneCustomContext
	for _, v := range standaloneContextPresets {
		if v == ctxVal && (!hasModel || model.ContextLength == nil || *model.ContextLength <= 0 || v <= *model.ContextLength) {
			st.contextPreset = strconv.FormatInt(v, 10)
			break
		}
	}
	st.customContext = strconv.FormatInt(ctxVal, 10)
	if preset.GPULayers != nil {
		st.gpuLayersText = strconv.Itoa(*preset.GPULayers)
	} else {
		st.gpuLayersText = ""
	}
	if preset.Threads != nil {
		st.threadsText = strconv.Itoa(*preset.Threads)
	} else {
		st.threadsText = ""
	}
	st.extraArgsText = joinExtraArgs(preset.ExtraArgs)
}

func (s *viewState) availableModelSelectOptions() []components.NativeSelectOption[string] {
	opts := []components.NativeSelectOption[string]{}
	for _, m := range s.registry.Models {
		if !m.Available {
			continue
		}
		opts = append(opts, components.NativeSelectOption[string]{Value: m.Name, Label: modelLabel(m)})
	}
	return opts
}

func (s *viewState) serviceParamsEngineOptions(model core.ModelAsset, hasModel bool) []components.NativeSelectOption[string] {
	if !hasModel {
		return nil
	}
	return s.standaloneEngineOptions(model)
}

func hasSelectOption(opts []components.NativeSelectOption[string], value string) bool {
	for _, opt := range opts {
		if opt.Value == value {
			return true
		}
	}
	return false
}

func (s *viewState) parseServiceParamsRequest(model core.ModelAsset, hasModel bool) (core.StandaloneStartRequest, string) {
	st := &s.serviceParams
	var port int
	if rawPort := strings.TrimSpace(st.portText); rawPort != "" {
		v, err := strconv.Atoi(rawPort)
		if err != nil || v < 0 || v > 65535 {
			return core.StandaloneStartRequest{}, l10n.TR("services.params.err.port")
		}
		port = v
	}

	rawCtx := st.contextPreset
	if rawCtx == standaloneCustomContext {
		rawCtx = strings.TrimSpace(st.customContext)
	}
	ctxVal, err := strconv.ParseInt(rawCtx, 10, 64)
	if err != nil || ctxVal <= 0 || (hasModel && model.ContextLength != nil && *model.ContextLength > 0 && ctxVal > *model.ContextLength) {
		if hasModel && model.ContextLength != nil && *model.ContextLength > 0 {
			return core.StandaloneStartRequest{}, l10n.TRf("models.serve.drawer.context_invalid", "limit", strconv.FormatInt(*model.ContextLength, 10))
		}
		return core.StandaloneStartRequest{}, l10n.TR("models.serve.drawer.context_invalid_nolimit")
	}

	var gpuLayers *int
	if rawGPU := strings.TrimSpace(st.gpuLayersText); rawGPU != "" {
		v, err := strconv.Atoi(rawGPU)
		if err != nil || v < -1 {
			return core.StandaloneStartRequest{}, l10n.TR("services.params.err.gpu_layers")
		}
		gpuLayers = &v
	}

	var threads *int
	if rawThreads := strings.TrimSpace(st.threadsText); rawThreads != "" {
		v, err := strconv.Atoi(rawThreads)
		if err != nil || v <= 0 {
			return core.StandaloneStartRequest{}, l10n.TR("services.params.err.threads")
		}
		threads = &v
	}

	extraArgs, ok := splitExtraArgs(st.extraArgsText)
	if !ok {
		return core.StandaloneStartRequest{}, l10n.TR("services.params.err.extra_args")
	}

	return core.StandaloneStartRequest{
		Model:       st.model,
		Engine:      st.engine,
		ContextSize: ctxVal,
		Port:        port,
		LANAccess:   st.lanAccess,
		GPULayers:   gpuLayers,
		Threads:     threads,
		ExtraArgs:   extraArgs,
	}, ""
}

func (s *viewState) saveServicePresetFromRequest(req core.StandaloneStartRequest) bool {
	preset := core.ServicePreset{
		Model:       req.Model,
		Engine:      req.Engine,
		Port:        req.Port,
		LANAccess:   req.LANAccess,
		ContextSize: req.ContextSize,
		GPULayers:   cloneIntPtrUI(req.GPULayers),
		Threads:     cloneIntPtrUI(req.Threads),
		ExtraArgs:   append([]string(nil), req.ExtraArgs...),
	}
	if s.simulate(previewSaveServicePreset{preset: preset}) {
		s.serviceParams.saveError = ""
		s.enqueueStatusToast(l10n.TR("services.params.toast.saved"))
		return true
	}
	if s.service == nil {
		s.upsertRecentServiceLocal(preset)
		s.serviceParams.saveError = ""
		s.enqueueStatusToast(l10n.TR("services.params.toast.saved"))
		return true
	}
	if err := s.service.SaveServicePreset(preset); err != nil {
		var apiErr *core.ApiError
		if errors.As(err, &apiErr) && apiErr != nil {
			if apiErr.Message != "" {
				s.serviceParams.saveError = apiErr.Message
			} else {
				s.serviceParams.saveError = human(apiErr.Code)
			}
		} else {
			s.serviceParams.saveError = l10n.TR("services.params.toast.save_failed")
		}
		s.enqueueStatusToast(l10n.TR("services.params.toast.save_failed"))
		return false
	}
	s.serviceParams.saveError = ""
	s.recentServices = s.service.RecentServices()
	s.bumpServiceSnapshotSeq()
	s.enqueueStatusToast(l10n.TR("services.params.toast.saved"))
	return true
}

func (s *viewState) upsertRecentServiceLocal(preset core.ServicePreset) {
	out := make([]core.ServicePreset, 0, len(s.recentServices)+1)
	out = append(out, preset)
	for _, item := range s.recentServices {
		if item.Model == preset.Model && item.Engine == preset.Engine {
			continue
		}
		out = append(out, item)
	}
	s.recentServices = out
}
