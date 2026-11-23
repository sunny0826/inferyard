package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
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
	for _, task := range s.downloads {
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
	card(c, "准备模型与引擎", func() {
		for _, asset := range []struct {
			kind, label string
			available   bool
		}{{"engine", "引擎", assets.engine}, {"model", "模型", assets.model}} {
			status := "待下载"
			kind := stateNeutral
			if asset.available {
				status = "已发现"
				kind = stateSuccess
			} else {
				for _, task := range s.downloads {
					if (task.Kind == asset.kind || task.Kind == "polystrata") && task.State == "downloading" {
						status = "下载中"
						kind = stateActive
						break
					}
				}
			}
			badge(c, asset.label+" · "+status, kind, badgeOptions{Style: badgeDot})
		}
		if s.registry.Refreshing {
			ui.Text(c, "正在读取本地资源…").TextColor(tokens(c).Color.FgSecondary)
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
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			primaryButton(c, "到下载页").OnClick(func() { s.selected = "downloads" })
			button(c, "使用已有资源").OnClick(func() { s.selected = "settings" })
		})
	})
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
			button(c, "返回任务").Shrink(0).OnClick(func() { s.selected = "dashboard" })
		})
}

func (s *viewState) downloadGuideStatus() string {
	assets := s.availableTaskAssets()
	active := s.activeDownloadCount()
	failed := s.unresolvedDownloadFailures()
	switch {
	case active > 0 && failed > 0:
		return fmt.Sprintf("正在下载 %d 项资源，另有 %d 项下载失败。", active, failed)
	case active > 0:
		return fmt.Sprintf("正在下载 %d 项资源…", active)
	case failed > 0:
		return fmt.Sprintf("有 %d 项下载失败。", failed)
	case assets.model && assets.engine:
		return "模型与引擎已就绪，可返回任务页开始测评。"
	default:
		return "下载所需引擎与模型后返回任务页。"
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
