package pages

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

func (s *viewState) standaloneDrawer(c *ui.Context) {
	drawer(c, &s.standalone.drawerOpen, drawerOptions{
		Title: tr(c, "models.serve.drawer.title"),
	}, func(_ ui.Element) {
		model, ok := s.findModelAsset(s.standalone.modelName)
		if !ok {
			s.standalone.drawerOpen = false
			return
		}
		t := tokens(c)
		ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
			keyValueRow(c, tr(c, "models.serve.drawer.model"), modelLabel(model), false)
			engineOptions := s.standaloneEngineOptions(model)
			if len(engineOptions) == 0 {
				noticeBanner(c, noticeBannerOptions{
					Kind:    stateWarning,
					Size:    bannerCompact,
					Message: tr(c, "models.serve.drawer.no_engine"),
				})
			} else {
				validSelected := false
				for _, opt := range engineOptions {
					if opt.Value == s.standalone.engineName {
						validSelected = true
						break
					}
				}
				if !validSelected {
					s.standalone.engineName = s.defaultStandaloneEngine(model, engineOptions)
				}
				selectField(c, tr(c, "models.serve.drawer.engine"), "", &s.standalone.engineName, engineOptions)
			}

			ctxOptions := standaloneContextOptions(model)
			ctxHint := ""
			if model.ContextLength != nil && *model.ContextLength > 0 {
				ctxHint = trf(c, "models.serve.drawer.context_limit", "limit", strconv.FormatInt(*model.ContextLength, 10))
			}
			selectField(c, tr(c, "models.serve.drawer.context"), ctxHint, &s.standalone.contextPreset, ctxOptions)
			if s.standalone.contextPreset == standaloneCustomContext {
				field(c, tr(c, "models.serve.drawer.context_custom_label"), &s.standalone.customContext)
			}

			ctxVal, ctxErr := s.resolvedStandaloneContext(model)
			if ctxErr != "" {
				noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: ctxErr})
			}
			memErr := s.standaloneMemoryShortfall(model)
			if memErr != "" {
				noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: memErr})
			}

			canStart := len(engineOptions) > 0 && s.standalone.engineName != "" && ctxErr == "" && memErr == "" && !s.busy && !s.standalone.preparing
			ui.Row(c).FillWidth().Justify(ui.End).Gap(t.Density.Row).Children(func() {
				outlineButton(c, tr(c, "models.serve.action.cancel")).OnClick(func() {
					s.standalone.drawerOpen = false
				})
				if primaryButton(c, tr(c, "models.serve.action.confirm_start")).Disabled(!canStart).Clicked() {
					s.launchStandalone(model.Name, s.standalone.engineName, ctxVal)
				}
			})
		})
	})
}

func (s *viewState) standaloneDialogs(c *ui.Context) {
	c.Key("standalone-switch-dialog")
	switchTitle := tr(c, "models.serve.switch.title")
	switchRes := components.AlertDialog(c, &s.standalone.switchConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: switchTitle},
		Title:         switchTitle,
		Message:       trf(c, "models.serve.switch.message", "model", s.standalone.switchModel),
		ConfirmLabel:  tr(c, "models.serve.action.switch_confirm"),
		CancelLabel:   tr(c, "models.serve.action.cancel"),
	})
	switch switchRes {
	case components.DialogConfirmed:
		targetName := s.standalone.switchModel
		prefill := s.standalone.switchPrefill
		s.standalone.switchConfirm = false
		s.standalone.switchModel = ""
		s.standalone.switchPrefill = false
		s.stopActiveServiceThen(func() {
			if target, ok := s.findModelAsset(targetName); ok {
				s.openStandaloneDrawer(target, prefill)
			}
		})
	case components.DialogCancelled:
		s.standalone.switchConfirm = false
		s.standalone.switchModel = ""
		s.standalone.switchPrefill = false
	}

	c.Key("standalone-benchmark-handoff-dialog")
	handoffTitle := tr(c, "models.serve.handoff.title")
	handoffRes := components.AlertDialog(c, &s.standalone.benchmarkConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: handoffTitle},
		Title:         handoffTitle,
		Message:       tr(c, "models.serve.handoff.message"),
		ConfirmLabel:  tr(c, "models.serve.handoff.confirm"),
		CancelLabel:   tr(c, "models.serve.action.cancel"),
	})
	switch handoffRes {
	case components.DialogConfirmed:
		next := s.standalone.benchmarkAction
		s.standalone.benchmarkConfirm = false
		s.standalone.benchmarkAction = nil
		s.stopActiveServiceThen(next)
	case components.DialogCancelled:
		s.standalone.benchmarkConfirm = false
		s.standalone.benchmarkAction = nil
	}
}
