package pages

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) hasActiveExitNotice() bool {
	return len(s.effectiveExitedNotices()) > 0
}

func (s *viewState) renderExitedServiceCard(c *ui.Context) {
	notices := s.effectiveExitedNotices()
	if len(notices) == 0 {
		return
	}
	multi := len(notices) > 1
	for idx, notice := range notices {
		s.renderSingleExitedNoticeCard(c, notice, idx, multi)
	}
}

func (s *viewState) renderSingleExitedNoticeCard(c *ui.Context, notice core.ServiceExitNotice, idx int, multi bool) {
	t := tokens(c)
	scale := stateScale(c, stateDanger)
	outerR := t.Radius.Surface
	pad := t.Card.Padding
	innerR := concentricRadius(outerR, pad)

	modelName := strings.TrimSpace(notice.Model)
	if modelName == "" {
		modelName = tr(c, "engine.card.title")
	}
	engineName := strings.TrimSpace(notice.Engine)
	cardKey := notice.ID
	if cardKey == "" {
		cardKey = fmt.Sprintf("idx-%d", idx)
	}

	cardBox := ui.Column(c.Key("service-exited-notice-card-"+cardKey)).FillWidth().
		Padding(pad).Radius(outerR).Gap(t.Density.Row).
		Background(scale.SubtleBg).Border(1, scale.Border)
	cardBox.Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			components.Icon(c, reicon.CircleAlert).Size(18, 18).TextColor(scale.Fg)
			ui.Text(c, tr(c, "services.exited.title")).Bold().FontSize(t.Type.CardTitle).TextColor(scale.Fg).SingleLine()
			if notice.WhileAway {
				inlineBadge(c, tr(c, "services.exited.while_away_badge"), stateDanger, badgeOptions{Size: sizeSM})
			}
			ui.Box(c).Grow(1)
			if notice.ExitCode != nil {
				inlineBadge(c, trf(c, "services.exited.exit_code", "code", fmt.Sprintf("%d", *notice.ExitCode)), stateDanger, badgeOptions{Size: sizeSM, Style: badgeOutline})
			}
		})

		metaParts := []string{modelName}
		if engineName != "" {
			metaParts = append(metaParts, engineName)
		}
		if notice.Port > 0 {
			metaParts = append(metaParts, trf(c, "services.recent.port_fixed", "port", fmt.Sprintf("%d", notice.Port)))
		}
		if notice.ExitedAt != "" {
			metaParts = append(metaParts, trf(c, "services.exited.exited_at", "time", formatTimestampShort(notice.ExitedAt)))
		} else if notice.DetectedAt != "" {
			metaParts = append(metaParts, trf(c, "services.exited.detected_at", "time", formatTimestampShort(notice.DetectedAt)))
		}
		ui.Text(c, joinParts(metaParts)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgPrimary)

		if notice.WhileAway {
			ui.Text(c, tr(c, "services.exited.while_away_desc")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
		if hintText := serviceExitHintText(c, notice.Hint); hintText != "" {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).
				Padding(t.Density.Tight, t.Density.Row).Radius(innerR).
				Background(t.Color.Surface).Border(1, scale.Border).Children(func() {
				components.Icon(c, reicon.Info).Size(14, 14).TextColor(scale.Fg)
				ui.Text(c, hintText).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Grow(1)
			})
		}
		if len(notice.LogTail) > 0 {
			ui.Column(c).FillWidth().Gap(t.Density.Hairline).
				Padding(t.Density.Row).Radius(innerR).
				Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Children(func() {
				ui.Text(c, tr(c, "services.exited.log_tail")).FontSize(t.Type.ShellLabel).TextColor(t.Color.FgSecondary)
				ui.Text(c, strings.Join(notice.LogTail, "\n")).
					FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Selectable()
			})
		}

		canLaunch := !s.busy && !s.standalone.isPreparing(notice.Model) && (s.serveSupported || s.preview != nil)
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			restartLabel := tr(c, "services.exited.action.restart_same")
			restartBtn := inlineButton(c, components.ButtonPrimary, restartLabel).
				Disabled(!canLaunch || notice.Model == "" || notice.Engine == "")
			if multi {
				restartBtn.Label(restartLabel + " · " + modelName)
			}
			if restartBtn.Clicked() {
				s.restartFromExitNotice(notice)
			}
			adjustLabel := tr(c, "services.exited.action.adjust_params")
			adjustBtn := inlineButton(c, components.ButtonOutline, adjustLabel).Disabled(!canLaunch)
			if multi {
				adjustBtn.Label(adjustLabel + " · " + modelName)
			}
			if adjustBtn.Clicked() {
				s.openServiceParamsDrawerFromExitNotice(notice)
			}
			logLabel := tr(c, "services.exited.action.view_log")
			logBtn := inlineButton(c, components.ButtonOutline, logLabel).Disabled(strings.TrimSpace(notice.LogPath) == "")
			if multi {
				logBtn.Label(logLabel + " · " + modelName)
			}
			if logBtn.Clicked() {
				s.openEngineCardLog(notice.LogPath)
			}
			ui.Box(c).Grow(1)
			dismissLabel := tr(c, "services.exited.action.dismiss")
			dismissBtn := inlineButton(c, components.ButtonGhost, dismissLabel)
			if multi {
				dismissBtn.Label(dismissLabel + " · " + modelName)
			}
			if dismissBtn.Clicked() {
				s.dismissExitedServiceNoticeByID(notice.ID)
			}
		})
	})
}

