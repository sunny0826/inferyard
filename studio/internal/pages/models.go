package pages

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validName(s string) bool { return core.ValidDownloadName(s) }
func quote(s string) string   { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

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
		s.header(c, tr(c, "models.page.title"))
		s.modelsPageActions(c, wizardOpen)
		if *wizardOpen {
			card(c, tr(c, "models.wizard.card_title"), func() {
				s.localEngineWizardView(c)
				button(c, tr(c, "models.action.collapse")).OnClick(func() { *wizardOpen = false })
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
					Icon:    lucide.Layers,
					Title:   tr(c, "models.empty.no_models"),
					Surface: emptySunken,
					Action: func() {
						button(c, tr(c, "models.action.go_downloads")).OnClick(func() { s.selected = "downloads" })
					},
				})
				return
			}
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Tight), materialInset).Children(func() {
				for _, model := range s.registry.Models {
					m := model
					c.Key(m.Name)
					listRow(c, listRowOptions{
						Title:    modelLabel(m),
						Subtitle: modelDetails(m),
						Trailing: func() { s.modelRowTrailing(c, m, pending) },
					})
				}
			})
		})
		card(c, tr(c, "models.section.installed_engines"), func() {
			if len(s.registry.Engines) == 0 {
				emptyGuideCard(c, emptyGuideOptions{
					Icon:    lucide.Cpu,
					Title:   tr(c, "models.empty.no_engines"),
					Surface: emptySunken,
					Action: func() {
						ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.CardGap).Children(func() {
							button(c, tr(c, "models.action.goto_downloads")).OnClick(func() { s.selected = "downloads" })
							button(c, tr(c, "models.action.configure_local_engine")).OnClick(func() { *wizardOpen = true })
						})
					},
				})
				return
			}
			ui.Text(c, s.engineFit()).TextColor(t.Color.FgSecondary)
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Tight), materialInset).Children(func() {
				for _, engine := range s.registry.Engines {
					eng := engine
					c.Key(eng.Name)
					listRow(c, listRowOptions{
						Title:    eng.Name,
						Subtitle: s.engineStateLine(eng),
						Trailing: func() { s.engineRowTrailing(c, eng, pending) },
					})
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
	s.assetDeleteDialog(c, pending)
}

func modelTaskAction(c *ui.Context, s *viewState, model core.ModelAsset) {
	if !model.Available {
		return
	}
	label := tr(c, "models.action.benchmark_with_model")
	hint := tr(c, "models.action.benchmark_blocked_hint")
	blocked := s.busy || s.roundWorkActive()
	if blocked {
		wrap := ui.Box(c)
		wrap.Children(func() {
			button(c, label).Label(label + " · " + model.Name).Disabled(true).Description(hint)
		})
		components.Tooltip(c, wrap, hint)
		return
	}
	b := button(c, label).Label(label + " · " + model.Name)
	if b.Clicked() {
		s.model = model.Name
		s.newBenchmark()
	}
}

func modelDetails(model core.ModelAsset) string {
	parts := []string{}
	if model.Format != "" {
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
		reason := deviceLabel(model.Reason)
		if reason == "" {
			reason = model.Reason
		}
		parts = append(parts, l10n.TRf("models.detail.unavailable_reason", "reason", reason))
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
	if engine.Kind == "local_installation" {
		parts = append(parts, l10n.TR("engine.detail.installation_decl"))
	}
	if engine.Source != "" {
		parts = append(parts, engine.Source)
	}
	el := s.eligibility(engine.Name)
	switch el.State {
	case core.EngineCheckEligible:
		parts = append(parts, l10n.TR("engine.detail.verified"))
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

func (s *viewState) adoptDownloadedEngine() {
	if s.engine != "" {
		return
	}
	for _, task := range s.downloads {
		if task.State != "done" || (task.Kind != "polystrata" && task.Kind != "engine") {
			continue
		}
		wantPath := cleanMatchPath(emptyText(task.InstallationPath))
		if wantPath == "" {
			wantPath = cleanMatchPath(emptyText(task.EnginePath))
		}
		if wantPath == "" {
			continue
		}
		for _, e := range s.registry.Engines {
			if cleanMatchPath(e.Path) != wantPath {
				continue
			}
			if s.checks.Engines[e.Name].State == core.EngineCheckEligible || !s.checks.Supported {
				s.engine = e.Name
				return
			}
		}
	}
	for _, e := range s.registry.Engines {
		if s.checks.Engines[e.Name].State == core.EngineCheckEligible {
			s.engine = e.Name
			return
		}
	}
	if !s.checks.Supported && len(s.registry.Engines) > 0 {
		s.engine = s.registry.DefaultEngine
	}
}
