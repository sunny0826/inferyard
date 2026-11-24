package pages

import (
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type taskAssets struct {
	model  bool
	engine bool
}

func (s *viewState) availableTaskAssets() taskAssets {
	assets := taskAssets{engine: len(s.registry.Engines) > 0}
	for _, model := range s.registry.Models {
		assets.model = assets.model || model.Available
	}
	return assets
}

func (s *viewState) selectedModelPath() string {
	if s.model == "" {
		return ""
	}
	for _, model := range s.registry.Models {
		if model.Name == s.model {
			return cleanMatchPath(model.Path)
		}
	}
	return ""
}

func (s *viewState) reconcileSelectedModel(prevModelPath string) bool {
	if s.model == "" {
		return false
	}
	for _, model := range s.registry.Models {
		if model.Name == s.model && model.Available {
			return false
		}
	}
	if prevModelPath != "" {
		var matches []string
		for _, model := range s.registry.Models {
			if !model.Available || model.Format != "strata_pack" {
				continue
			}
			for _, covered := range model.CoveredPaths {
				if cleanMatchPath(covered) == prevModelPath {
					matches = append(matches, model.Name)
					break
				}
			}
		}
		if len(matches) == 1 {
			s.model = matches[0]
			return false
		}
		s.model = ""
		return true
	}
	s.model = ""
	return false
}

func (s *viewState) adoptOnlyModel() {
	if s.model != "" {
		return
	}
	name := ""
	availCount := 0
	for _, model := range s.registry.Models {
		if model.Available {
			name = model.Name
			availCount++
		}
	}
	if availCount == 1 {
		s.model = name
		return
	}
	if s.engine != "" {
		compat := ""
		compatCount := 0
		for _, model := range s.registry.Models {
			if model.Available && s.compatibleModel(model) {
				compat = model.Name
				compatCount++
			}
		}
		if compatCount == 1 {
			s.model = compat
			return
		}
	}
	for i := len(s.downloads) - 1; i >= 0; i-- {
		task := s.downloads[i]
		if task.Kind != "polystrata" || task.State != "done" || task.ModelConfigPath == nil {
			continue
		}
		wantPath := cleanMatchPath(*task.ModelConfigPath)
		if wantPath == "" {
			continue
		}
		for _, model := range s.registry.Models {
			if !model.Available || model.Format != "strata_pack" {
				continue
			}
			if s.engine != "" && !s.compatibleModel(model) {
				continue
			}
			if cleanMatchPath(model.Path) == wantPath {
				s.model = model.Name
				return
			}
		}
	}
	if localEng := s.localEngine(); localEng != nil && localEng.Family == "polystrata" && localEng.Path != "" {
		engDir := filepath.Dir(cleanMatchPath(localEng.Path))
		colocated := ""
		colocatedCount := 0
		for _, model := range s.registry.Models {
			if !model.Available || model.Format != "strata_pack" || !s.compatibleModel(model) {
				continue
			}
			if filepath.Dir(cleanMatchPath(model.Path)) == engDir {
				colocated = model.Name
				colocatedCount++
			}
		}
		if colocatedCount == 1 {
			s.model = colocated
		}
	}
}

func (s *viewState) preparingTask() bool {
	if s.running() != nil || s.binding || s.resumeSource != nil {
		return false
	}
	if s.task.stage != "" {
		return s.task.stage == "prepare_candidate"
	}
	return s.progress != nil && (s.progress.NextAction == "device_check" || s.progress.NextAction == "prepare_candidate")
}

func (s *viewState) needsTaskAssets() bool {
	assets := s.availableTaskAssets()
	return s.preparingTask() && (!assets.model || !assets.engine)
}

func (s *viewState) taskAssetsView(c *ui.Context) {
	assets := s.availableTaskAssets()
	rec := s.starterRecommendation()
	hasStarter := s.hasStarterContent(rec)
	t := tokens(c)
	card(c, tr(c, "onboarding.assets.title"), func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			for _, asset := range []struct {
				kind, label string
				available   bool
			}{
				{"engine", tr(c, "onboarding.assets.engine"), assets.engine},
				{"model", tr(c, "onboarding.assets.model"), assets.model},
			} {
				status := tr(c, "onboarding.assets.pending")
				kind := stateNeutral
				if asset.available {
					status = tr(c, "onboarding.assets.found")
					kind = stateSuccess
				} else {
					for _, task := range s.downloads {
						if (task.Kind == asset.kind || task.Kind == "polystrata") && task.State == "downloading" {
							status = tr(c, "onboarding.assets.downloading")
							kind = stateActive
							break
						}
					}
				}
				badge(c, asset.label+" · "+status, kind, badgeOptions{Style: badgeDot})
			}
		})
		if s.registry.Refreshing {
			ui.Text(c, tr(c, "onboarding.assets.refreshing")).TextColor(t.Color.FgSecondary)
		}
		errorView(c, s.registry.ModelError)
		shown := map[string]bool{}
		for _, bundle := range s.taskBundles() {
			reason := emptyText(bundle.BlockedReason)
			if reason != "" && reason != "no_local_model" && !shown[reason] {
				noticeBanner(c, noticeBannerOptions{
					Kind:    stateDanger,
					Size:    bannerCompact,
					Message: human(reason),
				})
				shown[reason] = true
			}
		}
		if s.engineState.State == "orphaned" {
			s.stopOrphanButton(c)
		}
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if s.activeDownloadCount() > 0 {
				primaryButton(c, tr(c, "onboarding.assets.view_install")).OnClick(func() { s.selected = "downloads" })
			} else if !hasStarter {
				primaryButton(c, tr(c, "onboarding.assets.go_downloads")).OnClick(func() { s.selected = "downloads" })
			}
			button(c, tr(c, "onboarding.assets.use_existing")).OnClick(func() { s.selected = "settings" })
		})
	})
	if hasStarter && s.activeDownloadCount() == 0 {
		s.renderStarterRecommendationPanel(c, "dashboard")
	}
}

