package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"math"
	"strings"
)

func (w *viewState) candidateView(c *ui.Context) {
	ui.Text(c, "创建当前题包候选").Bold().FontSize(22)
	if w.progress != nil {
		for _, b := range w.progress.Bundles {
			if b.Phase == "run_complete" {
				ui.Text(c, b.Bundle+" 已封存。剩余题包按同样流程进行：候选 → 启动与绑定 → 运行。").TextColor(c.Theme().Success)
				break
			}
		}
	}
	if b := w.active(); b != nil {
		ui.Text(c, b.Bundle+" · 将冻结模型、引擎与运行预算。")
	}
	field(c, "引擎文件绝对路径", &w.engine)
	if ui.PrimaryButton(c, "创建候选").AlignSelf(ui.Start).Disabled(w.busy || w.engine == "").Clicked() {
		w.execute(w.nextCommand(), false)
	}
}
func (w *viewState) serviceView(c *ui.Context) {
	ui.Text(c, "启动模型服务并绑定").Bold().FontSize(22)
	if w.bindingStale {
		ui.Text(c, "旧绑定已失效，请重启服务并重新绑定").TextColor(c.Theme().Danger)
	}
	ui.Text(c, "Studio 启动冻结候选中的引擎并管理其生命周期；退出 Studio 时自动停止。绑定时 CLI 校验 PID、启动时间与实际参数。")
	command := commandOf(append([]string{w.startup.Path}, w.startup.Args...))
	// Engine commands have no inferyard prefix; the display is selectable, never editable.
	command = command[len("inferyard "):]
	ui.Text(c, command).Font("monospace").Selectable()
	if ui.Button(c, "复制启动命令").AlignSelf(ui.Start).Disabled(w.startup.Path == "").Clicked() {
		c.WriteClipboard(command)
		c.Toast("已复制。")
	}
	switch w.engineState.State {
	case "starting":
		ui.Text(c, fmt.Sprintf("正在启动（PID %d）——模型加载可能需要几分钟…", w.engineState.Pid))
		ui.Progress(c, math.NaN())
	case "running":
		ui.Text(c, fmt.Sprintf("运行中 · PID %d · %s", w.engineState.Pid, w.engineState.Endpoint)).TextColor(c.Theme().Success)
	case "stopping":
		ui.Text(c, "正在停止服务…")
	case "failed":
		if w.engineState.Error != nil {
			ui.Text(c, w.engineState.Error.Message).TextColor(c.Theme().Danger)
		}
	}
	startable := w.engineState.State == "" || w.engineState.State == "stopped" || w.engineState.State == "failed"
	ui.Row(c).Gap(10).Children(func() {
		if ui.PrimaryButton(c, "启动服务").AlignSelf(ui.Start).Disabled(w.busy || w.candidate == "" || !startable).Clicked() {
			w.startEngine()
		}
		if w.engineState.State == "running" {
			label := "绑定服务"
			if b := w.active(); b != nil && b.BoundPath != nil {
				label = "重新绑定"
			}
			if ui.PrimaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil).Clicked() {
				w.execute(w.managedBindCommand(), false)
			}
		}
		if w.engineState.State == "starting" || w.engineState.State == "running" {
			if ui.Button(c, "停止服务").AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil).Clicked() {
				w.stopEngine()
			}
		}
	})

	ui.Text(c, "绑定已有进程")
	options := []string{}
	for _, p := range w.processes {
		options = append(options, fmt.Sprintf("%d · %s", p.PID, p.Name))
	}
	ui.Field(c, "已启动的本机进程", func() { ui.Select(c, &w.process, options) })
	field(c, "endpoint", &w.bindEndpoint)
	if ui.PrimaryButton(c, "绑定进程").AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil || w.process == "" || w.bindEndpoint == "").Clicked() {
		pid, _, _ := strings.Cut(w.process, " · ")
		w.execute(w.bindCommand(pid, w.bindEndpoint), false)
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
		if ui.Button(c, "取消运行").AlignSelf(ui.Start).Disabled(w.busy || task.State == "cancelling").Clicked() {
			w.guideWork(func() (string, *core.ApiError) {
				_, e := w.service.CancelRun(id)
				return "已请求取消，等待安全收尾。", e
			})
		}
		return
	}
	ui.Text(c, "运行前请核对设备、预算与停止条件。完成封存后进入下一步；有剩余题包时需同样创建候选并绑定。")
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
	if ui.PrimaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy).Clicked() {
		w.execute(w.nextCommand(), false)
	}
}
func (w *viewState) reportView(c *ui.Context) {
	ui.Text(c, "生成并核验两题包报告").Bold().FontSize(22)
	ui.Text(c, "汇总已封存的运行，核验报告字节、来源与语义。")
	if ui.PrimaryButton(c, "生成报告并核验").AlignSelf(ui.Start).Disabled(w.busy).Clicked() {
		w.execute(w.nextCommand(), true)
	}
}
