package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type engineInfoRow struct {
	label, value string
	tabular      bool
}

func engineStateKind(state string) stateKind {
	switch state {
	case "running":
		return stateSuccess
	case "starting", "stopping":
		return stateActive
	case "failed":
		return stateDanger
	case "orphaned":
		return stateWarning
	default:
		return stateNeutral
	}
}

func validLoopbackURL(raw string) (string, bool) {
	if raw == "" || !isLoopbackURL(raw) {
		return "", false
	}
	return raw, true
}

func engineWebUIInfo(state core.EngineState) (url string, canOpen bool, disabledReason string) {
	u, ok := validLoopbackURL(state.Endpoint)
	if !ok {
		return "", false, ""
	}
	if state.WebUIDisabled {
		return u, false, l10n.TR("engine.webui.disabled")
	}
	if state.Family == "mlx-lm" {
		return u, false, l10n.TR("engine.webui.api_only")
	}
	if state.State != "running" {
		return u, false, ""
	}
	return u, true, ""
}

func engineFamilyTitle(family, adapter string) string {
	switch family {
	case "llama-cpp":
		return "llama.cpp"
	case "polystrata":
		return "PolyStrata"
	case "mlx-lm":
		return "mlx-lm"
	case "vllm":
		return "vLLM"
	case "sglang":
		return "SGLang"
	case "lmstudio":
		return "LM Studio"
	}
	switch adapter {
	case "prism_llama_server_v1", "llama_cpp_b11146_v1":
		return "llama.cpp"
	default:
		return adapter
	}
}

func formatContextTokens(n int64) string {
	if n <= 0 {
		return ""
	}
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dK", n/1024)
	}
	return groupedInt(n)
}

func (s *viewState) selectedEngineFamily() string {
	for _, e := range s.registry.Engines {
		if e.Name == s.engine {
			return e.Family
		}
	}
	return ""
}

func (s *viewState) engineCardSummary() []engineInfoRow {
	state := s.engineState
	family := state.Family
	adapter := state.Adapter
	execName := displayName(state.Executable)
	modelRaw := state.Model
	contextSize := state.ContextSize
	candidateRaw := state.Candidate

	if !engineLocked(state.State) && state.State != "orphaned" {
		pendingCandidate := s.managedEngineCandidate()
		if candidateRaw == "" {
			candidateRaw = pendingCandidate
		}
		useStartup := s.startup.Family != "" || s.startup.Adapter != "" || s.startup.Path != "" || s.startup.ModelPath != "" || s.startup.ContextSize > 0
		if useStartup && (s.candidate == "" || pendingCandidate == "" || s.candidate == pendingCandidate) {
			if family == "" {
				family = s.startup.Family
			}
			if adapter == "" {
				adapter = s.startup.Adapter
			}
			if execName == "" && s.startup.Path != "" {
				execName = displayName(s.startup.Path)
			}
			if modelRaw == "" {
				modelRaw = s.startup.ModelPath
			}
			if contextSize <= 0 {
				contextSize = s.startup.ContextSize
			}
		}
		if family == "" && s.engine != "" {
			family = s.selectedEngineFamily()
		}
		if modelRaw == "" && s.model != "" {
			modelRaw = s.model
		}
	}

	var rows []engineInfoRow
	familyTitle := engineFamilyTitle(family, adapter)
	var engineParts []string
	if familyTitle != "" {
		engineParts = append(engineParts, familyTitle)
	}
	if execName != "" && execName != familyTitle {
		engineParts = append(engineParts, execName)
	}
	if len(engineParts) == 0 && !engineLocked(state.State) && state.State != "orphaned" && s.engine != "" {
		engineParts = append(engineParts, s.engine)
	}
	if len(engineParts) > 0 {
		rows = append(rows, engineInfoRow{label: l10n.TR("engine.info.engine"), value: strings.Join(engineParts, " · ")})
	}
	if modelName := displayName(modelRaw); modelName != "" {
		rows = append(rows, engineInfoRow{label: l10n.TR("engine.info.model"), value: modelName})
	}
	ctxStr := formatContextTokens(contextSize)
	switch {
	case ctxStr != "" && state.Pid > 0:
		rows = append(rows, engineInfoRow{label: l10n.TR("engine.info.params"), value: l10n.TRf("engine.info.context_pid", "context", ctxStr, "pid", fmt.Sprintf("%d", state.Pid)), tabular: true})
	case ctxStr != "":
		rows = append(rows, engineInfoRow{label: l10n.TR("engine.info.context"), value: ctxStr, tabular: true})
	case state.Pid > 0:
		rows = append(rows, engineInfoRow{label: l10n.TR("engine.info.process"), value: fmt.Sprintf("PID %d", state.Pid), tabular: true})
	}
	if candidateRaw != "" {
		rows = append(rows, engineInfoRow{label: l10n.TR("engine.info.config"), value: displayName(candidateRaw)})
	}
	return rows
}

