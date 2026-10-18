package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type speedStage uint8

const (
	speedPreparing speedStage = iota
	speedBinding
	speedPlanning
	speedMeasuring
	speedReading
	speedDone
	speedCancelling
	speedCancelled
	speedFailed
	speedPartial
	speedQueued
)

type speedSample struct {
	context                  *int
	prompt                   *int
	seconds, prefill, decode *float64
	reason                   string
	missing                  string
}

type speedTask struct {
	selectionModel, selectionEngine string
	result                          *inferyard.ContextSpeedResult
	completed                       int
	budget                          float64
	id, model, engine               string
	context                         int
	stage                           speedStage
	samples                         []speedSample
	summary                         *speedSample
	reason                          string
}

func (r *speedTask) active() bool {
	return r.stage <= speedReading || r.stage == speedCancelling || r.stage == speedQueued
}

func (r *speedTask) label() string {
	return [...]string{"准备专项", "启动与绑定", "冻结计划", "测量", "读取结果", "执行完成", "取消中 · 等待收尾", "已取消", "未完成", "部分完成", "等待测量"}[r.stage]
}

func (r *speedTask) validCount() int {
	if r.result != nil {
		return r.result.Measurement.ValidCount
	}
	n := 0
	for _, v := range r.samples {
		if v.prompt != nil && v.seconds != nil && v.prefill != nil && v.decode != nil && v.reason == "" {
			n++
		}
	}
	return n
}

type speedWorkspace struct {
	selected                     bool
	context                      int
	current, opened              *speedTask
	history                      []*speedTask
	readError                    string
	table, samples, historyTable pageTableState
}

func (s *viewState) speedSelection(c *ui.Context) bool {
	if s.speed == nil {
		return false
	}
	w := s.speed
	ui.Field(c, "测评类型", func() {
		components.NativeSelect(c, &w.selected, []components.NativeSelectOption[bool]{
			{Value: false, Label: "常规测评"}, {Value: true, Label: "长上下文速度"},
		}).Trigger.Label("测评类型")
	})
	if !w.selected {
		return false
	}
	ui.Field(c, "上下文档位", func() {
		components.NativeSelect(c, &w.context, []components.NativeSelectOption[int]{
			{Value: 65536, Label: "64K · 65,536 tokens"},
			{Value: 131072, Label: "128K · 131,072 tokens"},
			{Value: 262144, Label: "256K · 262,144 tokens"},
		}).Trigger.Label("上下文档位")
	})
	ui.Text(c, "zh-context-speed · 5 次测量 · 每次输出 256 tokens")
	ui.Text(c, "5 次正式测量；另含探测、预热与排空。墙钟预算由冻结配方生成。")
	componentFold(c, "查看配方与停止条件", func() {
		ui.Text(c, "并发 1 · 固定输入 · 关闭输入缓存与投机解码")
		ui.Text(c, "超时、内存、磁盘及设备限制由 CLI 核验；容量不足或题库尚未批准时拒绝执行。")
	})
	if s.preview != nil {
		ui.Text(c, "模拟数据；数值不代表模型性能。").TextColor(c.Theme().Warning)
	}
	reason := s.speedStartBlockReason()
	if engineLocked(s.engineState.State) {
		button(c, "停止当前服务").Disabled(s.busy || s.engineState.State == "stopping").OnClick(s.stopEngine)
	}
	if reason != "" {
		ui.Text(c, reason).TextColor(c.Theme().Warning)
	}
	if s.eligibility(s.engine).Reason == "cli_outdated" {
		button(c, "打开 CLI 设置").OnClick(func() { s.selected = "settings" })
	}
	if primaryButton(c, "开始专项测评").Disabled(reason != "").AlignSelf(ui.Start).Clicked() {
		s.startSpeed()
	}
	return true
}

