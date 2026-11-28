package pages

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func (s *viewState) modelsView(c *ui.Context) {
	t := tokens(c)
	host := ui.Column(c.Key("models-page-host")).FillWidth().Gap(t.Density.Section)
	pending := ui.Local(host, "pending-delete", func() pendingAssetDelete { return pendingAssetDelete{} })
	wizardOpen := ui.Local(host, "local-engine-wizard-open", func() bool { return false })
	if s.wizardOpenRequested {
		*wizardOpen = true
		s.wizardOpenRequested = false
	}
	host.Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			s.header(c, tr(c, "models.page.title"))
			ui.Box(c).Grow(1)
			s.modelsIconPageActions(c, wizardOpen)
		})
		if *wizardOpen {
			mt := tokens(c)
			applyMaterial(c, ui.Column(c).FillWidth().Gap(mt.Density.CardGap).Padding(mt.Card.Padding), materialCard).Children(func() {
				studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(mt.Density.Row), func() {
					ui.Row(c).Grow(1).MinWidth(0).Children(func() {
						heading(c, tr(c, "models.wizard.card_title"), 2)
					})
					if outlineIconButton(c, reicon.ChevronUp, tr(c, "models.action.collapse")).Clicked() {
						*wizardOpen = false
					}
				})
				s.localEngineRegistrationView(c)
			})
		}
		card(c, tr(c, "models.section.installed_models"), func() {
			modelErr := s.registry.ModelError
			if s.suppressOutdatedCLIError(modelErr) {
				modelErr = nil
			}
			errorView(c, modelErr)
			s.modelStorageAndRootsView(c)
			if len(s.registry.Models) == 0 {
				emptyGuideCard(c, emptyGuideOptions{
					Icon:    reicon.Layers,
					Title:   tr(c, "models.empty.no_models"),
					Surface: emptySunken,
					Action: func() {
						button(c, tr(c, "models.action.go_downloads")).OnClick(func() { s.navigateTo("library", "") })
					},
				})
				return
			}
			ui.Column(c).FillWidth().Children(func() {
				for i, model := range s.registry.Models {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					m := model
					c.Key(m.Name)
					s.installedModelRow(c, m, pending)
				}
			})
		})
		card(c, tr(c, "models.section.installed_engines"), func() {
			if len(s.registry.Engines) == 0 {
				emptyGuideCard(c, emptyGuideOptions{
					Icon:    reicon.Cpu,
					Title:   tr(c, "models.empty.no_engines"),
					Surface: emptySunken,
					Action: func() {
						ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.CardGap).Children(func() {
							button(c, tr(c, "models.action.goto_downloads")).OnClick(func() { s.navigateTo("library", "") })
							button(c, tr(c, "models.action.configure_local_engine")).OnClick(func() { *wizardOpen = true })
						})
					},
				})
				return
			}
			ui.Text(c, s.engineFit()).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			ui.Column(c).FillWidth().Children(func() {
				for i, engine := range s.registry.Engines {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					eng := engine
					c.Key(eng.Name)
					s.installedEngineRow(c, eng, pending)
				}
			})
		})
		card(c, tr(c, "engine.discovery.heading"), func() {
			if len(s.registry.Models) == 0 || len(s.registry.Engines) == 0 {
				s.managedServiceView(c)
				return
			}
			s.discoveryView(c, false)
		})
	})
	s.standaloneDrawer(c)
	s.assetDeleteDialog(c, pending)
}

func (s *viewState) modelsIconPageActions(c *ui.Context, wizardOpen *bool) {
	t := tokens(c)
	studioTooltipGroup(c, ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.CardGap), func() {
		outlineIconButton(c, reicon.Download, tr(c, "models.action.download_more")).OnClick(func() {
			s.libraryTab = 0
			s.navigateTo("library", "")
		})
		outlineIconButton(c, reicon.FolderPlus, tr(c, "models.action.add_model_dir")).Disabled(s.busy || !s.canBrowse()).OnClick(func() {
			s.pickExtraModelsRoot(emptyText(s.settings.Effective.ModelsRoot))
		})
		engineLabel := tr(c, "engine.wizard.title")
		engineIcon := reicon.Link
		if *wizardOpen {
			engineLabel = tr(c, "models.action.collapse_add_engine")
			engineIcon = reicon.ChevronUp
		}
		outlineIconButton(c, engineIcon, engineLabel).OnClick(func() { *wizardOpen = !*wizardOpen })
		refreshIconButton(c, tr(c, "models.action.refresh"), s.registry.Refreshing || s.refreshing).
			Disabled(s.registry.Refreshing || s.busy).
			OnClick(s.refreshAssetsAction)
	})
}

func modelTaskAction(c *ui.Context, s *viewState, model core.ModelAsset) {
	if !model.Available {
		return
	}
	label := tr(c, "models.action.benchmark_with_model")
	var reason string
	switch {
	case s.environmentMissing():
		reason = tr(c, "shell.nav.new_task_hint.env_missing")
	case s.busy:
		reason = tr(c, "models.action.blocked.busy")
	case s.roundWorkActive():
		reason = tr(c, "models.action.benchmark_blocked_hint")
	}
	if modelRowButtonClicked(c, outlineIconButton, reicon.Gauge, label, model.Name, reason) {
		if eng := s.resolveCompatibleEngineForModel(model.Name); eng != "" {
			s.newBenchmarkWithSelection(eng, model.Name)
		} else {
			s.newBenchmark()
		}
	}
}

