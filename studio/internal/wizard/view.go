package wizard

import (
	"fmt"
	"math"
	"regexp"

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
func (w *viewState) view(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().Children(func() {
		ui.Column(c).Width(210).Padding(24).Gap(20).Background(t.Surface).Children(func() {
			ui.Text(c, "首次上手").Bold().FontSize(18)
			for i, label := range []string{"准备", "设备检查", "候选", "启动与绑定", "运行", "报告核验"} {
				mark := "○"
				if i < w.step() {
					mark = "✓"
				} else if i == w.step() {
					mark = "●"
				}
				ui.Text(c, mark+"  "+label).FontSize(15)
			}
			ui.Progress(c, float64(w.step())/6)
			if b := w.active(); b != nil {
				ui.Badge(c, b.Bundle)
			}
			if w.progress != nil {
				for _, b := range w.progress.Bundles {
					if b.Phase == "run_complete" {
						ui.Text(c, b.Bundle+" · 已封存").TextColor(t.Success)
					}
				}
			}
		})
		ui.Scroll(c).Grow(1).Padding(28).Children(func() {
			ui.Column(c).Padding(24).Gap(14).Background(t.Surface).Radius(14).Children(func() {
				if !w.loaded {
					ui.Text(c, "正在读取工作区进度…")
					return
				}
				if w.failure != "" {
					ui.Text(c, w.failure).TextColor(t.Danger)
					if ui.Button(c, "重新检查").Disabled(w.busy).Clicked() {
						go w.refresh()
					}
				}
				if b := w.active(); b != nil && b.BlockedReason != nil {
					ui.Text(c, human(*b.BlockedReason)).TextColor(t.Danger)
				}
				if w.message != "" {
					ui.Text(c, w.message)
				}
				switch w.step() {
				case 0:
					w.prepare(c)
				case 1:
					w.device(c)
				case 2:
					w.candidateView(c)
				case 3:
					w.serviceView(c)
				case 4:
					w.runView(c)
				case 5:
					w.reportView(c)
				case 6:
					w.done(c)
				}
				if ui.Button(c, "刷新进度").Disabled(w.busy).Clicked() {
					go w.refresh()
				}
				if w.busy {
					ui.Spinner(c)
					ui.Text(c, "正在执行，请稍候…")
				}
			})
		})
	})
}
func field(c *ui.Context, label string, value *string) {
	ui.Field(c, label, func() { ui.TextInput(c, value).Width(500) })
}
func (w *viewState) prepare(c *ui.Context) {
	if w.ready.CLI.Status != "ok" {
		ui.Text(c, "安装 InferYard CLI").Bold().FontSize(22)
		ui.Text(c, "CLI 尚未安装。自动安装经 uv 从 PyPI 安装，需要网络；未找到 uv 时会下载官方独立工具到 Studio 目录，不修改终端配置。")
		if w.installStatus != "" {
			ui.Text(c, w.installStatus)
		}
		if ui.PrimaryButton(c, "自动安装 CLI "+core.Version).Disabled(w.busy).Clicked() {
			w.installCLI()
		}
		if ui.Button(c, "手动指定路径").Disabled(w.busy).Clicked() {
			w.openMain("/settings")
		}
		return
	}
	ui.Text(c, "初始化工作区").Bold().FontSize(22)
	ui.Text(c, "初始化目标必须是新目录；成功后自动切换工作区。")
	field(c, "新工作区目录", &w.initDir)
	if ui.Button(c, "一键配置目录").Disabled(w.busy).Clicked() {
		w.work(func() (string, *core.ApiError) { _, e := w.service.Provision(); return "目录已配置。", e })
	}
	if ui.PrimaryButton(c, "初始化工作区").Disabled(w.busy || w.initDir == "").Clicked() {
		w.execute("inferyard init --out "+quote(w.initDir), false)
	}
}
func (w *viewState) device(c *ui.Context) {
	ui.Text(c, "准备模型与引擎，检查设备").Bold().FontSize(22)
	ui.Text(c, "已有本地文件可直接检查设备。下载写入新目录，不覆盖现有产物。")
	ui.Form(c, func() {
		field(c, "模型 URL（固定 revision 的 GGUF）", &w.modelURL)
		field(c, "凭据环境变量名（可留空，禁止明文 token）", &w.tokenEnv)
		if ui.Button(c, "下载模型").Disabled(w.busy || w.modelURL == "").Clicked() {
			w.acquireModel()
		}
		field(c, "引擎 URL", &w.engineURL)
		if ui.Button(c, "下载引擎").Disabled(w.busy || w.engineURL == "").Clicked() {
			w.execute("inferyard engine acquire --source "+quote(w.engineURL)+" --out "+quote(fresh(w.progress.Workspace+"/engines", "engine")), false)
		}
		field(c, "本地引擎文件绝对路径", &w.engine)
	})
	for _, d := range w.downloads {
		ui.Text(c, fmt.Sprintf("%s · 已下载 %.1f MiB", d.OutName, float64(d.DownloadedBytes)/(1024*1024)))
		if d.State == "downloading" {
			ui.Progress(c, math.NaN())
			id := d.TaskId
			if ui.Button(c, "取消下载 "+d.OutName).Clicked() {
				w.work(func() (string, *core.ApiError) {
					_, e := w.service.CancelDownload(id)
					return "已请求取消下载。", e
				})
			}
		}
		if d.State == "failed" || d.State == "cancelled" {
			ui.Text(c, "下载未完成，请检查来源后重新下载。")
		} else if d.State == "done" {
			ui.Badge(c, "下载完成")
		}
	}
	if ui.PrimaryButton(c, "检查设备").Disabled(w.busy).Clicked() {
		w.execute(w.nextCommand(), false)
	}
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (w *viewState) acquireModel() {
	if w.tokenEnv != "" && !envName.MatchString(w.tokenEnv) {
		w.failure = "只接受环境变量名，请勿输入明文 token。"
		return
	}
	root := text(w.ready.Models.Path)
	if root == "" {
		w.failure = human("settings_incomplete")
		return
	}
	name := fmt.Sprintf("model-%d", uniqueID())
	if w.tokenEnv != "" {
		w.execute("inferyard model acquire --source "+quote(w.modelURL)+" --out "+quote(root+"/"+name)+" --token-env "+quote(w.tokenEnv), false)
		return
	}
	source := w.modelURL
	w.work(func() (string, *core.ApiError) {
		_, e := w.service.CreateDownload(core.CreateDownloadRequest{SourceUrl: source, OutName: name})
		return "已加入下载队列。", e
	})
}
func (w *viewState) candidateView(c *ui.Context) {
	ui.Text(c, "创建当前题包候选").Bold().FontSize(22)
	if b := w.active(); b != nil {
		ui.Text(c, b.Bundle+" · 将冻结模型、引擎与运行预算。")
	}
	field(c, "引擎文件绝对路径", &w.engine)
	if ui.PrimaryButton(c, "创建候选").Disabled(w.busy || w.engine == "").Clicked() {
		w.execute(w.nextCommand(), false)
	}
}
func (w *viewState) serviceView(c *ui.Context) {
	ui.Text(c, "在外部终端启动模型服务").Bold().FontSize(22)
	ui.Text(c, "以下命令来自冻结候选。请由操作者启动；Studio 不代启、不代停。")
	command := commandOf(append([]string{w.startup.Path}, w.startup.Args...))
	// Engine commands have no inferyard prefix; the display is selectable, never editable.
	command = command[len("inferyard "):]
	ui.Text(c, command).Font("monospace").Selectable()
	if ui.Button(c, "复制启动命令").Disabled(w.startup.Path == "").Clicked() {
		c.WriteClipboard(command)
		c.Toast("已复制，请在外部终端执行。")
	}
	if !w.binding {
		if ui.PrimaryButton(c, "我已启动，继续绑定").Clicked() {
			w.binding = true
			go w.refresh()
		}
		return
	}
	options := []string{}
	for _, p := range w.processes {
		options = append(options, fmt.Sprintf("%d · %s", p.PID, p.Name))
	}
	ui.Field(c, "已启动的本机进程", func() { ui.Select(c, &w.process, options) })
	field(c, "endpoint", &w.endpoint)
	ui.Text(c, "绑定时 CLI 校验 PID、启动时间与实际参数。")
	if ui.PrimaryButton(c, "绑定进程").Disabled(w.busy || w.process == "" || w.endpoint == "").Clicked() {
		w.execute(w.nextCommand(), false)
	}
}
func (w *viewState) runView(c *ui.Context) {
	ui.Text(c, "运行当前题包").Bold().FontSize(22)
	if task := w.running(); task != nil {
		value := math.NaN()
		label := "正在运行，等待进度…"
		if p := w.runProgress; p != nil && p.Total != nil && *p.Total > 0 {
			value = float64(p.Done) / float64(*p.Total)
			label = fmt.Sprintf("%d / %d 题", p.Done, *p.Total)
		}
		ui.Progress(c, value)
		ui.Text(c, label)
		id := task.TaskId
		if ui.Button(c, "取消运行").Disabled(w.busy || task.State == "cancelling").Clicked() {
			w.work(func() (string, *core.ApiError) {
				_, e := w.service.CancelRun(id)
				return "已请求取消，等待安全收尾。", e
			})
		}
		return
	}
	ui.Text(c, "运行前请核对设备、预算与停止条件。完成封存后自动进入下一题包。")
	for _, task := range w.tasks {
		if task.State == "failed" || task.State == "cancelled" {
			ui.Text(c, "上次运行未完成，请检查停止原因后重试。")
			break
		}
	}
	label := "开始运行"
	if b := w.active(); b != nil && b.NextAction == "resume" {
		label = "继续未完成的运行"
	}
	if ui.PrimaryButton(c, label).Disabled(w.busy).Clicked() {
		w.execute(w.nextCommand(), false)
	}
}
func (w *viewState) reportView(c *ui.Context) {
	ui.Text(c, "生成并核验两题包报告").Bold().FontSize(22)
	ui.Text(c, "汇总已封存的运行，核验报告字节、来源与语义。")
	if ui.PrimaryButton(c, "生成报告并核验").Disabled(w.busy).Clicked() {
		w.execute(w.nextCommand(), true)
	}
}
func (w *viewState) done(c *ui.Context) {
	ui.Text(c, "完成 · 两题包报告已核验").Bold().FontSize(22).TextColor(c.Theme().Success)
	ui.Badge(c, "已核验 verified")
	ui.Text(c, "zh-core · 已封存\nzh-svg-pelican · 已封存")
	if ui.PrimaryButton(c, "在主窗口打开报告").Clicked() {
		w.openMain("/reports")
	}
}