func serviceExitHintText(c *ui.Context, hint string) string {
	trimmed := strings.TrimSpace(hint)
	switch trimmed {
	case "":
		return ""
	case core.ExitHintOOM:
		return tr(c, "core.service.exit_hint.oom")
	default:
		return trimmed
	}
}

func (s *viewState) restartFromExitNotice(notice core.ServiceExitNotice) {
	ctxSize := notice.ContextSize
	if ctxSize <= 0 {
		ctxSize = 8192
	}
	req := core.StandaloneStartRequest{
		Model:       notice.Model,
		Engine:      notice.Engine,
		ContextSize: ctxSize,
		Port:        notice.RequestedPort,
		LANAccess:   notice.LANAccess,
		GPULayers:   cloneIntPtrUI(notice.GPULayers),
		Threads:     cloneIntPtrUI(notice.Threads),
		ExtraArgs:   append([]string(nil), notice.ExtraArgs...),
	}
	s.launchOrRestartService(req, "")
}

func (s *viewState) dismissExitedServiceNoticeByID(id string) {
	if id != "" {
		if s.dismissingNoticeIDs == nil {
			s.dismissingNoticeIDs = map[string]bool{}
		}
		s.dismissingNoticeIDs[id] = true
	} else {
		s.dismissingAllNotices = true
	}
	s.removeExitedNotice(id)
	s.bumpServiceSnapshotSeq()
	if s.simulate(previewDismissExitNotice{id: id}) {
		delete(s.dismissingNoticeIDs, id)
		if id == "" {
			s.dismissingAllNotices = false
		}
		return
	}
	if s.service == nil {
		delete(s.dismissingNoticeIDs, id)
		if id == "" {
			s.dismissingAllNotices = false
		}
		return
	}
	go func() {
		var dismissErr error
		if id != "" {
			dismissErr = s.service.DismissExitedServiceNoticeByID(id)
		} else {
			dismissErr = s.service.DismissExitedServiceNotice()
		}
		var restored []core.ServiceExitNotice
		if dismissErr != nil {
			restored = s.service.ExitedServiceNotices()
		}
		s.runOnUI(func() {
			if id != "" {
				delete(s.dismissingNoticeIDs, id)
			} else {
				s.dismissingAllNotices = false
			}
			if dismissErr != nil {
				s.syncExitedNotices(restored)
				var apiErr *core.ApiError
				if !errors.As(dismissErr, &apiErr) || apiErr == nil {
					apiErr = core.Err("internal_error")
				}
				s.failure = apiErr
				s.failurePage = s.selected
				return
			}
			s.bumpServiceSnapshotSeq()
		})
	}()
}

func (s *viewState) markStandalonePreparing(req core.StandaloneStartRequest) {
	s.standalone.drawerOpen = false
	s.serviceParams.open = false
	s.standalone.setPreparing(req.Model, true)
	s.standalone.lastModel = req.Model
	if s.standalone.lastParams == nil {
		s.standalone.lastParams = map[string]standaloneAttempt{}
	}
	s.standalone.lastParams[req.Model] = standaloneAttempt{engine: req.Engine, context: req.ContextSize}
	s.standalone.setLastError(req.Model, nil)
}

func (s *viewState) launchOrRestartService(req core.StandaloneStartRequest, restartServiceID string) {
	if restartServiceID != "" {
		s.markStandalonePreparing(req)
		s.stopManagedServiceThen(restartServiceID, func() {
			s.launchStandaloneRequest(req)
		})
		return
	}
	if existing, ok := s.findRunningServiceByModelEngine(req.Model, req.Engine); ok {
		s.markStandalonePreparing(req)
		s.stopManagedServiceThen(existing.ServiceID, func() {
			s.launchStandaloneRequest(req)
		})
		return
	}
	s.launchStandaloneRequest(req)
}

func (s *viewState) launchStandaloneRequest(req core.StandaloneStartRequest) {
	s.markStandalonePreparing(req)
	if s.simulate(previewStandaloneStart{
		model:       req.Model,
		engine:      req.Engine,
		contextSize: req.ContextSize,
		port:        req.Port,
		lanAccess:   req.LANAccess,
		gpuLayers:   cloneIntPtrUI(req.GPULayers),
		threads:     cloneIntPtrUI(req.Threads),
		extraArgs:   append([]string(nil), req.ExtraArgs...),
	}) {
		return
	}
	if s.service == nil {
		s.standalone.setPreparing(req.Model, false)
		return
	}
	go func() {
		state, apiErr := s.service.StartStandalone(s.ctx, req)
		if s.ctx != nil && s.ctx.Err() != nil {
			return
		}
		snap := s.captureServiceStateSnapshot()
		s.runOnUI(func() {
			s.standalone.setPreparing(req.Model, false)
			s.applyServiceStateSnapshot(snap)
			if apiErr != nil {
				s.standalone.setLastError(req.Model, apiErr)
				s.failure = apiErr
				s.failurePage = s.selected
				return
			}
			s.standalone.setLastError(req.Model, nil)
			s.markServiceExpanded(serviceKey(state))
			s.enqueueStatusToast(l10n.TR("models.serve.toast.started"))
		})
	}()
}

