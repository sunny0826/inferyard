package pages

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) hasActiveExitNotice() bool {
	return s.exitedNotice != nil && !s.exitedNotice.Dismissed
}

func (s *viewState) renderExitedServiceCard(c *ui.Context) {
	if !s.hasActiveExitNotice() {
		return
	}
	notice := *s.exitedNotice
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

	cardBox := ui.Column(c.Key("service-exited-notice-card")).FillWidth().
		Padding(pad).Radius(outerR).Gap(t.Density.Row).
		Background(scale.SubtleBg).Border(1, scale.Border)
	cardBox.Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			components.Icon(c, lucide.CircleAlert).Size(18, 18).TextColor(scale.Fg)
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
				components.Icon(c, lucide.Info).Size(14, 14).TextColor(scale.Fg)
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

		canLaunch := !s.busy && !s.standalone.preparing && (s.serveSupported || s.preview != nil)
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if inlineButton(c, components.ButtonPrimary, tr(c, "services.exited.action.restart_same")).
				Disabled(!canLaunch || notice.Model == "" || notice.Engine == "").Clicked() {
				s.restartFromExitNotice(notice)
			}
			if inlineButton(c, components.ButtonOutline, tr(c, "services.exited.action.adjust_params")).
				Disabled(!canLaunch).Clicked() {
				s.openServiceParamsDrawerFromExitNotice(notice)
			}
			if inlineButton(c, components.ButtonOutline, tr(c, "services.exited.action.view_log")).
				Disabled(strings.TrimSpace(notice.LogPath) == "").Clicked() {
				s.openEngineCardLog(notice.LogPath)
			}
			ui.Box(c).Grow(1)
			if inlineButton(c, components.ButtonGhost, tr(c, "services.exited.action.dismiss")).Clicked() {
				s.dismissExitedServiceNotice()
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
		GPULayers:   cloneIntPtrUI(notice.GPULayers),
		Threads:     cloneIntPtrUI(notice.Threads),
		ExtraArgs:   append([]string(nil), notice.ExtraArgs...),
	}
	s.requestStandaloneLaunchWithConfirm(req, false)
}

func (s *viewState) dismissExitedServiceNotice() {
	s.exitedNotice = nil
	if s.simulate(previewDismissExitNotice{}) {
		return
	}
	if s.service == nil {
		return
	}
	go func() {
		_ = s.service.DismissExitedServiceNotice()
	}()
}

func (s *viewState) requestStandaloneLaunchWithConfirm(req core.StandaloneStartRequest, directRestart bool) {
	if !directRestart && s.managedServiceActive() {
		s.serviceParams.open = false
		s.standalone.switchConfirm = true
		s.standalone.switchModel = req.Model
		s.standalone.switchPrefill = false
		s.standalone.switchAction = func() {
			s.launchStandaloneRequest(req)
		}
		return
	}
	s.startStandaloneWithStopIfNeeded(req)
}

func (s *viewState) startStandaloneWithStopIfNeeded(req core.StandaloneStartRequest) {
	if s.managedServiceActive() {
		s.stopActiveServiceThen(func() {
			s.launchStandaloneRequest(req)
		})
		return
	}
	s.launchStandaloneRequest(req)
}

func (s *viewState) launchStandaloneRequest(req core.StandaloneStartRequest) {
	s.standalone.drawerOpen = false
	s.serviceParams.open = false
	s.standalone.preparing = true
	s.standalone.preparingModel = req.Model
	s.standalone.lastModel = req.Model
	if s.standalone.lastParams == nil {
		s.standalone.lastParams = map[string]standaloneAttempt{}
	}
	s.standalone.lastParams[req.Model] = standaloneAttempt{engine: req.Engine, context: req.ContextSize}
	s.standalone.lastError = nil
	if s.simulate(previewStandaloneStart{
		model:       req.Model,
		engine:      req.Engine,
		contextSize: req.ContextSize,
		port:        req.Port,
		gpuLayers:   cloneIntPtrUI(req.GPULayers),
		threads:     cloneIntPtrUI(req.Threads),
		extraArgs:   append([]string(nil), req.ExtraArgs...),
	}) {
		return
	}
	if s.service == nil {
		s.standalone.preparing = false
		return
	}
	s.enqueueStatusToast(l10n.TR("models.serve.toast.started"))
	go func() {
		state, apiErr := s.service.StartStandalone(s.ctx, req)
		if s.ctx != nil && s.ctx.Err() != nil {
			return
		}
		presets := s.service.RecentServices()
		notice := s.service.ExitedServiceNotice()
		s.runOnUI(func() {
			s.standalone.preparing = false
			s.standalone.preparingModel = ""
			s.recentServices = presets
			s.exitedNotice = notice
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

func (s *viewState) recentServicesSection(c *ui.Context) {
	t := tokens(c)
	canLaunch := !s.busy && !s.standalone.preparing && (s.serveSupported || s.preview != nil)
	for _, preset := range s.recentServices {
		running := s.isPresetRunning(preset)
		modelAsset, foundModel := s.findModelAsset(preset.Model)
		modelAvailable := foundModel && modelAsset.Available
		cardEl, innerR := compactCard(c, ui.Column(c.Key("recent-svc-"+preset.Model+"-"+preset.Engine)).FillWidth().Gap(t.Density.Tight), t.Density.Stack)
		cardEl.Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				modelIconBox(c, preset.Model, 30, innerR)
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						ui.Text(c, preset.Model).Bold().FontSize(t.Type.Section).SingleLine()
						enginePill(c, preset.Engine)
						if running {
							inlineBadge(c, tr(c, "services.recent.running_badge"), stateSuccess, badgeOptions{Size: sizeSM, Style: badgeDot})
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
				})
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					if inlineButton(c, components.ButtonOutline, tr(c, "services.recent.action.params")).
						Label(tr(c, "services.recent.action.params") + " · " + preset.Model).Clicked() {
						if running {
							s.openServiceParamsDrawerForRunning()
						} else {
							s.openServiceParamsDrawerForPreset(preset)
						}
					}
					if inlineButton(c, components.ButtonPrimary, tr(c, "services.recent.action.start")).
						Label(tr(c, "services.recent.action.start") + " · " + preset.Model).
						Disabled(running || !canLaunch || !modelAvailable).Clicked() {
						s.startFromPreset(preset)
					}
				})
			})
		})
	}
}

func (s *viewState) isPresetRunning(preset core.ServicePreset) bool {
	st := s.engineState
	if st.State != "running" && st.State != "starting" && st.State != "stopping" {
		return false
	}
	modelMatch := (st.ModelName != "" && st.ModelName == preset.Model) || displayName(st.Model) == preset.Model
	engineMatch := st.EngineName == "" || st.EngineName == preset.Engine
	return modelMatch && engineMatch
}

func (s *viewState) startFromPreset(preset core.ServicePreset) {
	ctxSize := preset.ContextSize
	if ctxSize <= 0 {
		ctxSize = 8192
	}
	s.requestStandaloneLaunchWithConfirm(core.StandaloneStartRequest{
		Model:       preset.Model,
		Engine:      preset.Engine,
		ContextSize: ctxSize,
		Port:        preset.Port,
		GPULayers:   cloneIntPtrUI(preset.GPULayers),
		Threads:     cloneIntPtrUI(preset.Threads),
		ExtraArgs:   append([]string(nil), preset.ExtraArgs...),
	}, false)
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
