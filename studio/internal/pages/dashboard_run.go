package pages

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"path/filepath"
	"strings"
	"time"
)

func (w *viewState) candidateView(c *ui.Context) {
	w.verifyTaskEngines()
	heading(c, "benchmark", 3)
	w.adoptDownloadedEngine()

	modelOptions := []components.NativeSelectOption[string]{{Value: "", Label: "使用 CLI 推荐模型"}}
	for _, m := range w.registry.Models {
		if m.Available {
			modelOptions = append(modelOptions, components.NativeSelectOption[string]{Value: m.Name, Label: m.Name})
		}
	}

	// The engine dropdown lists verified engines only; while the CLI is too
	// old to verify, every engine stays listed and config create is the final
	// gate. A selected engine revoked by re-verification stays visible as a
	// disabled option next to the warning.
	eligible := []string{}
	for _, e := range w.registry.Engines {
		el := w.eligibility(e.Name)
		if el.State == core.EngineCheckEligible || (!w.checks.Supported && el.State != core.EngineCheckIneligible) {
			eligible = append(eligible, e.Name)
		}
	}
	engineOptions := []components.NativeSelectOption[string]{}
	listed := false
	for _, name := range eligible {
		engineOptions = append(engineOptions, components.NativeSelectOption[string]{Value: name, Label: name})
		if name == w.engine {
			listed = true
		}
	}
	revoked := false
	if w.engine != "" && !listed {
		el := w.eligibility(w.engine)
		suffix := " · 核验中…"
		if el.State == core.EngineCheckIneligible {
			suffix = " · 不可用，请重新选择"
			revoked = true
		}
		engineOptions = append(engineOptions, components.NativeSelectOption[string]{Value: w.engine, Label: w.engine + suffix, Disabled: true})
	}
	ui.Field(c, "候选引擎", func() { components.NativeSelect(c, &w.engine, engineOptions) }).Description("仅列出核验可用的引擎；默认选中其中最新下载的引擎。")
	ui.Field(c, "测评模型", func() { components.NativeSelect(c, &w.model, modelOptions) }).Description("与模型页的选择框是同一选择。")
	if w.checks.Checking > 0 {
		ui.Text(c, fmt.Sprintf("正在核验 %d 个引擎…", w.checks.Checking)).TextColor(c.Theme().TextMuted)
	}

	summary := "测评模型：" + configuredModel(w.model)
	for _, m := range w.registry.Models {
		if m.Name != w.model {
			continue
		}
		parts := []string{}
		if m.Architecture != "" {
			parts = append(parts, "架构 "+m.Architecture)
		}
		if m.SizeBytes != nil {
			parts = append(parts, fmt.Sprintf("约 %.1f GiB", float64(*m.SizeBytes)/(1<<30)))
		}
		if len(parts) > 0 {
			summary += "（" + strings.Join(parts, " · ") + "）"
		}
	}
	ui.Text(c, summary)
	ui.Text(c, "引擎版本已核验；模型架构与引擎的适配在启动时验证。").TextColor(c.Theme().TextMuted)

	w.bundleSelection(c)
	createDisabled := w.environmentMissing() || w.progress == nil || w.progress.DevicePreflight.Status != "ok" || w.busy || w.engine == "" || w.model == "" || revoked || len(w.checkedBundles()) == 0 || w.engineState.State == "orphaned"
	if revoked {
		el := w.eligibility(w.engine)
		ui.Row(c).Wrap().Gap(c.Theme().Space(2)).Children(func() {
			statusBadge(c, "当前选择不可用", c.Theme().Warning)
			ui.Text(c, w.engine+" · "+human(el.Reason)+"。请重新选择可用引擎。")
		})
		button(c, "到模型页重选").AlignSelf(ui.Start).OnClick(func() { w.selected = "models" })
		createDisabled = true
	} else if len(eligible) == 0 {
		// 核验完成前不误报无可用引擎；创建保持禁用。
		if w.checks.Checking == 0 {
			ui.Text(c, "llama-server")
			ui.Text(c, "没有可用的引擎，请到下载页下载推荐引擎。")
			button(c, "到下载页").AlignSelf(ui.Start).OnClick(func() { w.selected = "downloads" })
		}
		createDisabled = true
	}
	if w.model == "" {
		ui.Text(c, "SmolLM2-135M-Instruct-Q4_K_M")
		button(c, "下载模型").OnClick(func() { w.selected = "downloads" })
	}
	if w.engineState.State == "orphaned" {
		w.stopOrphanButton(c)
	}
	if primaryButton(c, "运行 benchmark").AlignSelf(ui.Start).Disabled(createDisabled).Clicked() {
		w.startBenchmark()
	}
}
func (w *viewState) serviceView(c *ui.Context) {
	heading(c, "启动模型服务并绑定", 3)
	if w.bindingStale {
		ui.Text(c, "旧绑定已失效，请重启服务并重新绑定").TextColor(c.Theme().Danger)
	}
	ui.Text(c, "Studio 启动冻结候选中的引擎并管理其生命周期；退出 Studio 时自动停止。绑定时 CLI 校验 PID、启动时间与实际参数。")
	switch w.engineState.State {
	case "orphaned":
		ui.Text(c, "遗留引擎：请先处理上次 Studio 留下的服务或无效记录。").TextColor(c.Theme().Warning)
		errorView(c, w.engineState.Error)
		w.stopOrphanButton(c)
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
	candidateReady := w.candidate != "" || w.task.start.candidate != ""
	if !candidateReady {
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
		label := "启动服务"
		if startButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || !candidateReady || !startable || w.running() != nil).Clicked() {
			w.startEngine()
		}
		if w.engineState.State == "running" && w.failure != nil {
			label := "重新绑定失败的题包"
			if b := w.active(); b != nil && b.BoundPath != nil {
				label = "重新绑定"
			}
			if primaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil).Clicked() {
				if w.task.stage == "bind" {
					w.retryBenchmark()
				} else if w.bindingPhase() {
					w.bindDiscovered(w.engineState.Pid, w.engineState.Endpoint)
				} else {
					w.executeBinding(w.managedBindCommand())
				}
			}
		}
		if w.engineState.State == "starting" || w.engineState.State == "running" {
			if button(c, "停止服务").AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil).Clicked() {
				w.stopEngine()
			}
		}
	})

	ui.Text(c, "健康检查通过后按顺序绑定全部待绑题包。任一失败即停止，不自动重试。")
	if w.binding {
		ui.Text(c, displayName(w.bindingBundle)+" · 绑定中…")
	}
	if w.failure == nil {
		return
	}
	w.discoveryView(c, true)
	componentFold(c, "高级：手动绑定", func() {
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
			w.executeBinding(w.bindCommand(pid, w.bindEndpoint))
		}
	})
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
		if button(c, "取消运行").AlignSelf(ui.Start).Disabled((w.busy && w.task.stage == "") || task.State == "cancelling").Clicked() {
			if w.task.stage != "" {
				w.newBenchmark()
				return
			}
			if w.simulate(previewCancelRun{id: id}) {
				return
			}
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
	b := w.active()
	if b == nil || b.NextAction == "none" {
		return
	}
	if b.BlockedReason != nil {
		ui.Text(c, human(*b.BlockedReason))
	}
	if blockedPlan(*b) {
		return
	}
	label := "开始运行"
	if b.NextAction == "resume" {
		label = "继续未完成的运行"
	}
	if primaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || w.engineState.State != "running").Clicked() {
		if w.simulate(previewStartRun{bundle: b.Bundle}) {
			return
		}
		bundle := *b
		ctx, cancel := context.WithCancel(w.ctx)
		w.taskCancel = cancel
		state, workspace := w.engineState, w.progress.Workspace
		w.guideWork(func() (string, *core.ApiError) {
			defer cancel()
			e := executeExistingBundle(ctx, bundle, workspace, state, w.service.WizardExecute)
			return "运行已加入队列。", e
		})
	}

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

func configuredModel(name string) string {
	if name == "" {
		return "使用 CLI 推荐模型"
	}
	return name
}

func (s *viewState) bundleSelection(c *ui.Context) {
	s.checkedBundles()
	if s.progress == nil {
		return
	}
	for _, b := range s.progress.Bundles {
		checked := s.selectedBundles[b.Bundle]
		if components.Checkbox(c, &checked, b.Bundle).Changed() {
			s.selectedBundles[b.Bundle] = checked
		}
	}
	if len(s.checkedBundles()) == 0 {
		ui.Text(c, "至少选择一个题包。")
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
	if s.service == nil || s.task.stage != "prepare_candidate" || s.busy || s.refreshing {
		return
	}
	now := time.Now()
	if !s.task.lastVerify.IsZero() && now.Sub(s.task.lastVerify) < 5*time.Second {
		return
	}
	s.task.lastVerify = now
	go func() {
		s.service.VerifyEngines()
		if s.ctx.Err() != nil {
			return
		}
		s.win.Update(func() { s.checks = s.service.EngineChecks(); s.adoptDownloadedEngine() })
	}()
}
