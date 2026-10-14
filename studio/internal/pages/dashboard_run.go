package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"path/filepath"
	"strings"
)

func (w *viewState) candidateView(c *ui.Context) {
	heading(c, "创建当前题包候选", 3)
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
	field(c, "引擎文件绝对路径", &w.engine).Description("必填；从模型与下载页获取引擎后自动填入。").Error(pathError(w.engine))
	if primaryButton(c, "创建候选").AlignSelf(ui.Start).Disabled(w.busy || w.engine == "").Clicked() {
		w.execute(w.nextCommand(), false)
	}
}
func (w *viewState) serviceView(c *ui.Context) {
	heading(c, "启动模型服务并绑定", 3)
	if w.bindingStale {
		ui.Text(c, "旧绑定已失效，请重启服务并重新绑定").TextColor(c.Theme().Danger)
	}
	ui.Text(c, "Studio 启动冻结候选中的引擎并管理其生命周期；退出 Studio 时自动停止。绑定时 CLI 校验 PID、启动时间与实际参数。")
	command := commandOf(append([]string{w.startup.Path}, w.startup.Args...))
	// Engine commands have no inferyard prefix; the display is selectable, never editable.
	command = command[len("inferyard "):]
	if w.startup.Path == "" {
		command = "未配置：请先创建候选。"
	}
	ui.Text(c, command).Font("monospace").Selectable()
	if button(c, "复制启动命令").AlignSelf(ui.Start).Disabled(w.startup.Path == "").Clicked() {
		c.WriteClipboard(command)
		c.Toast("已复制。")
	}
	switch w.engineState.State {
	case "starting":
		ui.Text(c, fmt.Sprintf("正在启动（PID %d）——模型加载可能需要几分钟…", w.engineState.Pid))
		progressBar(c, -1, "模型服务启动进度")
	case "running":
		ui.Text(c, fmt.Sprintf("运行中 · PID %d · %s", w.engineState.Pid, w.engineState.Endpoint)).TextColor(c.Theme().Success)
	case "stopping":
		ui.Text(c, "正在停止服务…")
	case "failed":
		if w.engineState.Error != nil {
			ui.Text(c, w.engineState.Error.Message).TextColor(c.Theme().Danger)
		}
	}
	if w.candidate == "" {
		ui.Text(c, "请先创建候选，再启动服务。")
	}
	if w.running() != nil {
		ui.Text(c, "测评运行期间不能停止或重新绑定服务。")
	}
	startable := w.engineState.State == "" || w.engineState.State == "stopped" || w.engineState.State == "failed"
	ui.Row(c).Wrap().Gap(c.Theme().Space(2.5)).Children(func() {
		startButton := button
		if startable {
			startButton = primaryButton
		}
		if startButton(c, "启动服务").AlignSelf(ui.Start).Disabled(w.busy || w.candidate == "" || !startable).Clicked() {
			w.startEngine()
		}
		if w.engineState.State == "running" {
			label := "绑定服务"
			if b := w.active(); b != nil && b.BoundPath != nil {
				label = "重新绑定"
			}
			if primaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil).Clicked() {
				w.execute(w.managedBindCommand(), false)
			}
		}
		if w.engineState.State == "starting" || w.engineState.State == "running" {
			if button(c, "停止服务").AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil).Clicked() {
				w.stopEngine()
			}
		}
	})

	heading(c, "绑定已有进程", 3)
	options := []string{}
	for _, p := range w.processes {
		options = append(options, fmt.Sprintf("%d · %s", p.PID, p.Name))
	}
	ui.Field(c, "已启动的本机进程", func() { components.NativeSelect(c, &w.process, nativeOptions(options)) }).Description("请选择已启动的服务进程；绑定仍会核验身份。")
	field(c, "endpoint", &w.bindEndpoint).Description("必填；本机服务的 http 或 https URL。").Error(urlError(w.bindEndpoint))
	if button(c, "绑定进程").AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil || w.process == "" || w.bindEndpoint == "").Clicked() {
		if message := urlError(w.bindEndpoint); message != "" {
			w.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
			return
		}
		pid, _, _ := strings.Cut(w.process, " · ")
		w.execute(w.bindCommand(pid, w.bindEndpoint), false)
	}
}
func (w *viewState) runView(c *ui.Context) {
	heading(c, "运行当前题包", 3)
	if task := w.running(); task != nil {
		value := -1.0
		label := "正在运行，等待进度…（总量未知）"
		if p := w.runProgress; p != nil && p.Total != nil && *p.Total > 0 {
			value = progressValue(p.Done, p.Total)
			label = fmt.Sprintf("%d / %d 题", p.Done, *p.Total)
		}
		progressBar(c, value, "当前题包运行进度")
		ui.Text(c, "分母为冻结选中题集合；续测时为冻结续测子集。")
		if task.State == "cancelling" {
			ui.Text(c, "取消中／等待安全收尾。")
		}
		ui.Text(c, label)
		id := task.TaskId
		button(c, "查看实时详情").AlignSelf(ui.Start).OnClick(func() { w.selectTask(id) })
		if button(c, "取消运行").AlignSelf(ui.Start).Disabled(w.busy || task.State == "cancelling").Clicked() {
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
	if primaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy).Clicked() {
		w.execute(w.nextCommand(), false)
	}
}
func (w *viewState) reportView(c *ui.Context) {
	heading(c, "生成并核验两题包报告", 3)
	ui.Text(c, "汇总已封存的运行，核验报告字节、来源与语义。")
	if primaryButton(c, "生成报告并核验").AlignSelf(ui.Start).Disabled(w.busy).Clicked() {
		w.execute(w.nextCommand(), true)
	}
}

func (s *viewState) quickRunView(c *ui.Context) {
	card(c, "启动测评", func() {
		options := s.boundConfigs()
		if len(options) == 0 {
			ui.Text(c, "暂无已绑定题包；可在下方高级选项手动指定配置路径")
		}
		names := []string{}
		for _, o := range options {
			names = append(names, o.bundle)
		}
		if len(names) > 0 {
			s.firstBundle = defaultBundle(s.firstBundle, names, "")
			twoBound := len(names) >= 2
			if s.single || !twoBound {
				ui.Field(c, "题包", func() { components.NativeSelect(c, &s.firstBundle, nativeOptions(names)) })
				ui.Text(c, configOf(options, s.firstBundle)).Selectable()
			} else {
				s.secondBundle = defaultBundle(s.secondBundle, names, s.firstBundle)
				ui.Field(c, "第一题包", func() { components.NativeSelect(c, &s.firstBundle, nativeOptions(names)) })
				ui.Text(c, configOf(options, s.firstBundle)).Selectable()
				ui.Field(c, "第二题包", func() { components.NativeSelect(c, &s.secondBundle, nativeOptions(names)) })
				ui.Text(c, configOf(options, s.secondBundle)).Selectable()
			}
		}
		components.Checkbox(c, &s.manualConfig, "高级：手动配置路径")
		if s.manualConfig {
			field(c, "第一配置绝对路径", &s.manualFirst).Description("已绑定的配置文件绝对路径。").Error(pathError(s.manualFirst))
			if !s.single {
				field(c, "第二配置绝对路径", &s.manualSecond).Description("默认双题包需要不同的第二份绑定配置。").Error(pathError(s.manualSecond))
			}
		}
		first, second := configOf(options, s.firstBundle), configOf(options, s.secondBundle)
		if s.manualConfig {
			first, second = strings.TrimSpace(s.manualFirst), strings.TrimSpace(s.manualSecond)
		}
		twoBound := first != "" && second != ""
		if components.Checkbox(c, &s.single, "仅运行一个题包").Changed() {
			s.confirmed = false
		}
		if s.single {
			components.Checkbox(c, &s.confirmed, "我确认只运行一个题包")
		}
		ui.Text(c, "默认双题包，按配置顺序串行运行。运行前请核对设备、预算与停止条件。")
		if !twoBound && !s.single {
			ui.Text(c, "第二题包尚未绑定，双题包运行不可用。")
		}
		if s.running() != nil {
			ui.Text(c, "当前运行结束后可启动新的测评。")
		}
		if s.single && !s.confirmed {
			ui.Text(c, "请显式确认只运行一个题包。")
		}
		duplicate := first == second
		if !s.single && twoBound && duplicate {
			ui.Text(c, "两个题包不能相同。").TextColor(c.Theme().Danger)
		}
		if primaryButton(c, "启动运行").AlignSelf(ui.Start).Disabled(s.busy || s.running() != nil || first == "" || (!s.single && (!twoBound || duplicate)) || (s.single && !s.confirmed)).Clicked() {
			req := core.StartRunsRequest{ConfigPaths: []string{first}}
			if s.single {
				confirmed := true
				req.AllowSingleBundle = &confirmed
			} else {
				req.ConfigPaths = append(req.ConfigPaths, second)
			}
			s.work(func() (string, *core.ApiError) {
				tasks, e := s.service.StartRuns(req)
				if e == nil {
					ids := []string{}
					for _, t := range tasks {
						ids = append(ids, t.TaskId)
					}
					s.win.Update(func() { s.groups = append(s.groups, ids) })
				}
				return "运行已加入队列。", e
			})
		}
	})
}

// boundOption maps a bound bundle to the frozen config written by `config bind`.
type boundOption struct{ bundle, config string }

func (s *viewState) boundConfigs() []boundOption {
	options := []boundOption{}
	if s.progress == nil {
		return options
	}
	for _, b := range s.progress.Bundles {
		if b.BoundPath != nil {
			options = append(options, boundOption{bundle: b.Bundle, config: filepath.Join(*b.BoundPath, "config.toml")})
		}
	}
	return options
}

// defaultBundle keeps the current selection when still valid, otherwise picks
// the first option that is not exclude.
func defaultBundle(current string, names []string, exclude string) string {
	for _, n := range names {
		if n == current {
			return current
		}
	}
	for _, n := range names {
		if n != exclude {
			return n
		}
	}
	return names[0]
}

func configOf(options []boundOption, bundle string) string {
	for _, o := range options {
		if o.bundle == bundle {
			return o.config
		}
	}
	return ""
}

func nativeOptions(values []string) []components.NativeSelectOption[string] {
	options := make([]components.NativeSelectOption[string], len(values))
	for i, value := range values {
		options[i] = components.NativeSelectOption[string]{Value: value, Label: value}
	}
	return options
}
