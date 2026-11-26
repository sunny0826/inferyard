package pages

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
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
			s.verifyTaskEngines()
			engineOptions := s.standaloneEngineOptions(model)
			if len(engineOptions) == 0 {
				if s.checks.Checking > 0 {
					ui.Text(c, trf(c, "dashboard.candidate.verifying_engines", "count", s.checks.Checking)).TextColor(c.Theme().TextMuted)
				} else if !s.engineVerifyPending {
					noticeBanner(c, noticeBannerOptions{
						Kind:    stateWarning,
						Size:    bannerCompact,
						Message: tr(c, "models.serve.drawer.no_engine"),
					})
				}
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
			budgetCtx := ctxVal
			if budgetCtx <= 0 {
				budgetCtx = 8192
			}
			budget := s.checkMemoryBudget(model.Name, s.standalone.engineName, budgetCtx, nil)
			if budget.Known {
				s.serviceMemoryBudgetBar(c, budget)
			}
			memErr := ""
			if !budget.Known {
				memErr = s.standaloneMemoryShortfall(model)
				if memErr != "" {
					noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: memErr})
				}
			}

			memEnough := !budget.Known || budget.Enough
			canStart := len(engineOptions) > 0 && s.standalone.engineName != "" && ctxErr == "" && memEnough && memErr == "" && !s.busy && !s.standalone.isPreparing(model.Name)
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
		action := s.standalone.switchAction
		s.standalone.switchConfirm = false
		s.standalone.switchModel = ""
		s.standalone.switchPrefill = false
		s.standalone.switchAction = nil
		if action != nil {
			s.stopActiveServiceThen(action)
		} else {
			s.stopActiveServiceThen(func() {
				if target, ok := s.findModelAsset(targetName); ok {
					s.openStandaloneDrawer(target, prefill)
				}
			})
		}
	case components.DialogCancelled:
		s.standalone.switchConfirm = false
		s.standalone.switchModel = ""
		s.standalone.switchPrefill = false
		s.standalone.switchAction = nil
	}

	c.Key("standalone-benchmark-handoff-dialog")
	activeCount := len(s.activeStandaloneServices())
	titleKey, msgKey, confirmKey := standaloneHandoffCopyKeys(activeCount)
	handoffTitle := tr(c, titleKey)
	handoffMsg := tr(c, msgKey)
	if activeCount > 1 {
		handoffMsg = trf(c, msgKey, "count", strconv.Itoa(activeCount))
	}
	handoffRes := components.AlertDialog(c, &s.standalone.benchmarkConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: handoffTitle},
		Title:         handoffTitle,
		Message:       handoffMsg,
		ConfirmLabel:  tr(c, confirmKey),
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

func standaloneHandoffCopyKeys(activeCount int) (titleKey, msgKey, confirmKey string) {
	if activeCount > 1 {
		return "models.serve.handoff.multi_title", "models.serve.handoff.multi_message", "models.serve.handoff.multi_confirm"
	}
	return "models.serve.handoff.title", "models.serve.handoff.message", "models.serve.handoff.confirm"
}

func (s *viewState) modelStandaloneActions(c *ui.Context, m core.ModelAsset) {
	if !m.Available {
		return
	}
	rowState, svc, endpoint, logPath, canWebUI := s.standaloneRowState(m)
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
				s.copyServiceEndpoint(c, svc)
			})
		}
		if canWebUI {
			if webURL, ok, _ := serviceWebUIInfo(svc); ok {
				outlineButton(c, tr(c, "engine.action.open_webui")).OnClick(func() {
					s.openEngineWebUI(c, webURL)
				})
			}
		}
		if logPath != "" {
			outlineButton(c, tr(c, "engine.action.open_log")).OnClick(func() {
				s.openEngineCardLog(logPath)
			})
		}
		if rowState != "preparing" {
			targetID := svc.ServiceID
			dangerButton(c, tr(c, "models.serve.action.stop")).Disabled(rowState == "stopping" || s.busy).OnClick(func() {
				if targetID != "" {
					s.stopManagedServiceByID(targetID)
				} else {
					s.stopStandaloneService()
				}
			})
		}
	case "failed":
		badge(c, tr(c, "models.serve.badge.failed"), stateDanger, badgeOptions{Style: badgeDot, Size: sizeSM})
		if logPath != "" {
			outlineButton(c, tr(c, "engine.action.open_log")).OnClick(func() {
				s.openEngineCardLog(logPath)
			})
		}
		retryDisabled := s.standaloneStartBlockReason() != "" || s.serveProbing || s.busy || s.standalone.isPreparing(m.Name)
		button(c, tr(c, "models.serve.action.retry")).Disabled(retryDisabled).OnClick(func() {
			s.requestStandaloneStart(m, true)
		})
	default:
		label := tr(c, "models.serve.action.start")
		reason := s.standaloneStartBlockReason()
		disabled := reason != "" || s.serveProbing || s.busy || s.standalone.isPreparing(m.Name)
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