func (s *viewState) speedStartBlockReason() string {
	switch {
	case s.environmentMissing():
		return "请先完成环境准备。"
	case !s.preflightReady():
		return "请先完成工作区设备检查。"
	case s.newTaskPending:
		return "正在结束上一任务，请等待完成。"
	case s.busy:
		return "正在处理其他操作，请等待完成。"
	case s.running() != nil:
		return "已有任务尚未结束，请等待完成或取消后再开始。"
	case s.model == "":
		return "请选择测评模型。"
	case s.engine == "":
		return "请选择候选引擎。"
	case s.engineState.State == "orphaned":
		return "请先处理遗留服务记录，再开始专项测评。"
	case engineLocked(s.engineState.State):
		return "先停止当前受管服务，再按所选上下文启动新任务。"
	}
	e := s.eligibility(s.engine)
	switch e.State {
	case core.EngineCheckEligible:
		for _, model := range s.registry.Models {
			if model.Name == s.model && model.ContextLength != nil && s.speed != nil && *model.ContextLength < int64(s.speed.context) {
				return fmt.Sprintf("所选模型最多支持 %gK 上下文，不能运行 %dK 档；请更换长上下文模型。", float64(*model.ContextLength)/1024, s.speed.context/1024)
			}
		}
		return ""
	case core.EngineCheckChecking:
		return "正在核验所选引擎，请稍候。"
	case core.EngineCheckIneligible:
		return "所选引擎不可用：" + human(e.Reason) + "。请重新选择。"
	default:
		if e.Reason == "cli_outdated" {
			return "当前 CLI 不支持引擎核验；请在设置中选择支持长上下文速度的 CLI。"
		}
		return "所选引擎尚未通过核验，请到模型页查看原因。"
	}
}

func (s *viewState) speedTaskView(c *ui.Context) {
	r := s.speed.current
	s.header(c, "长上下文速度")
	ui.Text(c, fmt.Sprintf("%s · %s · %dK", r.model, r.engine, r.context/1024))
	label := "zh-context-speed · "
	if s.preview != nil {
		label += "模拟任务 "
	}
	ui.Text(c, label+r.id).TextColor(c.Theme().TextMuted)
	if r.budget > 0 {
		ui.Text(c, fmt.Sprintf("冻结墙钟预算 %.0f 秒（含准备与排空）", r.budget))
	}
	card(c, "任务进度", func() {
		ui.Row(c).Wrap().Gap(c.Theme().Space(2)).Children(func() {
			for i, label := range []string{"准备", "启动与绑定", "冻结计划", "测量", "读取结果"} {
				if speedStage(i) == r.stage {
					statusBadge(c, label, c.Theme().Accent)
				} else {
					ui.Text(c, label)
				}
			}
		})
	})
	step := "—"
	if r.stage <= speedReading {
		step = fmt.Sprint(int(r.stage) + 1)
	} else if r.stage == speedDone {
		step = "✓"
	}
	stepTrack(c, step, r.label(), func() {
		ui.Text(c, fmt.Sprintf("已完成 %d / 5 次测量", r.completed))
		progressBar(c, float64(r.completed)/5, "专项五次测量进度")
		if r.reason != "" {
			ui.Text(c, r.reason).TextColor(c.Theme().Warning)
		}
		switch {
		case r.active():
			button(c, "取消专项测评").Disabled(r.stage == speedCancelling).OnClick(s.cancelSpeed)
		default:
			primaryButton(c, "查看结果").OnClick(func() { s.openSpeed(r.id) })
			button(c, "用此组合新开任务").OnClick(func() { s.newSpeedTask(r) })
		}
	})
}

func (s *viewState) newSpeedTask(r *speedTask) {
	if r.selectionModel == "" || r.selectionEngine == "" {
		s.failure = &core.ApiError{Code: "context_speed_assets_unavailable", Message: "原任务模型或引擎已不可用，请恢复原资源后重新打开结果。"}
		return
	}
	s.newBenchmark()
	s.speed.selected, s.speed.context = true, r.context
	s.model, s.engine = r.model, r.engine
	if r.selectionModel != "" {
		s.model = r.selectionModel
	}
	if r.selectionEngine != "" {
		s.engine = r.selectionEngine
	}
}