func (s *viewState) engineStatusPanel(c *ui.Context, runActive bool, close func()) {
	t := tokens(c)
	theme := c.Theme()
	state := s.engineState
	label := state.State
	if label == "" {
		label = "stopped"
	}
	kind := engineStateKind(label)
	rows := s.engineCardSummary()

	headerOpts := engineHeaderOptions{
		statusText: stateLabel(label),
		kind:       kind,
	}
	switch state.State {
	case "starting", "running", "stopping":
		stopDisabled := !s.stopEnabled(true, runActive)
		stopTip := tr(c, "engine.tooltip.click_stop")
		if stopDisabled {
			if runActive {
				stopTip = tr(c, "engine.tooltip.stop_busy_run")
			} else {
				stopTip = tr(c, "engine.tooltip.stopping")
			}
		}
		headerOpts.action = &engineHeaderActionOptions{
			statusText:  stateLabel(label),
			actionLabel: tr(c, "engine.action.stop"),
			kind:        kind,
			action:      "stop",
			busy:        state.State == "stopping",
			disabled:    stopDisabled,
			tooltip:     stopTip,
		}
		headerOpts.onClick = func() {
			s.pendingDestructive = s.destructiveRequestFor(destructiveOption{kind: destructiveStopEngine, id: "header"})
			close()
		}
	case "orphaned":
	default:
		startDisabled := !s.canStartManagedEngine()
		startTip := tr(c, "engine.tooltip.click_start")
		if s.managedEngineCandidate() == "" {
			startTip = tr(c, "engine.tooltip.need_candidate")
		} else if startDisabled {
			startTip = tr(c, "engine.tooltip.cannot_start")
		}
		headerOpts.action = &engineHeaderActionOptions{
			statusText:  stateLabel(label),
			actionLabel: tr(c, "engine.action.start"),
			kind:        kind,
			action:      "start",
			busy:        s.actionBusy("start-engine"),
			disabled:    startDisabled,
			tooltip:     startTip,
		}
		headerOpts.onClick = s.startManagedEngine
	}

	ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(func() {
		engineCardHeader(c, headerOpts)
		if len(rows) > 0 {
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Density.Stack), materialSunken).Children(func() {
				for _, row := range rows {
					engineInfoKVRow(c, row.label, row.value, row.tabular)
				}
			})
		}
		s.engineEndpointBar(c, state)
		if state.State == "starting" || state.State == "stopping" {
			progressBar(c, -1, tr(c, "engine.status.transitioning"))
		}
		switch state.State {
		case "starting", "running", "stopping":
			if runActive {
				ui.Text(c, tr(c, "engine.notice.stop_after_run")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
			}
		case "orphaned":
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateWarning,
				Size:    bannerCompact,
				Message: tr(c, "engine.notice.orphan_unconfirmed"),
			})
			button(c, tr(c, "engine.action.go_dashboard")).FillWidth().Padding(theme.Space(1.75), theme.Space(3.5)).Radius(theme.Space(2.5)).OnClick(func() {
				s.navigateTo("services", "")
				close()
			})
		default:
			errorView(c, state.Error)
			if state.State == "failed" && state.LogPath != "" {
				outlineButton(c, tr(c, "engine.action.open_log")).FillWidth().Padding(theme.Space(1.75), theme.Space(3.5)).Radius(theme.Space(2.5)).OnClick(func() {
					s.openEngineCardLog(state.LogPath)
				})
			}
			if s.managedEngineCandidate() == "" {
				ui.Text(c, tr(c, "engine.notice.need_candidate")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				button(c, tr(c, "engine.action.go_dashboard")).FillWidth().Padding(theme.Space(1.75), theme.Space(3.5)).Radius(theme.Space(2.5)).OnClick(func() {
					s.openBenchmarksFlow()
					close()
				})
			}
		}
	})
}

type engineHeaderOptions struct {
	statusText string
	kind       stateKind
	action     *engineHeaderActionOptions
	onClick    func()
}

func engineCardHeader(c *ui.Context, opts engineHeaderOptions) {
	t := tokens(c)
	scale := stateScale(c, opts.kind)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		tile := ui.Box(c).Size(30, 30).Radius(t.Radius.Control).
			AlignItems(ui.Center).Justify(ui.Center).Background(scale.SubtleBg)
		if c.Preferences().HighContrast {
			tile.Border(1, scale.Border)
		} else {
			tile.Border(1, scale.Solid.Alpha(.16))
		}
		tile.Children(func() {
			components.Icon(c, lucide.Server).Size(15, 15).TextColor(scale.Fg)
		})
		heading(c, tr(c, "engine.card.title"), 3).Grow(1).MinWidth(0)
		if opts.action != nil {
			if engineHeaderActionButton(c, *opts.action).Clicked() && opts.onClick != nil {
				opts.onClick()
			}
		} else {
			engineStatusBadge(c, opts.statusText, opts.kind)
		}
	})
}

