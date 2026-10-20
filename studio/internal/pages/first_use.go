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

func (s *viewState) adoptOnlyModel() {
	if s.model != "" {
		return
	}
	name := ""
	for _, model := range s.registry.Models {
		if model.Available {
			if name != "" {
				return
			}
			name = model.Name
		}
	}
	s.model = name
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
		ui.Text(c, "开始测评前，需要一个本地模型和运行它的引擎。")
		ui.Text(c, "可在下载页获取推荐资源；已有资源可在设置中指定目录。")
		for _, asset := range []struct {
			kind, label string
			available   bool
		}{{"engine", "引擎", assets.engine}, {"model", "模型", assets.model}} {
			status := "待下载"
			if asset.available {
				status = "已发现"
			} else {
				for _, task := range s.downloads {
					if task.Kind == asset.kind && task.State == "downloading" {
						status = "下载中"
						break
					}
				}
			}
			ui.Text(c, asset.label+" · "+status)
		}
		if s.registry.Refreshing {
			ui.Text(c, "正在读取本地资源…")
		}
		errorView(c, s.registry.ModelError)
		shown := map[string]bool{}
		for _, bundle := range s.taskBundles() {
			reason := emptyText(bundle.BlockedReason)
			if reason != "" && reason != "no_local_model" && !shown[reason] {
				ui.Text(c, human(reason)).TextColor(c.Theme().Danger)
				shown[reason] = true
			}
		}
		if s.engineState.State == "orphaned" {
			s.stopOrphanButton(c)
		}
		ui.Row(c).Wrap().Gap(c.Theme().Space(2)).Children(func() {
			primaryButton(c, "到下载页").OnClick(func() { s.selected = "downloads" })
			button(c, "使用已有资源").OnClick(func() { s.selected = "settings" })
		})
		ui.Text(c, "资源就绪后返回任务，继续选择模型和测评内容。").TextColor(c.Theme().TextMuted)
	})
}

func (s *viewState) activeDownloadCount() int {
	count := 0
	for _, task := range s.downloads {
		if task.State == "downloading" {
			count++
		}
	}
	return count
}

func (s *viewState) downloadTaskGuide(c *ui.Context) {
	if !s.preparingTask() || s.environmentMissing() {
		return
	}
	card(c, "准备测评资源", func() {
		assets := s.availableTaskAssets()
		switch {
		case assets.model && assets.engine:
			ui.Text(c, "已发现模型与引擎，可以返回任务选择测评。引擎仍需通过核验。")
		case s.activeDownloadCount() > 0:
			ui.Text(c, fmt.Sprintf("正在下载 %d 项资源，完成后可返回任务继续。", s.activeDownloadCount()))
		default:
			ui.Text(c, "下载缺少的引擎与模型，然后返回任务。")
		}
		primaryButton(c, "返回任务").AlignSelf(ui.Start).OnClick(func() { s.selected = "dashboard" })
	})
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