func (s *viewState) openSpeed(id string) {
	s.openedReport = nil
	s.selected = "reports"
	if s.simulate(previewSpeedRead{id: id}) {
		return
	}
	var selected *speedTask
	for _, r := range s.speed.history {
		if r.id == id {
			copy := *r
			selected = &copy
			break
		}
	}
	if selected == nil {
		return
	}
	selected.samples, selected.summary, selected.result = nil, nil, nil
	s.speed.opened, s.speed.readError = selected, ""
	s.work(func() (string, *core.ApiError) {
		result, e := readSpeedFlow(s.ctx, s.service, selected)
		s.win.Update(func() {
			if s.speed.opened == nil || s.speed.opened.id != id {
				return
			}
			if e != nil {
				s.speed.readError = e.Message
				if s.speed.readError == "" {
					s.speed.readError = e.Code
				}
				s.speed.readError += " · " + fmt.Sprint(e.Limitations)
			} else {
				s.speed.opened = result
			}
		})
		return "", e
	})
}

func (s *viewState) speedResultView(c *ui.Context) {
	w, r := s.speed, s.speed.opened
	s.header(c, "长上下文速度 · 结果")
	ui.Text(c, r.model+" · "+r.engine+" · zh-context-speed")
	prefix := ""
	if s.preview != nil {
		prefix = "模拟数据 · "
	}
	ui.Text(c, prefix+r.id+" · "+r.label()).TextColor(c.Theme().Warning)
	ui.Row(c).Wrap().Gap(c.Theme().Space(2)).Children(func() {
		button(c, "返回历史任务").OnClick(func() { w.opened = nil; w.readError = "" })
		button(c, "用此组合新开任务").Disabled(s.busy).OnClick(func() { s.newSpeedTask(r) })
	})
	if w.readError != "" {
		ui.Text(c, w.readError).TextColor(c.Theme().Danger)
		primaryButton(c, "重新读取结果").OnClick(func() { s.openSpeed(r.id) })
		return
	}
	if r.stage == speedCancelled {
		ui.Text(c, "任务已取消。已完成的测量已保留；未完成项显示为 —。")
	} else if r.summary == nil {
		ui.Text(c, "测量结果不满足完整汇总条件；请展开诊断详情查看缺测或校验原因。").TextColor(c.Theme().Warning)
	}
	card(c, "测量结果", func() {
		if r.summary != nil {
			ui.Text(c, "有效 5/5 · 中位数")
			if r.result != nil && r.result.Summary != nil {
				v := r.result.Summary.Columns
				ui.Text(c, fmt.Sprintf("范围：Time %.2f–%.2f s · Prefill %.2f–%.2f tok/s · Decode %.2f–%.2f tok/s", v.Time.Min, v.Time.Max, v.Prefill.Min, v.Prefill.Max, v.Decode.Min, v.Decode.Max))
			}
			speedTable(c.Key("speed-summary"), &w.table, "专项六项指标", r, []speedSample{*r.summary})
		} else {
			ui.Text(c, fmt.Sprintf("有效 %d/5 · 未形成完整汇总", r.validCount()))
			if len(r.samples) > 0 {
				speedTable(c.Key("speed-partial"), &w.table, "专项已完成测量", r, r.samples)
			}
		}
		ui.Text(c, "Time 为输入处理耗时，包含首采样；不是完整请求耗时。")
		ui.Text(c, "Prompt length 为模板后实际输入。— 表示缺测，不计为 0。")
	})
	if r.summary == nil || r.reason != "" {
		componentFold(c.Key(r.id), "查看诊断详情", func() {
			if r.reason != "" {
				ui.Text(c, r.reason).TextColor(c.Theme().Warning)
			}
			for i, sample := range r.samples {
				if sample.missing != "" {
					ui.Text(c, fmt.Sprintf("第 %d 次缺测：%s", i+1, sample.missing)).TextColor(c.Theme().Warning)
				}
				if sample.reason != "" {
					ui.Text(c, fmt.Sprintf("第 %d 次：%s", i+1, sample.reason)).TextColor(c.Theme().Warning)
				}
			}
		})
	}
	if r.summary != nil {
		componentFold(c, "查看逐次测量", func() {
			ui.Text(c, "按第 1 至第 5 次测量顺序排列。")
			speedTable(c.Key("speed-samples"), &w.samples, "专项逐次测量", r, r.samples)
		})
	}
}

