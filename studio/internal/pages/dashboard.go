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
	case "run", "resume":
		return 4
	case "done":
		return 6
	default:
		return 5
	}
}

func (s *viewState) dashboardView(c *ui.Context) {
	s.header(c, "Dashboard")
	ui.Text(c, "配置、运行与跟踪本地模型测评。")
	if s.environmentMissing() {
		s.setupGuide(c)
	}
	s.conclusionView(c)
	s.stageView(c)
	if len(s.boundConfigs()) > 0 || s.progress == nil {
		s.quickRunView(c)
	}
	c.Key("tasks-card")
	card(c, "任务队列", func() { s.tasksView(c) })
	c.Key("history-card")
	card(c, "运行历史", func() { errorView(c, s.historyError); s.historyView(c) })
	if s.resumeSource != nil {
		s.resumeView(c)
	}
}

func (s *viewState) environmentMissing() bool {
	return s.ready.CLI.Status != "ok" || !s.ready.Workspace.Initialized || !s.ready.Results.Exists || !s.ready.Models.Exists
}

func (s *viewState) conclusionView(c *ui.Context) {
	card(c, "工作区进度", func() {
		step := s.step()
		ui.Row(c).Wrap().Gap(c.Theme().Space(2)).Label("六阶段进度").Children(func() {
			for i, label := range []string{"准备", "设备检查", "候选", "启动与绑定", "运行", "报告核验"} {
				switch {
				case i < step:
					ui.Text(c, "✓  "+label).TextColor(c.Theme().Success)
				case i == step:
					statusBadge(c, label, c.Theme().Accent)
				default:
					ui.Text(c, label).TextColor(c.Theme().TextMuted)
				}
			}
		})
		progressBar(c, float64(step)/6, "工作区六阶段准备进度").Description("六个准备阶段，不表示样本完成率。")
		if s.progress != nil {
			for _, b := range s.progress.Bundles {
				ui.Text(c, b.Bundle+" · "+stateLabel(b.Phase))
				if b.BlockedReason != nil {
					ui.Text(c, human(*b.BlockedReason)).TextColor(c.Theme().Danger)
				}
			}
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
			if primaryButton(c, "自动安装 CLI "+core.Version).AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
				s.installCLI()
			}
		})
	}
	s.readinessView(c)
	if s.ready.Workspace.Initialized {
		return
	}
	card(c, "初始化工作区", func() {
		ui.Text(c, "初始化目标必须是新目录；成功后自动切换工作区。")
		if s.ready.CLI.Status != "ok" {
			ui.Text(c, "请先安装 CLI，才能初始化工作区。")
		}
		field(c, "新工作区目录", &s.initDir).Description("必填绝对路径；目标必须为新目录。").Error(pathError(s.initDir))
		if primaryButton(c, "初始化工作区").AlignSelf(ui.Start).Disabled(s.busy || s.initDir == "" || s.ready.CLI.Status != "ok").Clicked() {
			if message := pathError(s.initDir); message != "" {
				s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
				return
			}
			s.execute("inferyard init --out "+quote(s.initDir), false)
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

func (s *viewState) readinessView(c *ui.Context) {
	card(c, "环境准备", func() {
		cli := "请安装 CLI 或在设置中配置路径"
		if s.ready.CLI.Status == "ok" {
			cli = "已就绪 · " + text(s.ready.CLI.Version)
		}
		ui.Text(c, "CLI："+cli)
		for _, entry := range []struct {
			label string
			dir   core.DirectoryReadiness
		}{{"工作区", s.ready.Workspace}, {"结果目录", s.ready.Results}, {"模型目录", s.ready.Models}} {
			state := "未配置"
			if entry.dir.Exists {
				state = "目录可用"
			} else if entry.dir.Configured {
				state = "待创建"
			}
			if entry.label == "工作区" && entry.dir.Exists {
				state = "待初始化"
				if entry.dir.Initialized {
					state = "已初始化"
				}
			}
			ui.Text(c, entry.label+"："+configuredValue(emptyText(entry.dir.Path))+" · "+state)
		}
		ui.Row(c).Wrap().Gap(c.Theme().Space(2.5)).Children(func() {
			if button(c, "准备目录").AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
				s.provision()
			}
			ui.Text(c, "路径在设置页修改").TextColor(c.Theme().TextMuted)
		})

	})
}
func (s *viewState) provision() {
	s.guideWork(func() (string, *core.ApiError) {
		v, e := s.service.Provision()
		if e == nil {
			s.win.Update(func() { s.loadSettings(v) })
		}
		return "目录已准备，请在 Dashboard 初始化工作区。", e
	})
}

func (s *viewState) stageView(c *ui.Context) {
	action := ""
	if s.progress != nil {
		action = s.progress.NextAction
	}
	if s.progress != nil && s.progress.DefaultScopeComplete {
		action = "done"
	}
	if s.running() != nil {
		action = "run"
	}
	switch action {
	case "device_check":
		stepTrack(c, "2", "设备检查", func() {
			ui.Text(c, "检查硬件、本机模型与推荐方案，写入工作区预检证据；不发送模型请求。")
			if s.ready.CLI.Status != "ok" {
				ui.Text(c, "请先安装 CLI，再运行设备检查。")
			}
			if primaryButton(c, "运行设备检查").AlignSelf(ui.Start).Disabled(s.busy || s.ready.CLI.Status != "ok").Clicked() {
				s.execute(s.nextCommand(), false)
			}
			ui.Text(c, "检查完成后，可在设备页查看明细；需要下载模型或引擎时，前往模型与下载页。")
			button(c, "查看设备页明细").AlignSelf(ui.Start).OnClick(func() { s.selected = "device" })
		})
	case "prepare_candidate":
		stepTrack(c, "3", "创建候选", func() { s.candidateView(c) })
	case "bind":
		stepTrack(c, "4", "启动与绑定", func() { s.serviceView(c) })
	case "run", "resume":
		if b := s.active(); b != nil && (emptyText(b.CandidatePath) != "" || emptyText(b.BoundPath) != "") {
			card(c, "模型服务", func() { s.serviceView(c) })
		}
		stepTrack(c, "5", "运行测评", func() { s.runView(c) })
	case "report":
		stepTrack(c, "6", "报告核验", func() { s.reportView(c) })
	case "verify":
		stepTrack(c, "6", "报告核验", func() {
			ui.Text(c, "报告已生成，尚未核验")
			if primaryButton(c, "核验报告").AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
				s.execute(s.nextCommand(), true)
			}
		})
	case "done":
		stepTrack(c, "6", "测评完成", func() {
			statusBadge(c, "已核验 verified", c.Theme().Success)
			button(c, "查看报告").AlignSelf(ui.Start).OnClick(func() { s.selected = "reports" })
		})
	default:
		if s.progress == nil {
			ui.Text(c, "无法读取工作区进度，请刷新重试")
		} else {
			ui.Text(c, "未识别的进度阶段："+action)
		}
	}
}
