package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (w *viewState) step() int {
	if w.running() != nil {
		return 4
	}
	if w.progress == nil {
		return 0
	}
	if w.progress.DefaultScopeComplete {
		return 6
	}
	switch w.progress.NextAction {
	case "device_check":
		return 1
	case "prepare_candidate":
		return 2
	case "bind":
		return 3
	case "run":
		return 4
	default:
		return 5
	}
}

func (s *viewState) wizardView(c *ui.Context) {
	s.header(c, "首次上手")
	card(c, "工作区进度", func() {
		step := s.step()
		for i, label := range []string{"准备", "设备检查", "候选", "启动与绑定", "运行", "报告核验"} {
			mark := "○"
			if i < step {
				mark = "✓"
			} else if i == step {
				mark = "●"
			}
			ui.Text(c, mark+"  "+label)
		}
		ui.Progress(c, float64(step)/6)
		if s.progress != nil {
			for _, b := range s.progress.Bundles {
				ui.Text(c, b.Bundle+" · "+stateLabel(b.Phase))
				if b.BlockedReason != nil {
					ui.Text(c, *b.BlockedReason).TextColor(c.Theme().Danger)
				}
			}
		}
	})
	card(c, "下一步", func() {
		page, label := "settings", "下一步：在设置页安装 CLI 或初始化工作区"
		switch s.step() {
		case 1:
			page, label = "device", "下一步：在设备页检查设备"
			ui.Text(c, "需要下载模型或引擎时，先前往「模型与下载」页。")
		case 2:
			page, label = "runs", "下一步：在运行页创建候选"
		case 3:
			page, label = "runs", "下一步：在运行页启动与绑定服务"
		case 4:
			page, label = "runs", "下一步：在运行页运行当前题包"
		case 5:
			page, label = "reports", "下一步：在报告页生成报告并核验"
		case 6:
			page, label = "reports", "已完成：在报告页查看已核验报告"
		}
		if ui.PrimaryButton(c, label).AlignSelf(ui.Start).Clicked() {
			s.selected = page
			s.detailID = ""
			go s.refresh(true)
		}
	})
}
func (s *viewState) setupGuide(c *ui.Context) {
	if s.ready.CLI.Status != "ok" {
		card(c, "安装 InferYard CLI", func() {
			ui.Text(c, "自动安装通过 uv 安装匹配版本的 CLI；缺少 uv 时下载官方独立工具，不修改终端配置。也可在路径设置中手动指定路径。")
			if s.cliInstallStatus != "" {
				ui.Text(c, s.cliInstallStatus)
			}
			if ui.PrimaryButton(c, "自动安装 CLI "+core.Version).AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
				s.installCLI()
			}
		})
	}
	card(c, "初始化工作区", func() {
		if s.ready.Workspace.Initialized {
			ui.Text(c, "工作区已初始化："+emptyText(s.ready.Workspace.Path)).TextColor(c.Theme().Success)
			return
		}
		ui.Text(c, "初始化目标必须是新目录；成功后自动切换工作区。")
		field(c, "新工作区目录", &s.initDir)
		if ui.PrimaryButton(c, "初始化工作区").AlignSelf(ui.Start).Disabled(s.busy || s.initDir == "" || s.ready.CLI.Status != "ok").Clicked() {
			s.execute("inferyard init --out "+quote(s.initDir), false)
		}
	})
}
func (s *viewState) runGuide(c *ui.Context) {
	if b := s.active(); b != nil && (b.CandidatePath != nil || b.BoundPath != nil) {
		card(c, "模型服务", func() { s.serviceView(c) })
	}
	if s.running() != nil {
		card(c, "进度引导", func() { s.runView(c) })
		return
	}
	if s.progress == nil {
		return
	}
	card(c, "进度引导", func() {
		if b := s.active(); b != nil && b.BlockedReason != nil {
			ui.Text(c, human(*b.BlockedReason)).TextColor(c.Theme().Danger)
		}
		switch s.progress.NextAction {
		case "prepare_candidate":
			s.candidateView(c)
		case "bind":
			ui.Text(c, "请在模型服务卡中启动并绑定服务。")
		case "run":
			s.runView(c)
		default:
			ui.Text(c, "请在向导总览查看下一步。")
		}
	})
}
func (s *viewState) reportGuide(c *ui.Context) {
	if s.progress == nil {
		return
	}
	if s.progress.DefaultScopeComplete {
		ui.Badge(c, "已核验 verified")
		return
	}
	if len(s.progress.Bundles) == 0 {
		return
	}
	for _, b := range s.progress.Bundles {
		if b.Phase != "run_complete" {
			return
		}
	}
	card(c, "生成报告并核验", func() { s.reportView(c) })
}