func cloneIntPtrUI(v *int) *int {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func formatTimestampShort(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) >= 19 && strings.Contains(trimmed, "T") {
		return strings.ReplaceAll(trimmed[:19], "T", " ")
	}
	return trimmed
}

func (s *viewState) hasVisibleRecentServices() bool {
	for _, preset := range s.recentServices {
		if !s.isPresetRunning(preset) {
			return true
		}
	}
	return false
}

func (s *viewState) recentServicesSection(c *ui.Context) {
	t := tokens(c)
	canLaunchBase := !s.busy && (s.serveSupported || s.preview != nil)
	for _, preset := range s.recentServices {
		if s.isPresetRunning(preset) {
			continue
		}
		canLaunch := canLaunchBase && !s.standalone.isPreparing(preset.Model)
		modelAsset, foundModel := s.findModelAsset(preset.Model)
		modelAvailable := foundModel && modelAsset.Available
		ctxSize := preset.ContextSize
		if ctxSize <= 0 {
			ctxSize = 8192
		}
		budget := s.checkMemoryBudget(preset.Model, preset.Engine, ctxSize, preset.GPULayers)
		memBlocked := budget.Known && !budget.Enough
		cardEl, innerR := compactCard(c, ui.Column(c.Key("recent-svc-"+preset.Model+"-"+preset.Engine)).FillWidth().Gap(t.Density.Tight), t.Density.Stack)
		cardEl.Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				modelIconBox(c, s.modelIconHint(preset.Model), 30, innerR)
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						ui.Text(c, preset.Model).Bold().FontSize(t.Type.Section).SingleLine()
						s.enginePill(c, preset.Engine)
						if preset.LANAccess {
							inlineBadge(c, tr(c, "services.card.badge.lan"), stateWarning, badgeOptions{Size: sizeSM})
						}
					})
					portLabel := tr(c, "services.recent.port_auto")
					if preset.Port > 0 {
						portLabel = trf(c, "services.recent.port_fixed", "port", fmt.Sprintf("%d", preset.Port))
					}
					parts := []string{portLabel}
					if ctx := formatContextTokens(preset.ContextSize); ctx != "" {
						parts = append(parts, ctx)
					}
					ui.Text(c, joinParts(parts)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
					if memBlocked {
						ui.Text(c, trf(c, "services.recent.memory_insufficient",
							"est", fmt.Sprintf("%.1f", budget.EstimatedGB),
							"remaining", fmt.Sprintf("%.1f", budget.RemainingGB),
						)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.Danger.Fg)
					}
				})
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					if inlineButton(c, components.ButtonOutline, tr(c, "services.recent.action.params")).
						Label(tr(c, "services.recent.action.params") + " · " + preset.Model).Clicked() {
						s.openServiceParamsDrawerForPreset(preset)
					}
					if inlineButton(c, components.ButtonPrimary, tr(c, "services.recent.action.start")).
						Label(tr(c, "services.recent.action.start") + " · " + preset.Model).
						Disabled(!canLaunch || !modelAvailable || memBlocked).Clicked() {
						s.startFromPreset(preset)
					}
				})
			})
		})
	}
}

func (s *viewState) isPresetRunning(preset core.ServicePreset) bool {
	_, ok := s.findRunningServiceByModelEngine(preset.Model, preset.Engine)
	return ok
}

func (s *viewState) startFromPreset(preset core.ServicePreset) {
	ctxSize := preset.ContextSize
	if ctxSize <= 0 {
		ctxSize = 8192
	}
	s.launchOrRestartService(core.StandaloneStartRequest{
		Model:       preset.Model,
		Engine:      preset.Engine,
		ContextSize: ctxSize,
		Port:        preset.Port,
		LANAccess:   preset.LANAccess,
		GPULayers:   cloneIntPtrUI(preset.GPULayers),
		Threads:     cloneIntPtrUI(preset.Threads),
		ExtraArgs:   append([]string(nil), preset.ExtraArgs...),
	}, "")
}

func splitExtraArgs(raw string) ([]string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	var out []string
	var cur strings.Builder
	var quote rune
	inToken := false
	escaped := false
	for _, r := range raw {
		if escaped {
			cur.WriteRune(r)
			inToken = true
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			inToken = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			inToken = true
		case ' ', '\t', '\n', '\r':
			if inToken {
				out = append(out, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteRune(r)
			inToken = true
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	if inToken {
		out = append(out, cur.String())
	}
	return out, true
}

func joinExtraArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\"'") {
			parts = append(parts, strconv.Quote(arg))
		} else {
			parts = append(parts, arg)
		}
	}
	return strings.Join(parts, " ")
}
