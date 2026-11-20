package pages

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"strings"
	"time"
)

func (w *viewState) engineChoices() ([]components.NativeSelectOption[string], bool, bool) {
	eligible := []string{}
	for _, e := range w.registry.Engines {
		el := w.eligibility(e.Name)
		if el.State == core.EngineCheckEligible || (!w.checks.Supported && el.State != core.EngineCheckIneligible) {
			eligible = append(eligible, e.Name)
		}
	}
	options := []components.NativeSelectOption[string]{}
	listed := false
	for _, name := range eligible {
		options = append(options, components.NativeSelectOption[string]{Value: name, Label: name})
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
		options = append(options, components.NativeSelectOption[string]{Value: w.engine, Label: w.engine + suffix, Disabled: true})
	}
	return options, revoked, len(eligible) > 0
}

func (w *viewState) candidateView(c *ui.Context) {
	w.speedTypeSwitch(c)
	w.candidateFields(c)
}

func (w *viewState) candidateFields(c *ui.Context) {
	w.verifyTaskEngines()
	w.adoptDownloadedEngine()
	w.adoptOnlyModel()

	modelOptions := []components.NativeSelectOption[string]{{Value: "", Label: "请选择模型"}}
	for _, m := range w.registry.Models {
		if m.Available {
			modelOptions = append(modelOptions, components.NativeSelectOption[string]{Value: m.Name, Label: modelLabel(m), Disabled: !w.compatibleModel(m)})
		}
	}

	engineOptions, revoked, hasEligible := w.engineChoices()
	engineHint := ""
	if !w.checks.Supported {
		engineHint = "当前 CLI 不支持引擎核验，列表中的引擎尚未核验。"
	}
	selectField(c, termCandidateEngine, engineHint, &w.engine, engineOptions)
	selectField(c, "测评模型", "", &w.model, modelOptions)
	if w.checks.Checking > 0 {
		ui.Text(c, fmt.Sprintf("正在核验 %d 个引擎…", w.checks.Checking)).TextColor(c.Theme().TextMuted)
	}

	if meta := w.modelMetadata(w.model); meta != "" {
		ui.Text(c, meta).TextColor(c.Theme().TextMuted)
	}

	if w.speedSelection(c) {
		return
	}
	w.bundleSelection(c)
	w.localSelectionView(c)
	createDisabled := w.environmentMissing() || !w.preflightReady() || w.busy || w.engine == "" || w.model == "" || revoked || len(w.checkedBundles()) == 0 || w.engineState.State == "orphaned" || w.localSelectionError() != ""
	if revoked {
		el := w.eligibility(w.engine)
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			statusBadge(c, "当前选择不可用", stateColor(c, stateWarning))
			ui.Text(c, w.engine+" · "+human(el.Reason)+"，请在上方重新选择。")
		})
		button(c, "查看已安装引擎").AlignSelf(ui.Start).OnClick(func() { w.selected = "models" })
		createDisabled = true
	} else if !hasEligible {
		if w.checks.Checking == 0 {
			ui.Text(c, "暂无可用引擎。")
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
				button(c, "到下载页").OnClick(func() { w.selected = "downloads" })
				button(c, "注册已有引擎").OnClick(func() { w.selected = "settings" })
			})
		}
		createDisabled = true
	}
	if w.model == "" && len(modelOptions) == 1 {
		ui.Text(c, "暂无可用模型。")
		button(c, "下载模型").OnClick(func() { w.selected = "downloads" })
	}
	if w.engineState.State == "orphaned" {
		w.stopOrphanButton(c)
	}
	if primaryButton(c, termStartBenchmark).AlignSelf(ui.Start).Disabled(createDisabled).Clicked() {
		w.startBenchmark()
	}
}

func failureView(c *ui.Context, title string, e *core.ApiError, next string) {
	ui.Text(c, title).Bold().TextColor(stateColor(c, stateDanger))
	if e != nil {
		errorView(c, e)
	} else {
		ui.Text(c, "未返回原因码。").TextColor(stateColor(c, stateDanger))
	}
	if next != "" {
		ui.Text(c, next).TextColor(c.Theme().TextMuted)
	}
}

