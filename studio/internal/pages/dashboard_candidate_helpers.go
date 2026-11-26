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

func bundleDescription(bundle string) string {
	switch bundle {
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
	ui.Field(c, tr(c, "dashboard.candidate.bundle"), func() {
		ui.Column(c).Gap(t.Density.Tight).Children(func() {
			for _, b := range s.progress.Bundles {
				checked := s.selectedBundles[b.Bundle]
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					if components.Checkbox(c, &checked, b.Bundle).Changed() {
						s.selectedBundles[b.Bundle] = checked
					}
					if description := bundleDescription(b.Bundle); description != "" {
						ui.Text(c, "· "+description).TextColor(c.Theme().TextMuted)
					}
				})
			}
		})
	})
	if len(s.checkedBundles()) == 0 {
		ui.Text(c, tr(c, "dashboard.candidate.select_bundle_required"))
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
			s.adoptDownloadedEngine()
			s.adoptOnlyModel()
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
