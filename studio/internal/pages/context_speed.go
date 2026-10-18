package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
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
)

type speedSample struct {
	prompt                   *int
	seconds, prefill, decode *float64
	reason                   string
}

type speedTask struct {
	id, model, engine, card string
	context                 int
	stage                   speedStage
	samples                 []speedSample
	summary                 *speedSample
	reason                  string
}

func (r *speedTask) active() bool { return r.stage <= speedReading || r.stage == speedCancelling }

func (r *speedTask) label() string {
	return [...]string{"准备专项", "启动与绑定", "冻结计划", "测量", "读取结果", "执行完成", "取消中 · 等待收尾", "已取消", "未完成"}[r.stage]
}

func (r *speedTask) validCount() int {
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
	ui.Text(c, "预算示例：最多 30 分钟；包含协议探测、5 次短文本预热和取消收尾。")
	componentFold(c, "查看配方与停止条件", func() {
		ui.Text(c, "并发 1 · 固定输入 · 关闭输入缓存与投机解码")
		ui.Text(c, "停止条件示例：单次 300 秒；可用内存低于 2 GiB、磁盘低于 1 GiB 或温度达到 90°C。实际预算须经设备核验。")
	})
	ui.Text(c, "界面预览 · 尚待认可；数值为模拟，不代表模型性能。").TextColor(c.Theme().Warning)
	blocked := s.environmentMissing() || !s.preflightReady() || s.busy || s.model == "" || s.engine == "" || s.eligibility(s.engine).State != core.EngineCheckEligible || s.engineState.State == "orphaned"
	if engineLocked(s.engineState.State) {
		ui.Text(c, "先停止当前受管服务，再按所选上下文启动新任务。")
		button(c, "停止当前服务").Disabled(s.busy || s.engineState.State == "stopping").OnClick(s.stopEngine)
		blocked = true
	}
	if blocked {
		ui.Text(c, "需完成环境准备、选择可用组合，且没有其他任务或受管服务在运行。").TextColor(c.Theme().Warning)
	}
	if primaryButton(c, "开始专项测评").Disabled(blocked).AlignSelf(ui.Start).Clicked() {
		s.simulate(previewSpeedStart{model: s.model, engine: s.engine, context: w.context})
	}
	return true
}

func (s *viewState) speedTaskView(c *ui.Context) {
	r := s.speed.current
	s.header(c, "长上下文速度")
	ui.Text(c, fmt.Sprintf("%s · %s · %dK", r.model, r.engine, r.context/1024))
	ui.Text(c, "zh-context-speed · 模拟任务 "+r.id).TextColor(c.Theme().TextMuted)
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
		ui.Text(c, fmt.Sprintf("已完成 %d / 5 次测量", len(r.samples)))
		progressBar(c, float64(len(r.samples))/5, "专项五次测量进度")
		if r.reason != "" {
			ui.Text(c, r.reason).TextColor(c.Theme().Warning)
		}
		switch {
		case r.active():
			button(c, "取消专项测评").Disabled(r.stage == speedCancelling).OnClick(func() { s.simulate(previewSpeedCancel{}) })
		default:
			primaryButton(c, "查看结果").OnClick(func() { s.openSpeed(r.id) })
			button(c, "用此组合新开任务").OnClick(func() { s.newSpeedTask(r) })
		}
	})
}

func (s *viewState) newSpeedTask(r *speedTask) {
	s.newBenchmark()
	s.speed.selected, s.speed.context = true, r.context
	s.model, s.engine = r.model, r.engine
}

func (s *viewState) openSpeed(id string) {
	s.openedReport = nil
	s.selected = "reports"
	s.simulate(previewSpeedRead{id: id})
}

func (s *viewState) speedResultView(c *ui.Context) {
	w, r := s.speed, s.speed.opened
	s.header(c, "长上下文速度 · 结果")
	ui.Text(c, r.model+" · "+r.engine+" · zh-context-speed")
	ui.Text(c, "模拟数据 · "+r.id+" · "+r.label()).TextColor(c.Theme().Warning)
	ui.Row(c).Wrap().Gap(c.Theme().Space(2)).Children(func() {
		button(c, "返回历史任务").OnClick(func() { w.opened = nil; w.readError = "" })
		button(c, "用此组合新开任务").Disabled(s.busy).OnClick(func() { s.newSpeedTask(r) })
	})
	if w.readError != "" {
		ui.Text(c, w.readError).TextColor(c.Theme().Danger)
		primaryButton(c, "重新读取结果").OnClick(func() { s.openSpeed(r.id) })
		return
	}
	if r.reason != "" {
		ui.Text(c, r.reason).TextColor(c.Theme().Warning)
	}
	card(c, "测量结果", func() {
		if r.summary != nil {
			ui.Text(c, "有效 5/5 · 中位数")
			speedTable(c.Key("speed-summary"), &w.table, "专项六项指标", r, []speedSample{*r.summary})
		} else {
			ui.Text(c, fmt.Sprintf("有效 %d/5 · 未形成完整汇总", r.validCount()))
			if len(r.samples) > 0 {
				speedTable(c.Key("speed-partial"), &w.table, "专项已完成测量", r, r.samples)
			}
		}
		for i, sample := range r.samples {
			if sample.reason != "" {
				ui.Text(c, fmt.Sprintf("第 %d 次：%s", i+1, sample.reason)).TextColor(c.Theme().Warning)
			}
		}
		ui.Text(c, "Time 为输入处理耗时，包含首采样；不是完整请求耗时。")
		ui.Text(c, "Prompt length 为模板后实际输入。— 表示缺测，不计为 0。")
	})
	if r.summary != nil {
		componentFold(c, "查看逐次测量", func() {
			ui.Text(c, "按第 1 至第 5 次测量顺序排列。")
			speedTable(c.Key("speed-samples"), &w.samples, "专项逐次测量", r, r.samples)
		})
	}
}

func speedTable(c *ui.Context, state *pageTableState, label string, task *speedTask, rows []speedSample) {
	columns := []ui.TableColumn{
		{Title: "Card", MinWidth: c.Theme().Rem(17)},
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
		ui.Text(c, []string{task.card, fmt.Sprint(task.context), prompt, speedNumber(v.seconds), speedNumber(v.prefill), speedNumber(v.decode)}[col]).SingleLine()
	}).Height(c.Theme().Space(8)*float32(len(rows)+1) + 2)
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
		ui.Text(c, "本窗口的模拟记录；关闭窗口后清空。").TextColor(c.Theme().TextMuted)
		selectableTable(&w.historyTable, func(i int) any { return w.history[i].id }, func(i int) string { return w.history[i].id })
		pageTable(c.Key("speed-history"), &w.historyTable, "专项历史表", []ui.TableColumn{
			{Title: "任务", MinWidth: c.Theme().Rem(11)}, {Title: "模型", MinWidth: c.Theme().Rem(15)},
			{Title: "Context", MinWidth: c.Theme().Rem(6)}, {Title: "状态", MinWidth: c.Theme().Rem(6)},
		}, len(w.history), func(row, col int) {
			r := w.history[row]
			ui.Text(c, []string{r.id, r.model, fmt.Sprint(r.context), r.label()}[col]).SingleLine()
		}).Height(c.Theme().Space(8)*float32(min(len(w.history), 5)+1) + 2)
		if i := selectedRow(&w.historyTable, len(w.history)); i >= 0 {
			if primaryButton(c, "打开专项结果").Clicked() || w.historyTable.submitted {
				s.openSpeed(w.history[i].id)
			}
		} else {
			ui.Text(c, "选择任务后打开结果。")
		}
	})
}