func (w *viewState) serviceView(c *ui.Context) {
	heading(c, "启动模型服务并绑定", 3)
	if w.bindingStale {
		ui.Text(c, "旧绑定已失效，请重启服务并重新绑定").TextColor(stateColor(c, stateDanger))
	}
	ui.Text(c, "由 Studio 托管模型服务，退出时自动停止。").TextColor(c.Theme().TextMuted)
	if w.stepFailed() {
		title := "模型服务失败"
		if w.step() == 3 {
			title = "连接并核验服务失败"
		}
		failureView(c, title, w.activeTaskFailure(),
			"可重新启动服务；若进程已在本机运行，请展开「连接已有本机服务」选择，或用「高级：手动连接」填写 PID 与地址。")
	}
	switch w.engineState.State {
	case "orphaned":
		ui.Text(c, "遗留引擎：请先处理上次 Studio 留下的服务或无效记录。").TextColor(stateColor(c, stateWarning))
		errorView(c, w.engineState.Error)
		w.stopOrphanButton(c)
	case "starting":
		ui.Text(c, fmt.Sprintf("正在启动（PID %d）——模型加载可能需要几分钟…", w.engineState.Pid))
		progressBar(c, -1, "模型服务启动进度")
	case "running":
		ui.Text(c, fmt.Sprintf("运行中 · PID %d · %s", w.engineState.Pid, w.engineState.Endpoint)).TextColor(stateColor(c, stateSuccess))
	case "stopping":
		ui.Text(c, "正在停止服务…")
	}
	candidateReady := w.candidate != "" || w.task.start.candidate != ""
	if !candidateReady {
		ui.Text(c, "请先创建测评配置，再启动服务。")
	}
	if w.running() != nil {
		ui.Text(c, "测评运行期间不能停止或重新连接服务。")
	}
	startable := w.engineState.State == "" || w.engineState.State == "stopped" || w.engineState.State == "failed"
	ui.Row(c).Wrap().Gap(tokens(c).Density.CardGap).Children(func() {
		startButton := button
		if startable {
			startButton = primaryButton
		}
		label := "启动服务"
		if startButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || !candidateReady || !startable || w.running() != nil).Clicked() {
			w.startEngine()
		}
		if w.engineState.State == "running" && w.failure != nil {
			label := "重新连接失败的测评题集"
			if b := w.bindTarget(); b != nil && b.BoundPath != nil {
				label = "重新连接"
			}
			if primaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil).Clicked() {
				if w.task.stage == "bind" {
					w.retryBenchmark()
				} else if w.bindingPhase() {
					w.bindDiscovered(w.engineState.Pid, w.engineState.Endpoint)
				} else {
					target := w.bindTargetBundle()
					command, e := w.managedBindCommand()
					w.executeBindingFor(target, command, e)
				}
			}
		}
		if w.engineState.State == "starting" || w.engineState.State == "running" {
			if w.stopEngineGate(c) {
				w.stopEngine()
			}
		}
	})

	if w.binding {
		ui.Text(c, displayName(w.bindingBundle)+" · 绑定中…")
	}
	componentFoldKeyed(c, "bind-discovered-services", "连接已有本机服务", func() { w.discoveryBody(c, true) })
	componentFold(c, "高级：手动连接", func() {
		heading(c, "连接已有进程", 3)
		options := []string{}
		for _, p := range w.processes {
			options = append(options, fmt.Sprintf("%d · %s", p.PID, p.Name))
		}
		selectField(c, "已启动的本机进程", "", &w.process, nativeOptions(options))
		field(c, "服务地址", &w.bindEndpoint).Description("本机 http 或 https URL").Error(urlError(w.bindEndpoint))
		if button(c, "连接并核验进程").AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil || w.process == "" || w.bindEndpoint == "").Clicked() {
			if message := urlError(w.bindEndpoint); message != "" {
				w.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
				return
			}
			pid, _, _ := strings.Cut(w.process, " · ")
			target := w.bindTargetBundle()
			command, e := w.bindCommand(pid, w.bindEndpoint)
			w.executeBindingFor(target, command, e)
		}
	})
}
func (w *viewState) runView(c *ui.Context) {
	heading(c, "运行当前测评题集", 3)
	if w.stepFailed() && w.running() == nil && !w.serviceCardVisible() {
		failureView(c, "运行未完成", w.activeTaskFailure(), "处理原因后重试未完成的测评题集；已完成的题集结果会保留。")
	}
	if task := w.running(); task != nil {
		value := -1.0
		label := "正在运行，等待进度…（总量未知）"
		if p := w.runProgress; p != nil && p.Total != nil && *p.Total > 0 {
			value = progressValue(p.Done, p.Total)
			label = fmt.Sprintf("%d / %d 题", p.Done, *p.Total)
		}
		progressBar(c, value, "当前测评题集运行进度")
		if task.State == "cancelling" {
			ui.Text(c, "取消中／等待安全收尾。")
		}
		ui.Text(c, label)
		id := task.TaskId
		ui.Row(c).Wrap().Gap(tokens(c).Density.Row).Children(func() { button(c, "查看实时详情").AlignSelf(ui.Start).OnClick(func() { w.selectTask(id) }) })
		if w.cancelRunGate(c, id, task.State) {
			if w.task.stage != "" {
				w.newBenchmark()
				return
			}
			if w.simulate(previewCancelRun{id: id}) {
				return
			}
			w.guideWork(func() (string, *core.ApiError) {
				_, e := w.cancelRunTask(id)
				return "已请求取消，等待安全收尾。", e
			})
		}
		return
	}
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
	if primaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || !w.runServiceReady(*b)).Clicked() {
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

func (w *viewState) runServiceReady(b inferyard.BundleProgress) bool {
	if w.engineState.State == "running" {
		return true
	}
	return b.NextAction == "run" && b.BoundPath != nil
}

func nativeOptions(values []string) []components.NativeSelectOption[string] {
	options := make([]components.NativeSelectOption[string], len(values))
	for i, value := range values {
		options[i] = components.NativeSelectOption[string]{Value: value, Label: value}
	}
	return options
}

func (w *viewState) modelMetadata(name string) string {
	for _, m := range w.registry.Models {
		if m.Name != name {
			continue
		}
		parts := []string{}
		if m.Architecture != "" {
			parts = append(parts, "架构 "+m.Architecture)
		}
		if m.SizeBytes != nil {
			parts = append(parts, fmt.Sprintf("约 %.1f GiB", float64(*m.SizeBytes)/(1<<30)))
		}
		return strings.Join(parts, " · ")
	}
	return ""
}

func bundleDescription(bundle string) string {
	switch bundle {
	case inferyard.DefaultBundleCore:
		return "中文基础能力"
	case inferyard.DefaultBundlePelican:
		return "SVG 绘图专项"
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
	ui.Field(c, termBundle, func() {
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
		ui.Text(c, "至少选择一个测评题集。")
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
