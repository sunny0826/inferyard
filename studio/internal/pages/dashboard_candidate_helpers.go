package pages

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func (w *viewState) modelMetadata(name string) string {
	for _, m := range w.registry.Models {
		if m.Name != name {
			continue
		}
		parts := []string{}
		if m.Architecture != "" {
			parts = append(parts, l10n.TRf("dashboard.model_meta.arch", "arch", m.Architecture))
		}
		if m.SizeBytes != nil {
			parts = append(parts, l10n.TRf("dashboard.model_meta.size_gib", "gib", fmt.Sprintf("%.1f", float64(*m.SizeBytes)/(1<<30))))
		}
		return strings.Join(parts, " · ")
	}
	return ""
}

func bundleDescription(bundle inferyard.BundleProgress) string {
	if text := inferyard.ResolveLocalizedText(bundle.Description, l10n.CurrentLocale()); text != "" {
		return text
	}
	switch bundle.Bundle {
	case inferyard.DefaultBundleCore:
		return l10n.TR("dashboard.bundle.desc_core")
	case inferyard.DefaultBundlePelican:
		return l10n.TR("dashboard.bundle.desc_pelican")
	default:
		return ""
	}
}

func (s *viewState) bundleSelection(c *ui.Context) {
	s.checkedBundles()
	if s.progress == nil {
		return
	}
	t := tokens(c)
	cols := min(2, max(1, len(s.progress.Bundles)))
	if compactLayout(c) {
		cols = 1
	}
	ui.Field(c, tr(c, "dashboard.candidate.bundle"), func() {
		ui.Grid(c).FillWidth().Columns(cols).Gap(t.Density.Row).Children(func() {
			for _, b := range s.progress.Bundles {
				bundle := b
				checked := s.selectedBundles[bundle.Bundle]
				tile := ui.Row(c.Key("bundle-card-" + bundle.Bundle)).FillWidth().AlignItems(ui.Center).
					Gap(t.Density.Row).Padding(t.Density.Stack).Radius(t.Radius.Control)
				bg := t.Color.Sunken
				border := t.Color.BorderSubtle
				borderW := float32(1)
				if checked {
					bg = t.Color.Surface
					if c.Theme().Dark {
						bg = t.Color.Elevated
					}
					border = t.Color.Accent
					borderW = 1.5
				} else if tile.Hovered() {
					bg = t.Color.SurfaceHover
					border = t.Color.BorderStrong
				}
				tile.Background(bg).Border(borderW, border)
				var cbChanged bool
				tile.Children(func() {
					cb := components.Checkbox(c, &checked, bundle.Bundle)
					if cb.Changed() {
						cbChanged = true
						s.selectedBundles[bundle.Bundle] = checked
					}
					if description := bundleDescription(bundle); description != "" {
						ui.Text(c, "· "+description).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).MinWidth(0).SingleLine()
					}
					ui.Box(c).Grow(1)
					phaseKind := stateNeutral
					switch bundle.Phase {
					case "run_complete":
						phaseKind = stateSuccess
					case "run_incomplete":
						phaseKind = stateWarning
					}
					inlineBadge(c, stateLabel(bundle.Phase), phaseKind, badgeOptions{Size: sizeSM, Style: badgeSoft})
				})
				if tile.Clicked() && !cbChanged {
					s.selectedBundles[bundle.Bundle] = !checked
				}
			}
		})
	})
	if len(s.checkedBundles()) == 0 {
		ui.Text(c, tr(c, "dashboard.candidate.select_bundle_required")).FontSize(t.Type.Caption).TextColor(stateColor(c, stateWarning))
	}
}

func blockedPlan(b inferyard.BundleProgress) bool {
	args, e := inferyard.SplitCommand(emptyText(b.NextCommand))
	if e != nil {
		return false
	}
	for _, arg := range args {
		if arg == "--plan" {
			return true
		}
	}
	return false
}

func (s *viewState) verifyTaskEngines() {
	if s.service == nil || (s.win == nil && s.updateWindow == nil) || s.engineVerifyPending {
		return
	}
	unchecked := s.hasUncheckedRegistryEngine()
	if !unchecked && (s.win == nil || s.task.stage != "prepare_candidate" || s.busy || s.refreshing) {
		return
	}
	now := time.Now()
	if !unchecked && !s.task.lastVerify.IsZero() && now.Sub(s.task.lastVerify) < 5*time.Second {
		return
	}
	s.task.lastVerify = now
	s.engineVerifyPending = true
	go func() {
		s.service.VerifyEngines()
		apply := func() {
			s.engineVerifyPending = false
			if s.ctx != nil && s.ctx.Err() != nil {
				return
			}
			s.checks = s.service.EngineChecks()
		}
		if s.updateWindow != nil {
			s.updateWindow(apply)
		} else if s.win != nil {
			s.win.Update(apply)
		}
	}()
}

func (s *viewState) checkWorkspaceDevice() {
	if s.progress == nil || s.progress.DevicePreflight.Status != "missing" || s.busy || s.ready.CLI.Status != "ok" || s.needsTaskAssets() {
		return
	}
	if s.simulate(previewCheckDevice) || s.service == nil {
		return
	}
	request, ok := s.takeWorkspaceDeviceRequest()
	if !ok {
		return
	}
	s.guideWork(func() (string, *core.ApiError) {
		r, e := s.service.WizardExecute(s.ctx, request)
		if e != nil {
			return "", e
		}
		if missingModelResult(r) {
			return "", nil
		}
		if r.ExitCode != 0 {
			return "", wizardFailure(r)
		}
		return l10n.TR("dashboard.toast.device_check_done"), nil
	})
}

func (s *viewState) taskBundles() []inferyard.BundleProgress {
	if s.progress == nil {
		return nil
	}
	if len(s.task.bundles) == 0 {
		return s.progress.Bundles
	}
	return selectedProgress(s.progress, s.task.bundles).Bundles
}

func (s *viewState) takeWorkspaceDeviceRequest() (core.WizardRequest, bool) {
	if s.progress == nil || s.progress.DevicePreflight.Status != "missing" || s.deviceChecked == s.progress.Workspace {
		return core.WizardRequest{}, false
	}
	s.deviceChecked = s.progress.Workspace
	return core.WizardRequest{Command: s.nextCommand(), ModelName: ""}, true
}
