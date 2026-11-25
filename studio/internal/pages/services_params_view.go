package pages

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

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

				modelAsset, foundModel := s.findModelAsset(st.model)
				hasModel := foundModel && modelAsset.Available
				if st.mode != serviceParamsModeNew && !hasModel {
					noticeBanner(c, noticeBannerOptions{
						Kind:    stateWarning,
						Size:    bannerCompact,
						Message: tr(c, "services.params.model_missing"),
					})
				}
				s.verifyTaskEngines()
				engineOpts := s.serviceParamsEngineOptions(modelAsset, hasModel)
				if len(engineOpts) == 0 {
					st.engine = ""
					if hasModel {
						if s.checks.Checking > 0 {
							ui.Text(c, trf(c, "dashboard.candidate.verifying_engines", "count", s.checks.Checking)).TextColor(c.Theme().TextMuted)
						} else if !s.engineVerifyPending {
							noticeBanner(c, noticeBannerOptions{
								Kind:    stateWarning,
								Size:    bannerCompact,
								Message: tr(c, "models.serve.drawer.no_engine"),
							})
						}
					}
				} else {
					if !hasSelectOption(engineOpts, st.engine) {
						st.engine = s.defaultStandaloneEngine(modelAsset, engineOpts)
					}
					selectField(c, tr(c, "services.params.engine_label"), "", &st.engine, engineOpts)
				}

				if key := st.model + "\x00" + st.engine; st.model != "" && st.engine != "" && key != st.lastLookupKey {
					s.applyPresetForSelection(st.model, st.engine)
				}

				field(c, tr(c, "services.params.port_label"), &st.portText).Description(tr(c, "services.params.port_desc"))

				ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
							ui.Text(c, tr(c, "services.params.lan_label")).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
							ui.Text(c, tr(c, "services.params.lan_desc")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
						})
						components.Switch(c, &st.lanAccess, tr(c, "services.params.lan_switch_label"), components.SwitchOptions{HideLabel: true})
					})
					if st.lanAccess {
						noticeBanner(c, noticeBannerOptions{
							Kind:    stateWarning,
							Size:    bannerCompact,
							Message: tr(c, "services.params.lan_warning"),
						})
					}
				})

				ctxOpts := standaloneContextOptions(modelAsset)
				ctxHint := ""
				if hasModel && modelAsset.ContextLength != nil && *modelAsset.ContextLength > 0 {
					ctxHint = trf(c, "models.serve.drawer.context_limit", "limit", strconv.FormatInt(*modelAsset.ContextLength, 10))
				}
				selectField(c, tr(c, "services.params.context_label"), ctxHint, &st.contextPreset, ctxOpts)
				if st.contextPreset == standaloneCustomContext {
					field(c, tr(c, "models.serve.drawer.context_custom_label"), &st.customContext)
				}

				req, validationErr := s.parseServiceParamsRequest(modelAsset, hasModel)
				budgetCtx := req.ContextSize
				if budgetCtx <= 0 {
					budgetCtx = 8192
				}
				budget := s.checkMemoryBudget(st.model, st.engine, budgetCtx, req.GPULayers)
				if hasModel {
					s.serviceMemoryBudgetBar(c, budget)
				}

				componentFoldKeyed(c, "service-params-advanced", tr(c, "services.params.advanced_fold"), func() {
					ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
						field(c, tr(c, "services.params.gpu_layers_label"), &st.gpuLayersText).Description(tr(c, "services.params.gpu_layers_desc"))
						field(c, tr(c, "services.params.threads_label"), &st.threadsText).Description(tr(c, "services.params.threads_desc"))
						field(c, tr(c, "services.params.extra_args_label"), &st.extraArgsText).Description(tr(c, "services.params.extra_args_desc"))
					})
				})

				if validationErr != "" {
					noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: validationErr})
				}
				memErr := ""
				if hasModel && !budget.Known {
					memErr = s.standaloneMemoryShortfall(modelAsset)
					if memErr != "" {
						noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: memErr})
					}
				}
				if st.saveError != "" {
					noticeBanner(c, noticeBannerOptions{Kind: stateDanger, Size: bannerCompact, Message: st.saveError})
				}

				memEnough := !budget.Known || budget.Enough
				valid := validationErr == "" && hasModel && st.model != "" && st.engine != "" && len(engineOpts) > 0 && hasSelectOption(engineOpts, st.engine)
				canStart := valid && memEnough && memErr == "" && !s.busy && !s.standalone.isPreparing(st.model) && (s.serveSupported || s.preview != nil)
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
						restartID := ""
						if st.mode == serviceParamsModeRunning {
							restartID = st.serviceID
						}
						s.launchOrRestartService(req, restartID)
					}
				})
			})
		})
	})
}