func modelRowButtonClicked(c *ui.Context, render func(*ui.Context, func() *ui.SVG, string) ui.Element, icon func() *ui.SVG, label, modelName, reason string) bool {
	name := label + " · " + modelName
	renderButton := func() ui.Element {
		return render(c, icon, "").Label(name).Size(tokens(c).Control.MD, tokens(c).Control.MD)
	}
	if reason != "" {
		iconButtonWithTooltip(c, reason, func() ui.Element {
			return renderButton().Disabled(true)
		})
		return false
	}
	btn := renderButton()
	studioTooltipVisual(c, btn, label)
	return btn.Clicked()
}

func modelDetails(model core.ModelAsset) string {
	parts := []string{}
	if model.Format != "" && model.Format != "gguf" && model.Format != "strata_pack" && model.Format != "directory" {
		parts = append(parts, model.Format)
	}
	if model.Architecture != "" {
		parts = append(parts, l10n.TRf("models.detail.arch", "arch", model.Architecture))
	}
	if model.SizeBytes != nil {
		parts = append(parts, l10n.TRf("models.detail.approx_gib", "gib", fmt.Sprintf("%.1f", float64(*model.SizeBytes)/(1<<30))))
	}
	if model.ContextLength != nil {
		parts = append(parts, l10n.TRf("models.detail.context_tokens", "tokens", strconv.FormatInt(*model.ContextLength, 10)))
	}
	if !model.Available {
		reason := strings.TrimSpace(deviceLabel(model.Reason))
		if reason == "" {
			reason = strings.TrimSpace(model.Reason)
		}
		if reason != "" {
			parts = append(parts, l10n.TRf("models.detail.unavailable_reason", "reason", reason))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return joinParts(parts)
}

func (s *viewState) engineBadgeState(engine core.EngineAsset) (string, stateKind) {
	switch s.eligibility(engine.Name).State {
	case core.EngineCheckEligible:
		return l10n.TR("engine.badge.eligible"), stateSuccess
	case core.EngineCheckChecking:
		return l10n.TR("engine.badge.checking"), stateActive
	case core.EngineCheckIneligible:
		return l10n.TR("engine.badge.ineligible"), stateDanger
	default:
		return l10n.TR("engine.badge.unknown"), stateNeutral
	}
}

func (s *viewState) engineStateLine(engine core.EngineAsset) string {
	parts := []string{}
	if engine.Family != "" {
		parts = append(parts, engine.Family)
	}
	declLabel := l10n.TR("engine.detail.installation_decl")
	if engine.Kind == "local_installation" && !strings.Contains(engine.Source, declLabel) {
		parts = append(parts, declLabel)
	}
	if engine.Source != "" {
		parts = append(parts, engine.Source)
	}
	el := s.eligibility(engine.Name)
	switch el.State {
	case core.EngineCheckEligible:
		if len(parts) == 0 {
			parts = append(parts, l10n.TR("engine.detail.verified"))
		}
	case core.EngineCheckChecking:
		parts = append(parts, l10n.TR("engine.detail.checking"))
	case core.EngineCheckIneligible:
		if el.Release != "" {
			parts = append(parts, l10n.TRf("engine.detail.ineligible_release", "reason", human(el.Reason), "release", el.Release))
		} else {
			parts = append(parts, l10n.TRf("engine.detail.ineligible", "reason", human(el.Reason)))
		}
	default:
		if !s.checks.Supported {
			parts = append(parts, l10n.TR("engine.detail.unverified_cli_old"))
		} else if el.Reason != "" {
			parts = append(parts, l10n.TRf("engine.detail.unverified_reason", "reason", human(el.Reason)))
		} else {
			parts = append(parts, l10n.TR("engine.badge.unknown"))
		}
	}
	return joinParts(parts)
}

func joinParts(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += " · "
		}
		out += part
	}
	return out
}

func (s *viewState) engineFit() string {
	if !s.checks.Supported && len(s.registry.Engines) > 0 {
		return l10n.TR("engine.fit.cli_outdated")
	}
	eligible, ineligible := 0, 0
	for _, e := range s.registry.Engines {
		switch s.eligibility(e.Name).State {
		case core.EngineCheckEligible:
			eligible++
		case core.EngineCheckIneligible:
			ineligible++
		}
	}
	if s.checks.Checking > 0 {
		return l10n.TRf("engine.fit.checking", "eligible", strconv.Itoa(eligible), "ineligible", strconv.Itoa(ineligible))
	}
	return l10n.TRf("engine.fit.summary", "eligible", strconv.Itoa(eligible), "ineligible", strconv.Itoa(ineligible))
}

func (s *viewState) refreshAssetsAction() {
	if s.simulate(previewReload) {
		return
	}
	if s.busy {
		return
	}
	if s.service != nil {
		_ = s.service.RefreshAssets()
		_ = s.service.RefreshDevice()
		go s.service.VerifyEngines()
	}
	go s.refresh(true)
}