func (s *viewState) activeDownloadCount() int {
	return s.countDownloads("downloading")
}

func (s *viewState) countDownloads(state string) int {
	count := 0
	for _, task := range s.downloads {
		if task.State == state {
			count++
		}
	}
	return count
}

func (s *viewState) unresolvedDownloadFailures() int {
	count := 0
	for i, task := range s.downloads {
		if task.State != "failed" || s.downloadResolved(task, i) {
			continue
		}
		count++
	}
	return count
}

func (s *viewState) downloadResolved(failed core.DownloadTask, failedIndex int) bool {
	for i, task := range s.downloads {
		if i >= failedIndex {
			break
		}
		if task.Kind != failed.Kind || task.SourceUrl != failed.SourceUrl || task.TaskId == failed.TaskId {
			continue
		}
		if failed.Kind == "polystrata" && failed.DisplayName != "" && task.DisplayName != "" && task.DisplayName != failed.DisplayName {
			continue
		}
		if task.State == "downloading" || task.State == "done" {
			return true
		}
	}
	return false
}

func (s *viewState) downloadTaskGuide(c *ui.Context) {
	if !s.preparingTask() || s.environmentMissing() {
		return
	}
	t := tokens(c)
	applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.CardGap).
		Padding(t.Density.Stack, t.Card.Padding), materialCard).
		Children(func() {
			ui.Text(c, s.downloadGuideStatus()).Grow(1).MinWidth(0).TextColor(t.Color.FgSecondary)
			button(c, tr(c, "onboarding.guide.back_dashboard")).Shrink(0).OnClick(func() { s.selected = "dashboard" })
		})
}

func (s *viewState) downloadGuideStatus() string {
	assets := s.availableTaskAssets()
	active := s.activeDownloadCount()
	failed := s.unresolvedDownloadFailures()
	switch {
	case active > 0 && failed > 0:
		return l10n.TRf("onboarding.guide.downloading_and_failed", "active", active, "failed", failed)
	case active > 0:
		return l10n.TRf("onboarding.guide.downloading", "active", active)
	case failed > 0:
		return l10n.TRf("onboarding.guide.failed", "failed", failed)
	case assets.model && assets.engine:
		return l10n.TR("onboarding.guide.ready")
	default:
		return l10n.TR("onboarding.guide.prompt")
	}
}

func (s *viewState) liveBundleTask(bundle string) *core.RunTask {
	for i := range s.tasks {
		task := &s.tasks[i]
		if task.Kind != "context_speed" && !terminal(task.State) && emptyText(task.Bundle) == bundle {
			return task
		}
	}
	return nil
}

func missingModelResult(r core.WizardResult) bool {
	return r.ExitCode == 2 && len(r.Limitations) == 1 && r.Limitations[0] == "no_local_model"
}
