package pages

import (
	"errors"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
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
	model         string
	engine        string
	lastLookupKey string
	portText      string
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
	st := s.engineState
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
	preset := core.ServicePreset{
		Model:       modelName,
		Engine:      engineName,
		Port:        st.Port,
		ContextSize: st.ContextSize,
		GPULayers:   cloneIntPtrUI(st.GPULayers),
		Threads:     cloneIntPtrUI(st.Threads),
		ExtraArgs:   append([]string(nil), st.ExtraArgs...),
	}
	if preset.ContextSize <= 0 {
		preset.ContextSize = 8192
	}
	s.serviceParams = serviceParamsDrawerState{
		open:   true,
		mode:   serviceParamsModeRunning,
		model:  modelName,
		engine: engineName,
	}
	s.populateServiceParamsFromPreset(preset)
	s.serviceParams.lastLookupKey = modelName + "\x00" + engineName
}

func (s *viewState) openServiceParamsDrawerFromExitNotice(notice core.ServiceExitNotice) {
	preset := core.ServicePreset{
		Model:       notice.Model,
		Engine:      notice.Engine,
		Port:        notice.Port,
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

func (s *viewState) serviceParamsDrawer(c *ui.Context) {
	titleKey := "services.params.title.new"
	switch s.serviceParams.mode {
	case serviceParamsModePreset:
		titleKey = "services.params.title.preset"
	case serviceParamsModeRunning:
		titleKey = "services.params.title.running"
	}
	drawer(c, &s.serviceParams.open, drawerOptions{
		Title: tr(c, titleKey),
		Width: c.Theme().Space(136),
	}, func(_ ui.Element) {
		t := tokens(c)
		st := &s.serviceParams
		ui.Scroll(c.Key("service-params-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				modelOpts := s.availableModelSelectOptions()
				if st.mode == serviceParamsModeNew {
					if len(modelOpts) == 0 {
						noticeBanner(c, noticeBannerOptions{
							Kind:    stateWarning,
							Size:    bannerCompact,
							Message: tr(c, "services.params.no_models"),
						})
					} else {
						if !hasSelectOption(modelOpts, st.model) {
							st.model = modelOpts[0].Value
						}
						selectField(c, tr(c, "services.params.model_label"), "", &st.model, modelOpts)
					}
				} else {
					keyValueRow(c, tr(c, "services.params.model_label"), st.model, false)
				}

				modelAsset, hasModel := s.findModelAsset(st.model)
				engineOpts := s.serviceParamsEngineOptions(modelAsset, hasModel, st.engine)
				if len(engineOpts) == 0 {
					st.engine = ""
					noticeBanner(c, noticeBannerOptions{
						Kind:    stateWarning,
						Size:    bannerCompact,
						Message: tr(c, "models.serve.drawer.no_engine"),
					})
				} else {
					if !hasSelectOption(engineOpts, st.engine) {
						if hasModel {
							st.engine = s.defaultStandaloneEngine(modelAsset, engineOpts)
						} else {
							st.engine = engineOpts[0].Value
						}
					}
					selectField(c, tr(c, "services.params.engine_label"), "", &st.engine, engineOpts)
				}

				if key := st.model + "\x00" + st.engine; st.model != "" && st.engine != "" && key != st.lastLookupKey {
					s.applyPresetForSelection(st.model, st.engine)
				}

				field(c, tr(c, "services.params.port_label"), &st.portText).Description(tr(c, "services.params.port_desc"))

				ctxOpts := standaloneContextOptions(modelAsset)
				ctxHint := ""
				if hasModel && modelAsset.ContextLength != nil && *modelAsset.ContextLength > 0 {
					ctxHint = trf(c, "models.serve.drawer.context_limit", "limit", strconv.FormatInt(*modelAsset.ContextLength, 10))
				}
				selectField(c, tr(c, "services.params.context_label"), ctxHint, &st.contextPreset, ctxOpts)
				if st.contextPreset == standaloneCustomContext {
					field(c, tr(c, "models.serve.drawer.context_custom_label"), &st.customContext)
				}

				componentFoldKeyed(c, "service-params-advanced", tr(c, "services.params.advanced_fold"), func() {
					ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
						field(c, tr(c, "services.params.gpu_layers_label"), &st.gpuLayersText).Description(tr(c, "services.params.gpu_layers_desc"))
						field(c, tr(c, "services.params.threads_label"), &st.threadsText).Description(tr(c, "services.params.threads_desc"))
						field(c, tr(c, "services.params.extra_args_label"), &st.extraArgsText).Description(tr(c, "services.params.extra_args_desc"))
					})
				})

				req, validationErr := s.parseServiceParamsRequest(modelAsset, hasModel)
				if validationErr != "" {
					noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: validationErr})
				}
				memErr := ""
				if hasModel {
					memErr = s.standaloneMemoryShortfall(modelAsset)
					if memErr != "" {
						noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: memErr})
					}
				}
				if st.saveError != "" {
					noticeBanner(c, noticeBannerOptions{Kind: stateDanger, Size: bannerCompact, Message: st.saveError})
				}

				valid := validationErr == "" && st.model != "" && st.engine != "" && len(engineOpts) > 0 && hasSelectOption(engineOpts, st.engine)
				canStart := valid && memErr == "" && !s.busy && !s.standalone.preparing && (s.serveSupported || s.preview != nil)
				startLabel := tr(c, "services.params.action.start")
				if st.mode == serviceParamsModeRunning {
					startLabel = tr(c, "services.params.action.save_restart")
				}

				ui.Row(c).FillWidth().Wrap().Justify(ui.End).Gap(t.Density.Row).Children(func() {
					if outlineButton(c, tr(c, "services.params.action.cancel")).Clicked() {
						st.open = false
					}
					if outlineButton(c, tr(c, "services.params.action.save_only")).Disabled(!valid).Clicked() {
						if s.saveServicePresetFromRequest(req) {
							st.open = false
						}
					}
					if primaryButton(c, startLabel).Disabled(!canStart).Clicked() {
						st.saveError = ""
						s.requestStandaloneLaunchWithConfirm(req, st.mode == serviceParamsModeRunning)
					}
				})
			})
		})
	})
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

func (s *viewState) serviceParamsEngineOptions(model core.ModelAsset, hasModel bool, current string) []components.NativeSelectOption[string] {
	if hasModel {
		return s.standaloneEngineOptions(model)
	}
	var opts []components.NativeSelectOption[string]
	for _, eng := range s.registry.Engines {
		opts = append(opts, components.NativeSelectOption[string]{Value: eng.Name, Label: eng.Name})
	}
	if current != "" && !hasSelectOption(opts, current) {
		opts = append(opts, components.NativeSelectOption[string]{Value: current, Label: current})
	}
	return opts
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