func engineStatusBadge(c *ui.Context, text string, kind stateKind) {
	t := tokens(c)
	scale := stateScale(c, kind)
	pill := ui.Row(c).AlignItems(ui.Center).Justify(ui.Center).Gap(t.Density.Tight+2).
		Padding(3, 9).Radius(t.Radius.Full).Background(scale.SubtleBg).Label(text)
	if c.Preferences().HighContrast {
		pill.Border(1, scale.Border)
	} else {
		pill.Border(1, scale.Solid.Alpha(.18))
	}
	pill.Children(func() {
		ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Background(scale.Solid)
		ui.Text(c, text).FontSize(t.Type.Caption).TextColor(scale.Fg).SingleLine()
	})
}

func engineInfoKVRow(c *ui.Context, key, value string, tabular bool) {
	t := tokens(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Box(c).Width(40).Shrink(0).Children(func() {
			ui.Text(c, key).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
		})
		val := ui.Text(c, value).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).
			Grow(1).MinWidth(0).SingleLine()
		if tabular {
			val.FontFeatures("tnum")
		}
	})
}

func (s *viewState) engineEndpointBar(c *ui.Context, state core.EngineState) {
	if state.Endpoint == "" {
		return
	}
	t := tokens(c)
	theme := c.Theme()
	uiURL, canOpen, disabledReason := engineWebUIInfo(state)
	validURL, hasLoopback := validLoopbackURL(state.Endpoint)
	bar := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).
		Padding(theme.Space(1), theme.Space(1.5)).Radius(t.Radius.Inset).
		Background(t.Color.Surface).Border(1, t.Color.BorderDefault)
	bar.Children(func() {
		if canOpen {
			linkBtn := ui.ButtonBase(c).Grow(1).MinWidth(0).
				Padding(theme.Space(1.25), theme.Space(1.5)).Radius(t.Radius.Control).
				Label(tr(c, "engine.action.open_webui"))
			var bg ui.Color
			switch {
			case linkBtn.Pressed():
				bg = t.Color.SurfacePressed
			case linkBtn.Hovered():
				bg = t.Color.Info.SubtleBg
			}
			if bg.A > 0 {
				linkBtn.Background(bg)
			}
			linkBtn.Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight + 2).Children(func() {
					components.Icon(c, lucide.Globe).Size(14, 14).TextColor(t.Color.Info.Fg)
					ui.Text(c, uiURL).FontSize(t.Type.Caption).FontFeatures("tnum").
						TextColor(t.Color.Info.Fg).Grow(1).MinWidth(0).SingleLine()
					ui.Row(c).AlignItems(ui.Center).Gap(3).
						Padding(2, 7).Radius(t.Radius.XS).Background(t.Color.Info.SubtleBg).Shrink(0).Children(func() {
						ui.Text(c, tr(c, "engine.action.open_webui")).FontSize(t.Type.ShellLabel).Bold().
							TextColor(t.Color.Info.Fg).SingleLine()
						components.Icon(c, lucide.ArrowUpRight).Size(12, 12).TextColor(t.Color.Info.Fg)
					})
				})
			})
			components.Tooltip(c, linkBtn, tr(c, "engine.tooltip.open_webui"))
			if linkBtn.Clicked() {
				s.openEngineWebUI(c, uiURL)
			}
		} else {
			ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(t.Density.Tight+2).
				Padding(theme.Space(1.25), theme.Space(1.5)).Children(func() {
				components.Icon(c, lucide.Globe).Size(14, 14).TextColor(t.Color.FgTertiary)
				ui.Text(c, loopbackEndpoint(state.Endpoint)).FontSize(t.Type.Caption).
					FontFeatures("tnum").TextColor(t.Color.FgSecondary).Grow(1).MinWidth(0).SingleLine()
				if disabledReason != "" {
					ui.Text(c, disabledReason).FontSize(t.Type.ShellLabel).
						TextColor(t.Color.FgTertiary).SingleLine().Shrink(0)
				}
			})
		}
		if hasLoopback {
			ui.Box(c).Width(1).Height(16).Background(t.Color.BorderSubtle)
			if iconButton(c, lucide.Copy, tr(c, "engine.action.copy_endpoint"), components.IconButtonCompact).Clicked() {
				s.copyEngineEndpoint(c, validURL)
			}
		}
	})
}
