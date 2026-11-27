package pages

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
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
	switchRes := studioAlertDialog(c, &s.standalone.switchConfirm, components.AlertDialogOptions{
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
	handoffRes := studioAlertDialog(c, &s.standalone.benchmarkConfirm, components.AlertDialogOptions{
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

func (s *viewState) renderModelStandaloneBadge(c *ui.Context, m core.ModelAsset) {
	if !m.Available {
		inlineBadge(c, tr(c, "models.badge.unavailable"), stateWarning, badgeOptions{Style: badgeDot, Size: sizeSM})
		return
	}
	rowState, _, _, _, _ := s.standaloneRowState(m)
	switch rowState {
	case "preparing", "starting", "running", "stopping":
		kind := stateActive
		if rowState == "running" {
			kind = stateSuccess
		}
		inlineBadge(c, tr(c, "models.serve.badge."+rowState), kind, badgeOptions{Style: badgeDot, Size: sizeSM})
	case "failed":
		inlineBadge(c, tr(c, "models.serve.badge.failed"), stateDanger, badgeOptions{Style: badgeDot, Size: sizeSM})
	}
}

func (s *viewState) renderModelStandaloneSubrow(c *ui.Context, m core.ModelAsset) {
	if !m.Available {
		return
	}
	t := tokens(c)
	rowState, svc, endpoint, logPath, canWebUI := s.standaloneRowState(m)
	switch rowState {
	case "preparing", "starting", "running", "stopping":
		var webURL string
		var webOK bool
		if canWebUI {
			webURL, webOK, _ = serviceWebUIInfo(svc)
		}
		if endpoint == "" && !webOK && logPath == "" {
			return
		}
		studioTooltipGroup(c, ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
			if endpoint != "" {
				ui.Text(c, endpoint).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
				outlineIconButton(c, lucide.Copy, tr(c, "engine.action.copy_endpoint")).OnClick(func() {
					s.copyServiceEndpoint(c, svc)
				})
			}
			if webOK {
				outlineIconButton(c, lucide.ExternalLink, tr(c, "engine.action.open_webui")).OnClick(func() {
					s.openEngineWebUI(c, webURL)
				})
			}
			if logPath != "" {
				outlineIconButton(c, lucide.FileText, tr(c, "engine.action.open_log")).OnClick(func() {
					s.openEngineCardLog(logPath)
				})
			}
		})
	case "failed":
		if logPath == "" {
			return
		}
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			outlineIconButton(c, lucide.FileText, tr(c, "engine.action.open_log")).OnClick(func() {
				s.openEngineCardLog(logPath)
			})
		})
	}
}

func (s *viewState) renderModelStandalonePrimaryAction(c *ui.Context, m core.ModelAsset) {
	if !m.Available {
		return
	}
	rowState, _, _, _, _ := s.standaloneRowState(m)
	switch rowState {
	case "preparing":
		return
	case "starting", "running", "stopping":
		stopLabel := tr(c, "models.serve.action.stop")
		var reason string
		if rowState == "stopping" {
			reason = tr(c, "models.serve.badge.stopping")
		} else if s.busy {
			reason = tr(c, "models.action.blocked.busy")
		}
		if modelRowButtonClicked(c, dangerIconButton, lucide.Square, stopLabel, m.Name, reason) {
			s.pendingDestructive = destructiveRequest{kind: string(destructiveStopStandalone), id: m.Name}
		}
		return
	}
	label := tr(c, "models.serve.action.start")
	render := primaryIconButton
	icon := lucide.Play
	retry := rowState == "failed"
	if retry {
		label = tr(c, "models.serve.action.retry")
		render = outlineIconButton
		icon = lucide.RotateCw
	}
	reason := s.standaloneStartBlockReason()
	if reason == "" && s.serveProbing {
		reason = tr(c, "models.serve.blocked.probing")
	}
	if reason == "" && len(s.standaloneEngineOptions(m)) == 0 && s.checks.Checking == 0 && !s.engineVerifyPending {
		reason = tr(c, "models.serve.drawer.no_engine")
	}
	if modelRowButtonClicked(c, render, icon, label, m.Name, reason) {
		s.requestStandaloneStart(m, retry)
	}
}

func (s *viewState) standaloneStopDialog(c *ui.Context) {
	targetModel := ""
	if destructiveKind(s.pendingDestructive.kind) == destructiveStopStandalone {
		targetModel = s.pendingDestructive.id
	}
	var targetID string
	displayModel := targetModel
	disabled := !s.idleForEngineStop()
	if targetModel != "" {
		if m, ok := s.findModelAsset(targetModel); ok {
			rowState, svc, _, _, _ := s.standaloneRowState(m)
			targetID = svc.ServiceID
			if title := s.serviceModelTitle(c, svc); title != "" {
				displayModel = title
			}
			if rowState != "starting" && rowState != "running" {
				disabled = true
			}
		} else {
			disabled = true
		}
	}
	if s.destructiveDialogModal(c, destructiveOption{
		kind:         destructiveStopStandalone,
		id:           targetModel,
		label:        tr(c, "services.card.stop_confirm_title"),
		confirmLabel: tr(c, "services.card.stop_confirm_action"),
		prompt:       trf(c, "services.card.stop_confirm_prompt", "model", displayModel),
		disabled:     disabled,
	}) {
		if targetID != "" {
			s.stopManagedServiceByID(targetID)
		} else {
			s.stopStandaloneService()
		}
	}
}