func speedTable(c *ui.Context, state *pageTableState, label string, task *speedTask, rows []speedSample) {
	columns := []ui.TableColumn{
		{Title: "模型 / 引擎", MinWidth: c.Theme().Rem(18)},
		{Title: "Context", MinWidth: c.Theme().Rem(7)},
		{Title: "Prompt length (tokens)", MinWidth: c.Theme().Rem(14)},
		{Title: "Time (s)", MinWidth: c.Theme().Rem(6)},
		{Title: "Prefill (tok/s)", MinWidth: c.Theme().Rem(9)},
		{Title: "Decode (tok/s)", MinWidth: c.Theme().Rem(9)},
	}
	selectableTable(state, func(i int) any { return i }, func(i int) string { return fmt.Sprintf("测量行 %d", i+1) })
	pageTable(c, state, label, columns, len(rows), func(row, col int) {
		v := rows[row]
		prompt := "—"
		if v.prompt != nil {
			prompt = fmt.Sprint(*v.prompt)
		}
		if col == 0 {
			speedIdentity(c, task)
			return
		}
		context := "—"
		if v.context != nil {
			context = fmt.Sprint(*v.context)
		}
		ui.Text(c, []string{"", context, prompt, speedNumber(v.seconds), speedNumber(v.prefill), speedNumber(v.decode)}[col]).SingleLine()
	}).Height(c.Theme().Rem(4)*float32(len(rows)+1) + 2)
}

func speedNumber(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f", *v)
}

func (s *viewState) speedHistoryView(c *ui.Context) {
	if s.speed == nil || len(s.speed.history) == 0 {
		return
	}
	w := s.speed
	card(c, "专项历史任务", func() {
		if s.preview != nil {
			ui.Text(c, "本窗口的模拟记录；关闭窗口后清空。").TextColor(c.Theme().TextMuted)
		} else {
			ui.Text(c, "打开时重新核验原始批次；不会重新测量。").TextColor(c.Theme().TextMuted)
		}
		selectableTable(&w.historyTable, func(i int) any { return w.history[i].id }, func(i int) string { return w.history[i].id })
		pageTable(c.Key("speed-history"), &w.historyTable, "专项历史表", []ui.TableColumn{
			{Title: "任务", MinWidth: c.Theme().Rem(11)}, {Title: "模型 / 引擎", MinWidth: c.Theme().Rem(24)},
			{Title: "Context", MinWidth: c.Theme().Rem(6)}, {Title: "状态", MinWidth: c.Theme().Rem(6)},
		}, len(w.history), func(row, col int) {
			r := w.history[row]
			if col == 1 {
				speedIdentity(c, r)
				return
			}
			ui.Text(c, []string{r.id, "", fmt.Sprint(r.context), r.label()}[col]).SingleLine()
		}).Height(c.Theme().Rem(4)*float32(min(len(w.history), 5)+1) + 2)
		if i := selectedRow(&w.historyTable, len(w.history)); i >= 0 {
			if primaryButton(c, "打开专项结果").Clicked() || w.historyTable.submitted {
				s.openSpeed(w.history[i].id)
			}
		} else {
			ui.Text(c, "选择任务后打开结果。")
		}
	})
}

func speedIdentity(c *ui.Context, task *speedTask) {
	ui.Column(c).Gap(c.Theme().Space(1)).Children(func() {
		ui.Text(c, task.model)
		ui.Text(c, task.engine).TextColor(c.Theme().TextMuted)
	})
}
